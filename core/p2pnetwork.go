package openp2p

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"os"
	"runtime"

	// _ "net/http/pprof"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var (
	v4l            *v4Listener
	onceP2PNetwork sync.Once
)

const (
	retryLimit                  = 20
	retryInterval               = 10 * time.Second
	DefaultLoginMaxDelaySeconds = 60
	MsgQueueSize                = 256
)

// golang not support float64 const
var (
	ma20 float64 = 1.0 / 20
	ma10 float64 = 1.0 / 10
	ma5  float64 = 1.0 / 5
)

type NodeData struct {
	NodeID uint64 // unused
	Data   []byte
}

type P2PNetwork struct {
	conn          *websocket.Conn
	online        bool
	running       bool
	restartCh     chan bool
	wgReconnect   sync.WaitGroup
	writeMtx      sync.Mutex
	reqGatewayMtx sync.Mutex
	hbTime        time.Time
	initTime      time.Time
	// for sync server time
	t1     int64 // nanoSeconds
	preRtt int64 // nanoSeconds
	dt     int64 // client faster then server dt nanoSeconds
	ddtma  int64
	ddt    int64    // differential of dt
	msgMap sync.Map //key: nodeID
	// msgMap     map[uint64]chan pushMsg //key: nodeID
	allTunnels           sync.Map // key: tid
	apps                 sync.Map //key: peerid when memapp for sdwan node data indicate app/random uint64 when portforward; value: *p2pApp
	limiter              *SpeedLimiter
	nodeData             chan []byte
	sdwan                *p2pSDWAN
	tunnelCloseCh        chan *P2PTunnel
	loginMaxDelaySeconds int
	peerNodeMutex        sync.Map
}

type msgCtx struct {
	data []byte
	ts   time.Time
}

func P2PNetworkInstance() {
	if GNetwork == nil {
		onceP2PNetwork.Do(func() {
			GNetwork = &P2PNetwork{
				restartCh:            make(chan bool, 1),
				tunnelCloseCh:        make(chan *P2PTunnel, 100),
				nodeData:             make(chan []byte, 10000),
				online:               false,
				running:              true,
				limiter:              newSpeedLimiter(gConf.Network.ShareBandwidth*1024*1024/8, 1),
				dt:                   0,
				ddt:                  0,
				loginMaxDelaySeconds: DefaultLoginMaxDelaySeconds,
				initTime:             time.Now(),
			}
			GNetwork.msgMap.Store(uint64(0), make(chan msgCtx, MsgQueueSize)) // for gateway
			GNetwork.StartSDWAN()
			v4l = &v4Listener{port: gConf.Network.PublicIPPort}
			go GNetwork.keepAlive() // init() will block, keepalive should before init
			GNetwork.init()
			go GNetwork.run()

			go func() {
				ticker := time.NewTicker(10 * time.Minute)
				defer ticker.Stop()
				for range ticker.C {
					dumpStack()
				}
			}()
			go func() {
				for {
					time.Sleep(time.Hour)
					oldIPv6 := gConf.IPv6()
					GNetwork.refreshIPv6()
					newIPv6 := gConf.IPv6()
					if oldIPv6 != newIPv6 {
						req := ReportBasic{
							Mac:             gConf.Network.mac,
							LanIP:           gConf.Network.localIP,
							OS:              gConf.Network.os,
							HasIPv4:         gConf.Network.hasIPv4,
							HasUPNPorNATPMP: gConf.Network.hasUPNPorNATPMP,
							Version:         OpenP2PVersion,
							IPv6:            newIPv6,
						}
						GNetwork.write(MsgReport, MsgReportBasic, &req)
					}
				}
			}()
			cleanTempFiles()
			// go func() {
			// 	log.Println("Starting pprof server on :16060")
			// 	log.Println(http.ListenAndServe("0.0.0.0:16060", nil))
			// }()
		})
	}
}

func (pn *P2PNetwork) keepAlive() {
	gLog.i("P2PNetwork keepAlive start")
	// !hbTime && !initTime = hang, exit worker
	var lastCheckTime time.Time

	for {
		time.Sleep(time.Second * 10)
		// Skip check if we're waking from sleep/hibernation
		now := time.Now()
		if !lastCheckTime.IsZero() && now.Sub(lastCheckTime) > NetworkHeartbeatTime*3 {
			gLog.i("Detected possible sleep/wake cycle, skipping this check")
			lastCheckTime = now
			continue
		}
		lastCheckTime = now
		if pn.hbTime.Before(time.Now().Add(-NetworkHeartbeatTime * 3)) {
			if pn.initTime.After(time.Now().Add(-NetworkHeartbeatTime * 3)) {
				gLog.d("Init less than 3 mins, skipping this check")
				continue
			}
			gLog.e("P2PNetwork keepAlive error, exit worker")
			dumpStack()
			os.Exit(9)
		}
	}
}

func dumpStack() {
	buf := make([]byte, 1024*1024)
	n := runtime.Stack(buf, true)
	tmpFile := "./log/stack.log.tmp"
	if err := os.WriteFile(tmpFile, buf[:n], 0644); err != nil {
		gLog.e("print runtime.Stack error")
		return
	}
	os.Rename(tmpFile, "./log/stack.log")
}

