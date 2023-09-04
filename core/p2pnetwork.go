package openp2p

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var (
	instance *P2PNetwork
	once     sync.Once
)

const (
	retryLimit    = 20
	retryInterval = 10 * time.Second
)

// golang not support float64 const
var (
	ma10 float64 = 1.0 / 10
	ma20 float64 = 1.0 / 20
)

type P2PNetwork struct {
	conn        *websocket.Conn
	online      bool
	running     bool
	restartCh   chan bool
	wgReconnect sync.WaitGroup
	writeMtx    sync.Mutex
	hbTime      time.Time
	// for sync server time
	t1    int64 // nanoSeconds
	dt    int64 // client faster then server dt nanoSeconds
	ddtma int64
	ddt   int64 // differential of dt
	// msgMap    sync.Map
	msgMap     map[uint64]chan pushMsg //key: nodeID
	msgMapMtx  sync.Mutex
	config     NetworkConfig
	allTunnels sync.Map
	apps       sync.Map //key: protocol+srcport; value: p2pApp
	limiter    *BandwidthLimiter
}

type pushMsg struct {
	data []byte
	ts   time.Time
}

const msgExpiredTime = time.Minute

func P2PNetworkInstance(config *NetworkConfig) *P2PNetwork {
	if instance == nil {
		once.Do(func() {
			instance = &P2PNetwork{
				restartCh: make(chan bool, 2),
				online:    false,
				running:   true,
				msgMap:    make(map[uint64]chan pushMsg),
				limiter:   newBandwidthLimiter(config.ShareBandwidth),
				dt:        0,
				ddt:       0,
			}
			instance.msgMap[0] = make(chan pushMsg) // for gateway
			if config != nil {
				instance.config = *config
			}
			instance.init()
			go instance.run()
			go func() {
				for {
					instance.refreshIPv6(false)
					time.Sleep(time.Hour)
				}
			}()
			cleanTempFiles()
		})
	}
	return instance
}

func (pn *P2PNetwork) run() {
	heartbeatTimer := time.NewTicker(NetworkHeartbeatTime)
	pn.t1 = time.Now().UnixNano()
	pn.write(MsgHeartbeat, 0, "")
	for pn.running {
		select {
		case <-heartbeatTimer.C:
			pn.t1 = time.Now().UnixNano()
			pn.write(MsgHeartbeat, 0, "")
		case <-pn.restartCh:
			pn.online = false
			pn.wgReconnect.Wait() // wait read/autorunapp goroutine end
			time.Sleep(ClientAPITimeout)
			err := pn.init()
			if err != nil {
				gLog.Println(LvERROR, "P2PNetwork init error:", err)
			}
		}
	}
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
	gConf.mtx.Lock() // lock for copy gConf.Apps and the modification of config(it's pointer)
	defer gConf.mtx.Unlock()
	allApps := gConf.Apps // read a copy, other thread will modify the gConf.Apps

	for _, config := range allApps {
		if config.nextRetryTime.After(time.Now()) || config.Enabled == 0 || config.retryNum >= retryLimit {
			continue
		}
		if config.AppName == "" {
			config.AppName = config.ID()
		}
		if i, ok := pn.apps.Load(config.ID()); ok {
			if app := i.(*p2pApp); app.isActive() {
				continue
			}
			pn.DeleteApp(*config)
		}

		if config.retryNum > 0 { // first time not show reconnect log
			gLog.Printf(LvINFO, "detect app %s disconnect, reconnecting the %d times...", config.AppName, config.retryNum)
			if time.Now().Add(-time.Minute * 15).After(config.retryTime) { // run normally 15min, reset retrynum
				config.retryNum = 0
			}
		}
		config.retryNum++
		config.retryTime = time.Now()
		config.nextRetryTime = time.Now().Add(retryInterval)
		config.connectTime = time.Now()
		config.peerToken = pn.config.Token
		gConf.mtx.Unlock() // AddApp will take a period of time, let outside modify gConf
		err := pn.AddApp(*config)
		gConf.mtx.Lock()
		if err != nil {
			config.errMsg = err.Error()
			if err == ErrPeerOffline { // stop retry, waiting for online
				config.retryNum = retryLimit
				gLog.Printf(LvINFO, " %s offline, it will auto reconnect when peer node online", config.PeerNode)
			}
		}
	}
}