func (pn *P2PNetwork) run() {
	heartbeatTimer := time.NewTicker(NetworkHeartbeatTime)
	pn.t1 = time.Now().UnixNano()
	pn.write(MsgHeartbeat, 0, "")
	for {
		select {
		case <-heartbeatTimer.C:
			pn.t1 = time.Now().UnixNano()
			pn.write(MsgHeartbeat, 0, "")
		case isRestartDelay := <-pn.restartCh:
			gLog.i("got restart channel")
			// pn.sdwan.reset()
			pn.online = false
			waitDone := make(chan struct{})
			go func() {
				defer close(waitDone)
				pn.wgReconnect.Wait()
			}()

			select {
			case <-waitDone:
			case <-time.After(30 * time.Second):
				gLog.e("pn.wgReconnect.Wait() timeout, mostly websocket hang. restart client")
				os.Exit(0)
			}

			if isRestartDelay {
				delay := ClientAPITimeout + time.Duration(rand.Int()%pn.loginMaxDelaySeconds)*time.Second
				time.Sleep(delay)
			}
			err := pn.init()
			if err != nil {
				gLog.e("P2PNetwork init error:%s", err)
			}
			gConf.retryAllApp()

		case t := <-pn.tunnelCloseCh:
			gLog.d("got tunnelCloseCh %s", t.config.LogPeerNode())
			pn.apps.Range(func(id, i interface{}) bool {
				app := i.(*p2pApp)
				for i := 0; i < app.tunnelNum; i++ {
					if app.Tunnel(i) == t {
						app.SetTunnel(nil, i)
					}
				}
				return true
			})
		}
	}
}

func (pn *P2PNetwork) NotifyTunnelClose(t *P2PTunnel) bool {
	select {
	case pn.tunnelCloseCh <- t:
		return true
	default:
	}
	return false
}

func (pn *P2PNetwork) Connect(timeout int) bool {
	// waiting for heartbeat
	for i := 0; i < (timeout / 1000); i++ {
		if pn.hbTime.After(time.Now().Add(-NetworkHeartbeatTime)) {
			return true
		}
		time.Sleep(time.Second)
	}
	return false
}

func (pn *P2PNetwork) runAll() {
	gConf.mtx.RLock() // lock for coRUpy gConf.Apps and the modification of config(it's pointer)
	defer gConf.mtx.RUnlock()
	allApps := gConf.Apps // read a copy, other thread will modify the gConf.Apps
	for _, config := range allApps {
		if config.Enabled == 0 {
			continue
		}
		if app := pn.findApp(config); app != nil {
			// update some attribute
			app.config.PunchPriority = config.PunchPriority
			app.config.UnderlayProtocol = config.UnderlayProtocol
			app.config.RelayNode = config.RelayNode
			continue
		}

		// config.peerToken = gConf.Network.Token // move to AddApp
		gConf.mtx.RUnlock() // AddApp will take a period of time, let outside modify gConf
		pn.AddApp(*config)
		gConf.mtx.RLock()

	}
}

func (pn *P2PNetwork) autorunApp() {
	gLog.i("autorunApp start")
	pn.wgReconnect.Add(1)
	defer pn.wgReconnect.Done()
	for pn.running && pn.online {
		time.Sleep(time.Second)
		pn.runAll()
	}
	gLog.i("autorunApp end")
}

func (pn *P2PNetwork) addRelayTunnel(config AppConfig, excludeNodes string) (*P2PTunnel, uint64, string, error) {
	gLog.i("addRelayTunnel to %s start", config.LogPeerNode())
	defer gLog.i("addRelayTunnel to %s end", config.LogPeerNode())
	var relayTunnel *P2PTunnel
	relayConfig := AppConfig{
		peerToken:        config.peerToken,
		PunchPriority:    config.PunchPriority,
		UnderlayProtocol: config.UnderlayProtocol,
		relayMode:        "private",
	}
	if config.RelayNode != excludeNodes {
		relayConfig.PeerNode = config.RelayNode
		// TODO: verify relay node is online
	}
	if relayConfig.PeerNode == "" {
		// find existing relay tunnel
		pn.apps.Range(func(id, i interface{}) bool {
			app := i.(*p2pApp)
			if app.config.PeerNode != config.PeerNode {
				return true
			}
			for i := 1; i < app.tunnelNum; i++ { // index 1 for relay tunnel
				if app.Tunnel(i) != nil && app.Tunnel(i).config.PeerNode != excludeNodes && time.Now().Before(app.hbTime[i].Add(TunnelHeartbeatTime*2)) {
					relayConfig.PeerNode = app.Tunnel(i).config.PeerNode
					relayConfig.relayMode = app.Tunnel(i).config.relayMode
					relayTunnel = app.Tunnel(i)
					gLog.d("found existing relay tunnel %s", relayConfig.LogPeerNode())
					return false
				}
			}
			return true
		})
		if relayConfig.PeerNode == "" { // request relay node
			pn.reqGatewayMtx.Lock()
			pn.write(MsgRelay, MsgRelayNodeReq, &RelayNodeReq{config.PeerNode, excludeNodes})
			head, body := pn.read("", MsgRelay, MsgRelayNodeRsp, ClientAPITimeout)
			pn.reqGatewayMtx.Unlock()
			if head == nil {
				return nil, 0, "", errors.New("read MsgRelayNodeRsp error")
			}
			rsp := RelayNodeRsp{}
			if err := json.Unmarshal(body, &rsp); err != nil {
				return nil, 0, "", errors.New("unmarshal MsgRelayNodeRsp error")
			}
			if rsp.RelayName == "" || rsp.RelayToken == 0 {
				gLog.e("MsgRelayNodeReq error")
				return nil, 0, "", errors.New("MsgRelayNodeReq error")
			}
			relayConfig.PeerNode = rsp.RelayName
			relayConfig.peerToken = rsp.RelayToken
			relayConfig.relayMode = rsp.Mode
			gLog.d("got relay node:%s", relayConfig.LogPeerNode())
		}

	}
	///
	if relayTunnel == nil {
		var err error
		relayTunnel, err = pn.addDirectTunnel(relayConfig, 0)
		if err != nil || relayTunnel == nil {
			gLog.w("direct connect error:%s", err)
			if err != nil && config.RelayNode != "" {
				return nil, 0, "", err // let outside known the specified relay node offline, than stop retry
			}
			return nil, 0, "", ErrConnectRelayNode // relay offline will stop retry
		}
	}

	// notify peer addRelayTunnel
	req := AddRelayTunnelReq{
		From:             gConf.Network.Node,
		RelayName:        relayConfig.PeerNode,
		RelayToken:       relayConfig.peerToken,
		RelayMode:        relayConfig.relayMode,
		RelayTunnelID:    relayTunnel.id,
		PunchPriority:    relayConfig.PunchPriority,
		UnderlayProtocol: relayConfig.UnderlayProtocol,
	}

	gLog.d("push %s the relay node(%s)", config.LogPeerNode(), relayConfig.LogPeerNode())
	pn.push(config.PeerNode, MsgPushAddRelayTunnelReq, &req)

	// wait relay ready
	head, body := pn.read(config.PeerNode, MsgPush, MsgPushAddRelayTunnelRsp, PeerAddRelayTimeount)
	if head == nil {
		gLog.e("read MsgPushAddRelayTunnelRsp error")
		return nil, 0, "", errors.New("read MsgPushAddRelayTunnelRsp error")
	}
	rspID := TunnelMsg{}
	if err := json.Unmarshal(body, &rspID); err != nil {
		gLog.d("Unmarshal error:%s", ErrPeerConnectRelay)
		return nil, 0, "", ErrPeerConnectRelay
	}
	return relayTunnel, rspID.ID, relayConfig.relayMode, nil
}

// use *AppConfig to save status
func (pn *P2PNetwork) AddApp(config AppConfig) error {
	config.peerToken = gConf.Network.Token
	gLog.i("addApp %s to %s:%s:%d start", config.AppName, config.LogPeerNode(), config.DstHost, config.DstPort)
	defer gLog.i("addApp %s to %s:%s:%d end", config.AppName, config.LogPeerNode(), config.DstHost, config.DstPort)
	if !pn.online {
		return errors.New("P2PNetwork offline")
	}
	if _, ok := pn.msgMap.Load(NodeNameToID(config.PeerNode)); !ok {
		pn.msgMap.Store(NodeNameToID(config.PeerNode), make(chan msgCtx, MsgQueueSize))
	}
	// check if app already exist?
	if pn.findApp(&config) != nil {
		return errors.New("P2PApp already exist")
	}

	app := p2pApp{
		// tunnel:    t,
		id:      rand.Uint64(),
		key:     rand.Uint64(),
		config:  config,
		iptree:  NewIPTree(config.Whitelist),
		running: true,
		// asyncWriteChan: make(chan []byte, WriteDataChanSize),
	}
	if config.SrcPort == 0 {
		app.id = NodeNameToID(config.PeerNode)
	}
	tunnelNum := 2

	app.Init(tunnelNum)
	if _, ok := pn.msgMap.Load(NodeNameToID(config.PeerNode)); !ok {
		pn.msgMap.Store(NodeNameToID(config.PeerNode), make(chan msgCtx, MsgQueueSize))
	}
	app.Start(true)
	pn.apps.Store(app.id, &app) // TODO: store appid
	gLog.d("Store app %d", app.id)

	return nil
}

func (pn *P2PNetwork) findApp(config *AppConfig) (app *p2pApp) {
	pn.apps.Range(func(id, i interface{}) bool {
		tempApp := i.(*p2pApp)
		if config.SrcPort == 0 { // sdwan app
			if tempApp.config.SrcPort == config.SrcPort &&
				tempApp.config.PeerNode == config.PeerNode {
				app = tempApp
				return false
			}
		} else { // portforward app
			if tempApp.config.SrcPort == config.SrcPort &&
				tempApp.config.Protocol == config.Protocol {
				app = tempApp
				return false
			}
		}
		return true
	})
	return
}

func (pn *P2PNetwork) DeleteApp(config AppConfig) {
	gLog.i("DeleteApp %s to %s:%s:%d start", config.AppName, config.LogPeerNode(), config.DstHost, config.DstPort)
	defer gLog.i("DeleteApp %s to %s:%s:%d end", config.AppName, config.LogPeerNode(), config.DstHost, config.DstPort)
	// close the apps of this config
	if tempApp := pn.findApp(&config); tempApp != nil {
		gLog.i("app %s exist, delete it", tempApp.config.AppName)
		tempApp.Close()
		if config.SrcPort != 0 {
			pn.apps.Delete(tempApp.id)
		} else {
			pn.apps.Delete(NodeNameToID(config.PeerNode))
		}

	}

}

func (pn *P2PNetwork) findTunnel(peerNode string) (t *P2PTunnel) {
	t = nil
	// find existing tunnel to peer
	pn.allTunnels.Range(func(id, i interface{}) bool {
		tmpt := i.(*P2PTunnel)
		if tmpt.config.PeerNode == peerNode {
			gLog.i("tunnel already exist %s", tmpt.config.LogPeerNode())
			isActive := tmpt.checkActive()
			// inactive, close it
			if !isActive {
				gLog.i("but it's not active, close it %s", tmpt.config.LogPeerNode())
				tmpt.close()
			} else {
				t = tmpt
			}
			return false
		}
		return true
	})
	return t
}