func (pn *P2PNetwork) autorunApp() {
	gLog.Println(LvINFO, "autorunApp start")
	pn.wgReconnect.Add(1)
	defer pn.wgReconnect.Done()
	for pn.running && pn.online {
		time.Sleep(time.Second)
		pn.runAll()
	}
	gLog.Println(LvINFO, "autorunApp end")
}

func (pn *P2PNetwork) addRelayTunnel(config AppConfig) (*P2PTunnel, uint64, string, error) {
	gLog.Printf(LvINFO, "addRelayTunnel to %s start", config.PeerNode)
	defer gLog.Printf(LvINFO, "addRelayTunnel to %s end", config.PeerNode)
	// request a relay node or specify manually(TODO)
	relayConfig := config
	relayMode := "private"
	if config.RelayNode == "" {
		pn.write(MsgRelay, MsgRelayNodeReq, &RelayNodeReq{config.PeerNode})
		head, body := pn.read("", MsgRelay, MsgRelayNodeRsp, ClientAPITimeout)
		if head == nil {
			return nil, 0, "", errors.New("read MsgRelayNodeRsp error")
		}
		rsp := RelayNodeRsp{}
		if err := json.Unmarshal(body, &rsp); err != nil {
			return nil, 0, "", errors.New("unmarshal MsgRelayNodeRsp error")
		}
		if rsp.RelayName == "" || rsp.RelayToken == 0 {
			gLog.Printf(LvERROR, "MsgRelayNodeReq error")
			return nil, 0, "", errors.New("MsgRelayNodeReq error")
		}
		gLog.Printf(LvINFO, "got relay node:%s", rsp.RelayName)

		relayConfig.PeerNode = rsp.RelayName
		relayConfig.peerToken = rsp.RelayToken
		relayMode = rsp.Mode
	} else {
		relayConfig.PeerNode = config.RelayNode
	}
	///
	t, err := pn.addDirectTunnel(relayConfig, 0)
	if err != nil {
		gLog.Println(LvERROR, "direct connect error:", err)
		return nil, 0, "", ErrConnectRelayNode // relay offline will stop retry
	}
	// notify peer addRelayTunnel
	req := AddRelayTunnelReq{
		From:       pn.config.Node,
		RelayName:  relayConfig.PeerNode,
		RelayToken: relayConfig.peerToken,
	}
	gLog.Printf(LvINFO, "push relay %s---------%s", config.PeerNode, relayConfig.PeerNode)
	pn.push(config.PeerNode, MsgPushAddRelayTunnelReq, &req)

	// wait relay ready
	head, body := pn.read(config.PeerNode, MsgPush, MsgPushAddRelayTunnelRsp, PeerAddRelayTimeount)
	if head == nil {
		gLog.Printf(LvERROR, "read MsgPushAddRelayTunnelRsp error")
		return nil, 0, "", errors.New("read MsgPushAddRelayTunnelRsp error")
	}
	rspID := TunnelMsg{}
	if err = json.Unmarshal(body, &rspID); err != nil {
		return nil, 0, "", errors.New("peer connect relayNode error")
	}
	return t, rspID.ID, relayMode, err
}