func (pn *P2PNetwork) addDirectTunnel(config AppConfig, tid uint64) (t *P2PTunnel, err error) {
	gLog.d("addDirectTunnel %s%d to %s:%s:%d tid:%d start", config.Protocol, config.SrcPort, config.LogPeerNode(), config.DstHost, config.DstPort, tid)
	defer gLog.d("addDirectTunnel %s%d to %s:%s:%d tid:%d end", config.Protocol, config.SrcPort, config.LogPeerNode(), config.DstHost, config.DstPort, tid)

	nodeID := NodeNameToID(config.PeerNode)
	mutex, _ := pn.peerNodeMutex.LoadOrStore(nodeID, &sync.Mutex{})
	mutex.(*sync.Mutex).Lock()
	defer mutex.(*sync.Mutex).Unlock()

	isClient := false
	// client side tid=0, assign random uint64
	if tid == 0 {
		tid = rand.Uint64()
		isClient = true
	}

	if _, ok := pn.msgMap.Load(nodeID); !ok {
		pn.msgMap.Store(nodeID, make(chan msgCtx, MsgQueueSize))
	}

	if isClient { // only client side find existing tunnel, server side should force build tunnel
		if existTunnel := pn.findTunnel(config.PeerNode); existTunnel != nil {
			return existTunnel, nil
		}
	}

	// server side
	if !isClient {
		t, err = pn.newTunnel(config, tid, isClient)
		return t, err // always return
	}

	// client side
	// peer info
	initErr := pn.requestPeerInfo(&config)
	if initErr != nil {
		gLog.w("%s init error:%s", config.LogPeerNode(), initErr)
		return nil, initErr
	}

	gLog.d("config.peerNode=%s,config.peerVersion=%s,config.peerIP=%s,config.peerLanIP=%s,gConf.Network.publicIP=%s,config.peerIPv6=%s,config.hasIPv4=%d,config.hasUPNPorNATPMP=%d,gConf.Network.hasIPv4=%d,gConf.Network.hasUPNPorNATPMP=%d,config.peerNatType=%d,gConf.Network.natType=%d,",
		config.LogPeerNode(), config.peerVersion, config.peerIP, config.peerLanIP, gConf.Network.publicIP, config.peerIPv6, config.hasIPv4, config.hasUPNPorNATPMP, gConf.Network.hasIPv4, gConf.Network.hasUPNPorNATPMP, config.peerNatType, gConf.Network.natType)

	// try Intranet
	if config.peerIP == gConf.Network.publicIP && compareVersion(config.peerVersion, SupportIntranetVersion) >= 0 { // old version client has no peerLanIP
		gLog.i("try Intranet")
		config.linkMode = LinkModeIntranet
		config.isUnderlayServer = 0
		if t, err = pn.newTunnel(config, tid, isClient); err == nil {
			return t, nil
		}
	}
	thisTunnelForcev6 := false
	// try TCP6
	if !strings.Contains(gConf.Network.Node, "openp2pS2STest") && IsIPv6(config.peerIPv6) && IsIPv6(gConf.IPv6()) && (config.PunchPriority&PunchPriorityUDPOnly == 0) {
		gLog.i("try TCP6")
		config.linkMode = LinkModeTCP6
		config.isUnderlayServer = 0
		if gConf.Forcev6 {
			thisTunnelForcev6 = true
		}
		if t, err = pn.newTunnel(config, tid, isClient); err == nil {
			return t, nil
		}
	}

	// try UDP6? maybe no

	// try IPv4
	if !thisTunnelForcev6 && !strings.Contains(gConf.Network.Node, "openp2pS2STest") && (config.hasIPv4 == 1 || gConf.Network.hasIPv4 == 1 || config.hasUPNPorNATPMP == 1 || gConf.Network.hasUPNPorNATPMP == 1) {
		if config.PunchPriority&PunchPriorityUDPOnly != 0 && compareVersion(config.peerVersion, SupportUDP4DirectVersion) >= 0 {
			gLog.i("try UDP4")
			config.linkMode = LinkModeUDP4
		} else {
			gLog.i("try TCP4")
			config.linkMode = LinkModeTCP4
		}

		if gConf.Network.hasIPv4 == 1 || gConf.Network.hasUPNPorNATPMP == 1 {
			config.isUnderlayServer = 1
		} else {
			config.isUnderlayServer = 0
		}
		if t, err = pn.newTunnel(config, tid, isClient); err == nil {
			return t, nil
		}
	}
	// try UDP4? maybe no
	var primaryPunchFunc func() (*P2PTunnel, error)
	var secondaryPunchFunc func() (*P2PTunnel, error)
	funcUDP := func() (t *P2PTunnel, err error) {
		if thisTunnelForcev6 || config.PunchPriority&PunchPriorityTCPOnly != 0 {
			return
		}
		// try UDPPunch
		for i := 0; i < Cone2ConeUDPPunchMaxRetry; i++ { // when both 2 nats has restrict firewall, simultaneous punching needs to be very precise, it takes a few tries
			if config.peerNatType == NATCone || gConf.Network.natType == NATCone {
				gLog.i("try UDP4 Punch")
				config.linkMode = LinkModeUDPPunch
				config.isUnderlayServer = 0
				if t, err = pn.newTunnel(config, tid, isClient); err == nil {
					return t, nil
				}
			}
			if !(config.peerNatType == NATCone && gConf.Network.natType == NATCone) { // not cone2cone, no more try
				break
			}
		}
		return
	}
	funcTCP := func() (t *P2PTunnel, err error) {
		if thisTunnelForcev6 || config.PunchPriority&PunchPriorityUDPOnly != 0 {
			return
		}
		// try TCPPunch
		for i := 0; i < Cone2ConeTCPPunchMaxRetry; i++ { // when both 2 nats has restrict firewall, simultaneous punching needs to be very precise, it takes a few tries
			if config.peerNatType == NATCone || gConf.Network.natType == NATCone {
				gLog.i("try TCP4 Punch")
				config.linkMode = LinkModeTCPPunch
				config.isUnderlayServer = 0
				if t, err = pn.newTunnel(config, tid, isClient); err == nil {
					gLog.i("TCP4 Punch ok")
					return t, nil
				}
			}
		}
		return
	}
	if config.PunchPriority&PunchPriorityTCPFirst != 0 {
		primaryPunchFunc = funcTCP
		secondaryPunchFunc = funcUDP
	} else {
		primaryPunchFunc = funcTCP
		secondaryPunchFunc = funcUDP
	}
	if t, err = primaryPunchFunc(); t != nil && err == nil {
		return t, err
	}
	if t, err = secondaryPunchFunc(); t != nil && err == nil {
		return t, err
	}

	// TODO: s2s won't return err
	return nil, err
}

func (pn *P2PNetwork) newTunnel(config AppConfig, tid uint64, isClient bool) (t *P2PTunnel, err error) {
	if isClient { // only client side find existing tunnel, server side should force build tunnel
		if existTunnel := pn.findTunnel(config.PeerNode); existTunnel != nil {
			return existTunnel, nil
		}
	}

	t = &P2PTunnel{
		config:         config,
		id:             tid,
		writeData:      make(chan []byte, WriteDataChanSize),
		writeDataSmall: make(chan []byte, WriteDataChanSize),
	}
	t.initPort()
	if isClient {
		if err = t.connect(); err != nil {
			gLog.d("p2pTunnel connect error:%s", err)
			return
		}
	} else {
		if err = t.listen(); err != nil {
			gLog.d("p2pTunnel listen error:%s", err)
			return
		}
	}
	// store it when success
	gLog.d("store tunnel %d", tid)
	pn.allTunnels.Store(tid, t)
	return
}

func (pn *P2PNetwork) init() error {
	gLog.i("P2PNetwork init start")
	defer gLog.i("P2PNetwork init end")
	pn.initTime = time.Now()
	pn.wgReconnect.Add(1)
	defer pn.wgReconnect.Done()
	var err error
	defer func() {
		if err != nil {
			// init failed, retry
			pn.close(true)
			gLog.e("P2PNetwork init error:%s", err)
		}
	}()
	ips, err := resolveServerIP(gConf.Network.ServerHost)
	if err != nil {
		gLog.e("resolve dns failed: %v", err)
		return err
	}
	gConf.Network.ServerIP = ips[0]
	if isAndroid() {
		net.DefaultResolver = &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				gLog.i("lookup dns %s %s", network, address)
				dialer := &net.Dialer{
					Timeout: 5 * time.Second,
				}
				primaryDNS := "119.29.29.29:53" // Tencent Cloud DNS
				return dialer.DialContext(ctx, network, primaryDNS)
			},
		}
	}
	v4l.stop() // stop old v4 listener if exist
	for {
		// detect nat type
		gConf.Network.publicIP, gConf.Network.natType, err = getNATType(gConf.Network.ServerIP, NATDetectPort1, NATDetectPort2)
		if err != nil {
			gLog.d("detect NAT type error:%s", err)
			break
		}
		if gConf.Network.hasIPv4 == 0 && gConf.Network.hasUPNPorNATPMP == 0 { // if already has ipv4 or upnp no need test again
			gConf.Network.hasIPv4, gConf.Network.hasUPNPorNATPMP = publicIPTest(gConf.Network.publicIP, gConf.Network.PublicIPPort)
		}

		// for testcase
		if strings.Contains(gConf.Network.Node, "openp2pS2STest") {
			gConf.Network.natType = NATSymmetric
			gConf.Network.hasIPv4 = 0
			gConf.Network.hasUPNPorNATPMP = 0
			gLog.i("openp2pS2STest debug")

		}
		if strings.Contains(gConf.Network.Node, "openp2pC2CTest") {
			gConf.Network.natType = NATCone
			gConf.Network.hasIPv4 = 0
			gConf.Network.hasUPNPorNATPMP = 0
			gLog.i("openp2pC2CTest debug")
		}

		// public ip and intranet connect
		v4l.start()
		pn.refreshIPv6()
		gLog.i("hasIPv4:%d, UPNP:%d, NAT type:%d, publicIP:%s, IPv6:%s", gConf.Network.hasIPv4, gConf.Network.hasUPNPorNATPMP, gConf.Network.natType, gConf.Network.publicIP, gConf.IPv6())
		gatewayURL := fmt.Sprintf("%s:%d", gConf.Network.ServerIP, gConf.Network.ServerPort)
		uri := "/api/v1/login"
		caCertPool, errCert := x509.SystemCertPool()
		if errCert != nil {
			gLog.e("Failed to load system root CAs:%s", errCert)
			caCertPool = x509.NewCertPool()
		}
		caCertPool.AppendCertsFromPEM([]byte(rootCA))
		caCertPool.AppendCertsFromPEM([]byte(rootEdgeCA))
		caCertPool.AppendCertsFromPEM([]byte(ISRGRootX1))
		config := tls.Config{
			RootCAs:            caCertPool,
			InsecureSkipVerify: gConf.TLSInsecureSkipVerify} // let's encrypt root cert "DST Root CA X3" expired at 2021/09/29. many old system(windows server 2008 etc) will not trust our cert
		websocket.DefaultDialer.TLSClientConfig = &config
		websocket.DefaultDialer.HandshakeTimeout = ClientAPITimeout * 3
		u := url.URL{Scheme: "wss", Host: gatewayURL, Path: uri}
		q := u.Query()
		q.Add("node", gConf.Network.Node)
		q.Add("token", fmt.Sprintf("%d", gConf.Network.Token))
		q.Add("version", OpenP2PVersion)
		q.Add("ipv4", gConf.Network.publicIP)
		q.Add("ipv6", gConf.IPv6())
		q.Add("nattype", fmt.Sprintf("%d", gConf.Network.natType))
		q.Add("sharebandwidth", fmt.Sprintf("%d", gConf.Network.ShareBandwidth))
		u.RawQuery = q.Encode()
		d := websocket.Dialer{
			NetDialContext: (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
			TLSClientConfig: &tls.Config{
				RootCAs:            caCertPool,               // 你的根证书池
				ServerName:         gConf.Network.ServerHost, // <--- 关键：把域名放到 ServerName
				InsecureSkipVerify: gConf.TLSInsecureSkipVerify,
			},
			HandshakeTimeout: 10 * time.Second,
		}

		ws, _, err := d.Dial(u.String(), nil)
		if err != nil {
			gLog.e("Dial error:%s", err)
			break
		}
		pn.running = true
		pn.online = true
		pn.conn = ws
		localAddr := strings.Split(ws.LocalAddr().String(), ":")
		if len(localAddr) == 2 {
			gConf.Network.localIP = localAddr[0]
		} else {
			err = errors.New("get local ip failed")
			break
		}
		go pn.readLoop()
		gConf.Network.mac = getmac(gConf.Network.localIP)
		gConf.Network.os = getOsName()
		go func() {
			req := ReportBasic{
				Mac:             gConf.Network.mac,
				LanIP:           gConf.Network.localIP,
				OS:              gConf.Network.os,
				HasIPv4:         gConf.Network.hasIPv4,
				HasUPNPorNATPMP: gConf.Network.hasUPNPorNATPMP,
				Version:         OpenP2PVersion,
			}
			rsp := netInfo()
			gLog.d("netinfo:%v", rsp)
			if rsp != nil && rsp.Country != "" {
				if IsIPv6(rsp.IP.String()) {
					gConf.setIPv6(rsp.IP.String())
				}
				req.NetInfo = *rsp
			} else {
				pn.refreshIPv6()
			}
			req.IPv6 = gConf.IPv6()
			pn.write(MsgReport, MsgReportBasic, &req)
		}()
		go pn.autorunApp()
		pn.write(MsgSDWAN, MsgSDWANInfoReq, nil)
		gLog.d("P2PNetwork init ok")
		break
	}

	return err
}