// use *AppConfig to save status
func (pn *P2PNetwork) AddApp(config AppConfig) error {
	gLog.Printf(LvINFO, "addApp %s to %s:%s:%d start", config.AppName, config.PeerNode, config.DstHost, config.DstPort)
	defer gLog.Printf(LvINFO, "addApp %s to %s:%s:%d end", config.AppName, config.PeerNode, config.DstHost, config.DstPort)
	if !pn.online {
		return errors.New("P2PNetwork offline")
	}
	// check if app already exist?
	appExist := false
	_, ok := pn.apps.Load(config.ID())
	if ok {
		appExist = true
	}
	if appExist {
		return errors.New("P2PApp already exist")
	}
	appID := rand.Uint64()
	appKey := uint64(0)
	var rtid uint64
	relayNode := ""
	relayMode := ""
	peerNatType := NATUnknown
	peerIP := ""
	errMsg := ""
	t, err := pn.addDirectTunnel(config, 0)
	if t != nil {
		peerNatType = t.config.peerNatType
		peerIP = t.config.peerIP
	}
	// TODO: if tcp failed, should try udp punching, nattype should refactor also, when NATNONE and failed we don't know the peerNatType

	if err != nil && err == ErrorHandshake {
		gLog.Println(LvERROR, "direct connect failed, try to relay")
		t, rtid, relayMode, err = pn.addRelayTunnel(config)
		if t != nil {
			relayNode = t.config.PeerNode
		}
	}

	if err != nil {
		errMsg = err.Error()
	}
	req := ReportConnect{
		Error:          errMsg,
		Protocol:       config.Protocol,
		SrcPort:        config.SrcPort,
		NatType:        pn.config.natType,
		PeerNode:       config.PeerNode,
		DstPort:        config.DstPort,
		DstHost:        config.DstHost,
		PeerNatType:    peerNatType,
		PeerIP:         peerIP,
		ShareBandwidth: pn.config.ShareBandwidth,
		RelayNode:      relayNode,
		Version:        OpenP2PVersion,
	}
	pn.write(MsgReport, MsgReportConnect, &req)
	if err != nil {
		return err
	}
	if rtid != 0 || t.conn.Protocol() == "tcp" {
		// sync appkey
		appKey = rand.Uint64()
		req := APPKeySync{
			AppID:  appID,
			AppKey: appKey,
		}
		gLog.Printf(LvINFO, "sync appkey to %s", config.PeerNode)
		pn.push(config.PeerNode, MsgPushAPPKey, &req)
	}
	app := p2pApp{
		id:        appID,
		key:       appKey,
		tunnel:    t,
		config:    config,
		iptree:    NewIPTree(config.Whitelist),
		rtid:      rtid,
		relayNode: relayNode,
		relayMode: relayMode,
		hbTime:    time.Now()}
	pn.apps.Store(config.ID(), &app)
	gLog.Printf(LvINFO, "%s use tunnel %d", app.config.AppName, app.tunnel.id)
	if err == nil {
		go app.listen()
	}
	return err
}

func (pn *P2PNetwork) DeleteApp(config AppConfig) {
	gLog.Printf(LvINFO, "DeleteApp %s%d start", config.Protocol, config.SrcPort)
	defer gLog.Printf(LvINFO, "DeleteApp %s%d end", config.Protocol, config.SrcPort)
	// close the apps of this config
	i, ok := pn.apps.Load(config.ID())
	if ok {
		app := i.(*p2pApp)
		gLog.Printf(LvINFO, "app %s exist, delete it", app.config.AppName)
		app.close()
		pn.apps.Delete(config.ID())
	}
}