func (pn *P2PNetwork) handleMessage(msg []byte) {
	head := openP2PHeader{}
	err := binary.Read(bytes.NewReader(msg[:openP2PHeaderSize]), binary.LittleEndian, &head)
	if err != nil {
		gLog.e("handleMessage error:%s", err)
		return
	}
	gLog.dev("handleMessage %+v", head)
	switch head.MainType {
	case MsgLogin:
		// gLog.Println(LevelINFO,string(msg))
		rsp := LoginRsp{}
		if err = json.Unmarshal(msg[openP2PHeaderSize:], &rsp); err != nil {
			gLog.e("wrong %v:%s", reflect.TypeOf(rsp), err)
			return
		}
		if rsp.Error != 0 {
			gLog.e("login error:%d, detail:%s", rsp.Error, rsp.Detail)
			pn.running = false
		} else {
			gConf.setToken(rsp.Token)
			gConf.setUser(rsp.User)
			if len(rsp.Node) >= MinNodeNameLen {
				gConf.setNode(rsp.Node)
			}
			gConf.save()
			if rsp.LoginMaxDelay > 0 {
				pn.loginMaxDelaySeconds = rsp.LoginMaxDelay
			}
			gLog.i("login ok. user=%s, node=%s", rsp.User, rsp.Node)
		}
	case MsgHeartbeat:
		gLog.dev("P2PNetwork heartbeat ok")
		pn.hbTime = time.Now()
		rtt := pn.hbTime.UnixNano() - pn.t1
		if rtt > int64(PunchTsDelay) || (pn.preRtt > 0 && rtt > pn.preRtt*5) {
			gLog.d("rtt=%dms too large ignore", rtt/int64(time.Millisecond))
			return // invalid hb rsp
		}
		pn.preRtt = rtt
		t2 := int64(binary.LittleEndian.Uint64(msg[openP2PHeaderSize : openP2PHeaderSize+8]))
		thisdt := pn.t1 + rtt/2 - t2
		newdt := thisdt
		if pn.dt != 0 {
			ddt := thisdt - pn.dt
			pn.ddt = ddt
			if pn.ddtma == 0 {
				pn.ddtma = pn.ddt
			} else {
				pn.ddtma = int64(float64(pn.ddtma)*(1-ma10) + float64(pn.ddt)*ma10) // avoid int64 overflow
				newdt = pn.dt + pn.ddtma
			}
		}
		pn.dt = newdt
		gLog.dev("synctime thisdt=%dms dt=%dms ddt=%dns ddtma=%dns rtt=%dms ", thisdt/int64(time.Millisecond), pn.dt/int64(time.Millisecond), pn.ddt, pn.ddtma, rtt/int64(time.Millisecond))
	case MsgPush:
		handlePush(head.SubType, msg)
	case MsgSDWAN:
		handleSDWAN(head.SubType, msg)
	default:
		i, ok := pn.msgMap.Load(uint64(0))
		if ok {
			ch := i.(chan msgCtx)
			select {
			case ch <- msgCtx{data: msg, ts: time.Now()}:
			default:
				gLog.e("msgQueue full, drop it")
			}
		}

		return
	}
}

func (pn *P2PNetwork) readLoop() {
	gLog.d("P2PNetwork readLoop start")
	pn.wgReconnect.Add(1)
	defer pn.wgReconnect.Done()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 使用带超时的 goroutine 读取
	readChan := make(chan []byte, 10)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				pn.conn.SetReadDeadline(time.Now().Add(NetworkHeartbeatTime + 10*time.Second))
				_, msg, err := pn.conn.ReadMessage()
				if err != nil {
					gLog.e("ReadMessage error:%s", err)
					readChan <- nil
					return
				}
				readChan <- msg
			}
		}
	}()

	readTimeout := 60 * time.Second

	for pn.running {
		select {
		case result := <-readChan:
			if result == nil {
				// 处理错误
				pn.close(false)
				return
			}
			pn.handleMessage(result)

		case <-time.After(readTimeout):
			gLog.e("ReadMessage timeout after %v", readTimeout)
			cancel()
			pn.close(false)
			return
		}
	}
	gLog.d("P2PNetwork readLoop end")
}

func (pn *P2PNetwork) write(mainType uint16, subType uint16, packet interface{}) error {
	if !pn.online {
		return errors.New("P2P network offline")
	}
	msg, err := newMessage(mainType, subType, packet)
	if err != nil {
		return err
	}
	pn.writeMtx.Lock()
	defer pn.writeMtx.Unlock()
	pn.conn.SetWriteDeadline(time.Now().Add(NetworkHeartbeatTime))
	if err = pn.conn.WriteMessage(websocket.BinaryMessage, msg); err != nil {
		gLog.e("write msgType %d,%d error:%s", mainType, subType, err)
		pn.close(false)
	}
	return err
}

func (pn *P2PNetwork) relay(to uint64, body []byte) error {
	i, ok := pn.allTunnels.Load(to)
	if !ok {
		return ErrRelayTunnelNotFound
	}
	tunnel := i.(*P2PTunnel)
	if tunnel.config.shareBandwidth > 0 {
		pn.limiter.Add(len(body), true)
	}
	var err error
	if err = tunnel.conn.WriteBuffer(body); err != nil {
		gLog.dev("relay to %d len=%d error:%s", to, len(body), err)
	}
	return err
}

func (pn *P2PNetwork) push(to string, subType uint16, packet interface{}) error {
	// gLog.d("push msgType %d to %s", subType, to)
	if !pn.online {
		return errors.New("client offline")
	}
	pushHead := PushHeader{}
	pushHead.From = gConf.nodeID()
	pushHead.To = NodeNameToID(to)
	pushHeadBuf := new(bytes.Buffer)
	err := binary.Write(pushHeadBuf, binary.LittleEndian, pushHead)
	if err != nil {
		return err
	}
	data, err := json.Marshal(packet)
	if err != nil {
		return err
	}
	// gLog.Println(LevelINFO,"write packet:", string(data))
	pushMsg := append(encodeHeader(MsgPush, subType, uint32(len(data)+PushHeaderSize)), pushHeadBuf.Bytes()...)
	pushMsg = append(pushMsg, data...)
	pn.writeMtx.Lock()
	defer pn.writeMtx.Unlock()
	pn.conn.SetWriteDeadline(time.Now().Add(NetworkHeartbeatTime))
	if err = pn.conn.WriteMessage(websocket.BinaryMessage, pushMsg); err != nil {
		gLog.e("push to %s error:%s", to, err)
		pn.close(false)
	}
	return err
}

func (pn *P2PNetwork) close(isRestartDelay bool) {
	if pn.running {
		if pn.conn != nil {
			pn.conn.Close()
		}
		pn.running = false
	}
	select {
	case pn.restartCh <- isRestartDelay:
	default:
	}
}

func (pn *P2PNetwork) read(node string, mainType uint16, subType uint16, timeout time.Duration) (head *openP2PHeader, body []byte) {
	var nodeID uint64
	if node == "" {
		nodeID = 0
	} else {
		nodeID = NodeNameToID(node)
	}
	i, ok := pn.msgMap.Load(nodeID)
	if !ok {
		gLog.e("read msg error: %s not found", node)
		return
	}
	ch := i.(chan msgCtx)
	for {
		select {
		case <-time.After(timeout):
			gLog.e("read msg error %d:%d timeout", mainType, subType)
			return
		case msg := <-ch:
			head = &openP2PHeader{}
			err := binary.Read(bytes.NewReader(msg.data[:openP2PHeaderSize]), binary.LittleEndian, head)
			if err != nil {
				gLog.e("read msg error:%s", err)
				break
			}
			if time.Since(msg.ts) > ReadMsgTimeout {
				gLog.d("read msg error expired %d:%d", head.MainType, head.SubType)
				continue
			}
			if head.MainType != mainType || head.SubType != subType {
				// gLog.d("read msg error type %d:%d expect %d:%d, requeue it", head.MainType, head.SubType, mainType, subType)
				ch <- msg
				time.Sleep(time.Second)
				continue
			}
			if mainType == MsgPush {
				body = msg.data[openP2PHeaderSize+PushHeaderSize:]
			} else {
				body = msg.data[openP2PHeaderSize:]
			}
			return
		}
	}
}

func (pn *P2PNetwork) updateAppHeartbeat(appID uint64, rtid uint64, updateRelayTs bool) {
	pn.apps.Range(func(id, i interface{}) bool {
		app := i.(*p2pApp)
		if app.id == appID {
			if updateRelayTs {
				app.UpdateRelayHeartbeatTs(rtid)
			} else {
				app.UpdateHeartbeat(rtid)
			}

		}
		return true
	})
}