func (pn *P2PNetwork) findTunnel(config *AppConfig) (t *P2PTunnel) {
	// find existing tunnel to peer
	pn.allTunnels.Range(func(id, i interface{}) bool {
		tmpt := i.(*P2PTunnel)
		if tmpt.config.PeerNode == config.PeerNode {
			gLog.Println(LvINFO, "tunnel already exist ", config.PeerNode)
			isActive := tmpt.checkActive()
			// inactive, close it
			if !isActive {
				gLog.Println(LvINFO, "but it's not active, close it ", config.PeerNode)
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
	gLog.Printf(LvDEBUG, "addDirectTunnel %s%d to %s:%s:%d start", config.Protocol, config.SrcPort, config.PeerNode, config.DstHost, config.DstPort)
	defer gLog.Printf(LvDEBUG, "addDirectTunnel %s%d to %s:%s:%d end", config.Protocol, config.SrcPort, config.PeerNode, config.DstHost, config.DstPort)
	isClient := false
	// client side tid=0, assign random uint64
	if tid == 0 {
		tid = rand.Uint64()
		isClient = true
	}

	pn.msgMapMtx.Lock()
	pn.msgMap[nodeNameToID(config.PeerNode)] = make(chan pushMsg, 50)
	pn.msgMapMtx.Unlock()
	// server side
	if !isClient {
		t, err = pn.newTunnel(config, tid, isClient)
		return t, err // always return
	}
	// client side
	// peer info
	initErr := pn.requestPeerInfo(&config)
	if initErr != nil {
		gLog.Println(LvERROR, "init error:", initErr)

		return nil, initErr
	}
	// try TCP6
	if IsIPv6(config.peerIPv6) && IsIPv6(gConf.IPv6()) {
		gLog.Println(LvINFO, "try TCP6")
		config.linkMode = LinkModeTCP6
		config.isUnderlayServer = 0
		if t, err = pn.newTunnel(config, tid, isClient); err == nil {
			return t, nil
		}
	}

	// TODO: try UDP6

	// try TCP4
	if config.hasIPv4 == 1 || pn.config.hasIPv4 == 1 || config.hasUPNPorNATPMP == 1 || pn.config.hasUPNPorNATPMP == 1 {
		gLog.Println(LvINFO, "try TCP4")
		config.linkMode = LinkModeTCP4
		if config.hasIPv4 == 1 || config.hasUPNPorNATPMP == 1 {
			config.isUnderlayServer = 0
		} else {
			config.isUnderlayServer = 1
		}
		if t, err = pn.newTunnel(config, tid, isClient); err == nil {
			return t, nil
		}
	}
	// TODO: try UDP4

	// try TCPPunch
	for i := 0; i < Cone2ConePunchMaxRetry; i++ { // when both 2 nats has restrict firewall, simultaneous punching needs to be very precise, it takes a few tries
		if config.peerNatType == NATCone && pn.config.natType == NATCone { // TODO: support c2s
			gLog.Println(LvINFO, "try TCP4 Punch")
			config.linkMode = LinkModeTCPPunch
			config.isUnderlayServer = 0
			if t, err = pn.newTunnel(config, tid, isClient); err == nil {
				gLog.Println(LvINFO, "TCP4 Punch ok")
				return t, nil
			}
		}
	}

	// try UDPPunch
	for i := 0; i < Cone2ConePunchMaxRetry; i++ { // when both 2 nats has restrict firewall, simultaneous punching needs to be very precise, it takes a few tries
		if config.peerNatType == NATCone || pn.config.natType == NATCone {
			gLog.Println(LvINFO, "try UDP4 Punch")
			config.linkMode = LinkModeUDPPunch
			config.isUnderlayServer = 0
			if t, err = pn.newTunnel(config, tid, isClient); err == nil {
				return t, nil
			}
		}
		if !(config.peerNatType == NATCone && pn.config.natType == NATCone) { // not cone2cone, no more try
			break
		}
	}
	return nil, ErrorHandshake // only ErrorHandshake will try relay
}

func (pn *P2PNetwork) newTunnel(config AppConfig, tid uint64, isClient bool) (t *P2PTunnel, err error) {
	if isClient { // only client side find existing tunnel
		if existTunnel := pn.findTunnel(&config); existTunnel != nil {
			return existTunnel, nil
		}
	}

	t = &P2PTunnel{pn: pn,
		config: config,
		id:     tid,
	}
	t.initPort()
	if isClient {
		if err = t.connect(); err != nil {
			gLog.Println(LvERROR, "p2pTunnel connect error:", err)
			return
		}
	} else {
		if err = t.listen(); err != nil {
			gLog.Println(LvERROR, "p2pTunnel listen error:", err)
			return
		}
	}
	// store it when success
	gLog.Printf(LvDEBUG, "store tunnel %d", tid)
	pn.allTunnels.Store(tid, t)
	return
}
func (pn *P2PNetwork) init() error {
	gLog.Println(LvINFO, "init start")
	pn.wgReconnect.Add(1)
	defer pn.wgReconnect.Done()
	var err error
	for {
		// detect nat type
		pn.config.publicIP, pn.config.natType, pn.config.hasIPv4, pn.config.hasUPNPorNATPMP, err = getNATType(pn.config.ServerHost, pn.config.UDPPort1, pn.config.UDPPort2)
		// for testcase
		if strings.Contains(pn.config.Node, "openp2pS2STest") {
			pn.config.natType = NATSymmetric
			pn.config.hasIPv4 = 0
			pn.config.hasUPNPorNATPMP = 0
			gLog.Println(LvINFO, "openp2pS2STest debug")

		}
		if strings.Contains(pn.config.Node, "openp2pC2CTest") {
			pn.config.natType = NATCone
			pn.config.hasIPv4 = 0
			pn.config.hasUPNPorNATPMP = 0
			gLog.Println(LvINFO, "openp2pC2CTest debug")
		}
		if err != nil {
			gLog.Println(LvDEBUG, "detect NAT type error:", err)
			break
		}
		gLog.Println(LvDEBUG, "detect NAT type:", pn.config.natType, " publicIP:", pn.config.publicIP)
		gatewayURL := fmt.Sprintf("%s:%d", pn.config.ServerHost, pn.config.ServerPort)
		uri := "/api/v1/login"
		caCertPool, err := x509.SystemCertPool()
		if err != nil {
			gLog.Println(LvERROR, "Failed to load system root CAs:", err)
		} else {
			caCertPool = x509.NewCertPool()
		}
		caCertPool.AppendCertsFromPEM([]byte(rootCA))
		caCertPool.AppendCertsFromPEM([]byte(ISRGRootX1))
		config := tls.Config{
			RootCAs:            caCertPool,
			InsecureSkipVerify: false} // let's encrypt root cert "DST Root CA X3" expired at 2021/09/29. many old system(windows server 2008 etc) will not trust our cert
		websocket.DefaultDialer.TLSClientConfig = &config
		u := url.URL{Scheme: "wss", Host: gatewayURL, Path: uri}
		q := u.Query()
		q.Add("node", pn.config.Node)
		q.Add("token", fmt.Sprintf("%d", pn.config.Token))
		q.Add("version", OpenP2PVersion)
		q.Add("nattype", fmt.Sprintf("%d", pn.config.natType))
		q.Add("sharebandwidth", fmt.Sprintf("%d", pn.config.ShareBandwidth))
		u.RawQuery = q.Encode()
		var ws *websocket.Conn
		ws, _, err = websocket.DefaultDialer.Dial(u.String(), nil)
		if err != nil {
			break
		}
		pn.online = true
		pn.conn = ws
		localAddr := strings.Split(ws.LocalAddr().String(), ":")
		if len(localAddr) == 2 {
			pn.config.localIP = localAddr[0]
		} else {
			err = errors.New("get local ip failed")
			break
		}
		go pn.readLoop()
		pn.config.mac = getmac(pn.config.localIP)
		pn.config.os = getOsName()
		go func() {
			req := ReportBasic{
				Mac:             pn.config.mac,
				LanIP:           pn.config.localIP,
				OS:              pn.config.os,
				HasIPv4:         pn.config.hasIPv4,
				HasUPNPorNATPMP: pn.config.hasUPNPorNATPMP,
				Version:         OpenP2PVersion,
			}
			rsp := netInfo()
			gLog.Println(LvDEBUG, "netinfo:", rsp)
			if rsp != nil && rsp.Country != "" {
				if IsIPv6(rsp.IP.String()) {
					gConf.setIPv6(rsp.IP.String())
				}
				req.NetInfo = *rsp
			} else {
				pn.refreshIPv6(true)
			}
			req.IPv6 = gConf.IPv6()
			pn.write(MsgReport, MsgReportBasic, &req)
		}()
		go pn.autorunApp()
		gLog.Println(LvDEBUG, "P2PNetwork init ok")
		break
	}
	if err != nil {
		// init failed, retry
		pn.restartCh <- true
		gLog.Println(LvERROR, "P2PNetwork init error:", err)
	}
	return err
}

func (pn *P2PNetwork) handleMessage(t int, msg []byte) {
	head := openP2PHeader{}
	err := binary.Read(bytes.NewReader(msg[:openP2PHeaderSize]), binary.LittleEndian, &head)
	if err != nil {
		gLog.Println(LvERROR, "handleMessage error:", err)
		return
	}
	switch head.MainType {
	case MsgLogin:
		// gLog.Println(LevelINFO,string(msg))
		rsp := LoginRsp{}
		if err = json.Unmarshal(msg[openP2PHeaderSize:], &rsp); err != nil {
			gLog.Printf(LvERROR, "wrong %v:%s", reflect.TypeOf(rsp), err)
			return
		}
		if rsp.Error != 0 {
			gLog.Printf(LvERROR, "login error:%d, detail:%s", rsp.Error, rsp.Detail)
			pn.running = false
		} else {
			pn.config.Token = rsp.Token
			pn.config.User = rsp.User
			gConf.setToken(rsp.Token)
			gConf.setUser(rsp.User)
			if len(rsp.Node) >= MinNodeNameLen {
				gConf.setNode(rsp.Node)
				pn.config.Node = rsp.Node
			}
			gLog.Printf(LvINFO, "login ok. user=%s,node=%s", rsp.User, rsp.Node)
		}
	case MsgHeartbeat:
		gLog.Printf(LvDEBUG, "P2PNetwork heartbeat ok")
		pn.hbTime = time.Now()
		rtt := pn.hbTime.UnixNano() - pn.t1
		t2 := int64(binary.LittleEndian.Uint64(msg[openP2PHeaderSize : openP2PHeaderSize+8]))
		dt := pn.t1 + rtt/2 - t2
		if pn.dt != 0 {
			ddt := dt - pn.dt
			if pn.ddtma > 0 && (ddt > (pn.ddtma+pn.ddtma/3) || ddt < (pn.ddtma-pn.ddtma/3)) {
				newdt := pn.dt + pn.ddtma
				gLog.Printf(LvDEBUG, "server time auto adjust dt=%.2fms to %.2fms", float64(dt)/float64(time.Millisecond), float64(newdt)/float64(time.Millisecond))
				dt = newdt
			}
			pn.ddt = ddt
			if pn.ddtma == 0 {
				pn.ddtma = pn.ddt
			} else {
				pn.ddtma = int64(float64(pn.ddtma)*(1-ma10) + float64(pn.ddt)*ma10) // avoid int64 overflow
			}
		}
		pn.dt = dt

		gLog.Printf(LvDEBUG, "server time dt=%dms ddt=%dns ddtma=%dns rtt=%dms ", pn.dt/int64(time.Millisecond), pn.ddt, pn.ddtma, rtt/int64(time.Millisecond))
	case MsgPush:
		handlePush(pn, head.SubType, msg)
	default:
		pn.msgMapMtx.Lock()
		ch := pn.msgMap[0]
		pn.msgMapMtx.Unlock()
		ch <- pushMsg{data: msg, ts: time.Now()}
		return
	}
}

func (pn *P2PNetwork) readLoop() {
	gLog.Printf(LvDEBUG, "P2PNetwork readLoop start")
	pn.wgReconnect.Add(1)
	defer pn.wgReconnect.Done()
	for pn.running {
		pn.conn.SetReadDeadline(time.Now().Add(NetworkHeartbeatTime + 10*time.Second))
		t, msg, err := pn.conn.ReadMessage()
		if err != nil {
			gLog.Printf(LvERROR, "P2PNetwork read error:%s", err)
			pn.conn.Close()
			pn.restartCh <- true
			break
		}
		pn.handleMessage(t, msg)
	}
	gLog.Printf(LvDEBUG, "P2PNetwork readLoop end")
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
	if err = pn.conn.WriteMessage(websocket.BinaryMessage, msg); err != nil {
		gLog.Printf(LvERROR, "write msgType %d,%d error:%s", mainType, subType, err)
		pn.conn.Close()
	}
	return err
}

func (pn *P2PNetwork) relay(to uint64, body []byte) error {
	gLog.Printf(LvDEBUG, "relay data to %d", to)
	i, ok := pn.allTunnels.Load(to)
	if !ok {
		return nil
	}
	tunnel := i.(*P2PTunnel)
	if tunnel.config.shareBandwidth > 0 {
		pn.limiter.Add(len(body))
	}
	tunnel.conn.WriteBuffer(body)
	return nil
}

func (pn *P2PNetwork) push(to string, subType uint16, packet interface{}) error {
	gLog.Printf(LvDEBUG, "push msgType %d to %s", subType, to)
	if !pn.online {
		return errors.New("client offline")
	}
	pushHead := PushHeader{}
	pushHead.From = nodeNameToID(pn.config.Node)
	pushHead.To = nodeNameToID(to)
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
	if err = pn.conn.WriteMessage(websocket.BinaryMessage, pushMsg); err != nil {
		gLog.Printf(LvERROR, "push to %s error:%s", to, err)
		pn.conn.Close()
	}
	return err
}

func (pn *P2PNetwork) read(node string, mainType uint16, subType uint16, timeout time.Duration) (head *openP2PHeader, body []byte) {
	var nodeID uint64
	if node == "" {
		nodeID = 0
	} else {
		nodeID = nodeNameToID(node)
	}
	pn.msgMapMtx.Lock()
	ch := pn.msgMap[nodeID]
	pn.msgMapMtx.Unlock()
	for {
		select {
		case <-time.After(timeout):
			gLog.Printf(LvERROR, "wait msg%d:%d timeout", mainType, subType)
			return
		case msg := <-ch:
			if msg.ts.Before(time.Now().Add(-msgExpiredTime)) {
				gLog.Printf(LvDEBUG, "msg expired error %d:%d", head.MainType, head.SubType)
				continue
			}
			head = &openP2PHeader{}
			err := binary.Read(bytes.NewReader(msg.data[:openP2PHeaderSize]), binary.LittleEndian, head)
			if err != nil {
				gLog.Println(LvERROR, "read msg error:", err)
				break
			}
			if head.MainType != mainType || head.SubType != subType {
				gLog.Printf(LvDEBUG, "read msg type error %d:%d, requeue it", head.MainType, head.SubType)
				ch <- msg
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

func (pn *P2PNetwork) updateAppHeartbeat(appID uint64) {
	pn.apps.Range(func(id, i interface{}) bool {
		app := i.(*p2pApp)
		if app.id != appID {
			return true
		}
		app.updateHeartbeat()
		return false
	})
}

// ipv6 will expired need to refresh.
func (pn *P2PNetwork) refreshIPv6(force bool) {
	if !force && !IsIPv6(gConf.IPv6()) { // not support ipv6, not refresh
		return
	}
	for i := 0; i < 3; i++ {
		client := &http.Client{Timeout: time.Second * 10}
		r, err := client.Get("http://6.ipw.cn")
		if err != nil {
			gLog.Println(LvDEBUG, "refreshIPv6 error:", err)
			continue
		}
		defer r.Body.Close()
		buf := make([]byte, 1024)
		n, err := r.Body.Read(buf)
		if n <= 0 {
			gLog.Println(LvINFO, "refreshIPv6 error:", err, n)
			continue
		}
		gConf.setIPv6(string(buf[:n]))
		break
	}

}

func (pn *P2PNetwork) requestPeerInfo(config *AppConfig) error {
	// request peer info
	pn.write(MsgQuery, MsgQueryPeerInfoReq, &QueryPeerInfoReq{config.peerToken, config.PeerNode})
	head, body := pn.read("", MsgQuery, MsgQueryPeerInfoRsp, ClientAPITimeout)
	if head == nil {
		return ErrNetwork // network error, should not be ErrPeerOffline
	}
	rsp := QueryPeerInfoRsp{}
	if err := json.Unmarshal(body, &rsp); err != nil {
		return ErrMsgFormat
	}
	if rsp.Online == 0 {
		return ErrPeerOffline
	}
	if compareVersion(rsp.Version, LeastSupportVersion) == LESS {
		return ErrVersionNotCompatible
	}
	config.peerVersion = rsp.Version
	config.hasIPv4 = rsp.HasIPv4
	config.peerIP = rsp.IPv4
	config.peerIPv6 = rsp.IPv6
	config.hasUPNPorNATPMP = rsp.HasUPNPorNATPMP
	config.peerNatType = rsp.NatType
	///
	return nil
}