// ipv6 will expired need to refresh.
func (pn *P2PNetwork) refreshIPv6() {
	for i := 0; i < 2; i++ {
		client := &http.Client{Timeout: time.Second * 10}
		r, err := client.Get("http://ipv6.ddnspod.com/")
		if err != nil {
			gLog.d("refreshIPv6 error:%s", err)
			continue
		}
		defer r.Body.Close()
		buf := make([]byte, 1024)
		n, err := r.Body.Read(buf)
		if n <= 0 {
			gLog.e("refreshIPv6 error:%s", err)
			continue
		}
		if IsIPv6(string(buf[:n])) {
			newIPv6 := string(buf[:n])
			if newIPv6 != gConf.IPv6() {
				gLog.i("refreshIPv6 change:%s ---> %s", gConf.IPv6(), newIPv6)
				gConf.setIPv6(newIPv6)

			}
		}
		gLog.d("refreshIPv6:%s", gConf.IPv6())
		break
	}

}

func (pn *P2PNetwork) requestPeerInfo(config *AppConfig) error {
	// request peer info
	// TODO: multi-thread issue
	pn.reqGatewayMtx.Lock()
	pn.write(MsgQuery, MsgQueryPeerInfoReq, &QueryPeerInfoReq{config.peerToken, config.PeerNode})
	head, body := pn.read("", MsgQuery, MsgQueryPeerInfoRsp, ClientAPITimeout)
	pn.reqGatewayMtx.Unlock()
	if head == nil {
		gLog.e("requestPeerInfo error")
		return ErrNetwork // network error, should not be ErrPeerOffline
	}
	rsp := QueryPeerInfoRsp{}
	if err := json.Unmarshal(body, &rsp); err != nil {
		return ErrMsgFormat
	}
	if rsp.Online == 0 {
		return ErrPeerOffline
	}
	if compareVersion(rsp.Version, LeastSupportVersion) < 0 {
		return ErrVersionNotCompatible
	}
	config.peerVersion = rsp.Version
	config.peerLanIP = rsp.LanIP
	config.hasIPv4 = rsp.HasIPv4
	config.peerIP = rsp.IPv4
	config.peerIPv6 = rsp.IPv6
	config.hasUPNPorNATPMP = rsp.HasUPNPorNATPMP
	config.peerNatType = rsp.NatType
	///
	return nil
}

func (pn *P2PNetwork) StartSDWAN() {
	// request peer info
	pn.sdwan = &p2pSDWAN{}
}

func (pn *P2PNetwork) ConnectNode(node string) error {
	if gConf.nodeID() <= NodeNameToID(node) {
		return errors.New("only the bigger nodeid connect")
	}
	peerNodeID := fmt.Sprintf("%d", NodeNameToID(node))
	config := AppConfig{Enabled: 1}
	config.AppName = peerNodeID
	config.SrcPort = 0
	config.PeerNode = node
	sdwan := gConf.getSDWAN()
	config.PunchPriority = int(sdwan.PunchPriority)
	// config.UnderlayProtocol = "kcp"
	if node != sdwan.CentralNode && gConf.Network.Node != sdwan.CentralNode { // neither is centralnode
		config.RelayNode = sdwan.CentralNode
		config.ForceRelay = int(sdwan.ForceRelay)
		if sdwan.Mode == SDWANModeCentral {
			config.ForceRelay = 1
		}
	}

	gConf.add(config, true)
	return nil
}

func (pn *P2PNetwork) WriteNode(nodeID uint64, buff []byte) error {
	i, ok := pn.apps.Load(nodeID)
	if !ok {
		return errors.New("peer not found")
	}
	var err error
	app := i.(*p2pApp)
	// TODO: move to app.write
	err = app.WriteNodeDataMP(buff)
	if err != nil {
		gLog.dev("appID:%d WriteNodeDataMP %s", app.id, err)
	}
	// gLog.dev("%d tunnel write node data bodylen=%d, relay=%t", app.Tunnel().id, len(buff), !app.isDirect())
	// if app.DirectTunnel() != nil { // direct
	// 	app.Tunnel().asyncWriteNodeData(MsgP2P, MsgNodeData, buff)
	// }
	// if app.Tunnel() != nil { // relay
	// 	fromNodeIDHead := new(bytes.Buffer)
	// 	binary.Write(fromNodeIDHead, binary.LittleEndian, gConf.nodeID())
	// 	all := app.RelayHead().Bytes()
	// 	all = append(all, encodeHeader(MsgP2P, MsgRelayNodeData, uint32(len(buff)+overlayHeaderSize))...)
	// 	all = append(all, fromNodeIDHead.Bytes()...)
	// 	all = append(all, buff...)
	// 	app.Tunnel().asyncWriteNodeData(MsgP2P, MsgRelayData, all)
	// }

	return err
}

func (pn *P2PNetwork) WriteBroadcast(buff []byte) error {
	///
	pn.apps.Range(func(id, i interface{}) bool {
		// newDestIP := net.ParseIP("10.2.3.2")
		// copy(buff[16:20], newDestIP.To4())
		// binary.BigEndian.PutUint16(buff[10:12], 0) // set checksum=0 for calc checksum
		// ipChecksum := calculateChecksum(buff[0:20])
		// binary.BigEndian.PutUint16(buff[10:12], ipChecksum)
		// binary.BigEndian.PutUint16(buff[26:28], 0x082e)
		app := i.(*p2pApp)

		if app.config.SrcPort != 0 { // normal portmap app
			return true
		}
		if app.config.peerIP == gConf.Network.publicIP { // mostly in a lan
			return true
		}
		err := app.WriteNodeDataMP(buff)
		if err != nil {
			gLog.dev("appID:%d WriteNodeDataMP %s", app.id, err)
		}
		return true
	})
	return nil
}

func (pn *P2PNetwork) ReadNode(tm time.Duration) []byte {
	select {
	case nd := <-pn.nodeData:
		return nd
	case <-time.After(tm):
	}
	return nil
}

