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
	v4l            *v4Listener
	instance       *P2PNetwork
	onceP2PNetwork sync.Once
	onceV4Listener sync.Once
)

const (
	retryLimit                  = 20
	retryInterval               = 10 * time.Second
	DefaultLoginMaxDelaySeconds = 60
)

// golang not support float64 const
var (
	ma10 float64 = 1.0 / 10
	ma5  float64 = 1.0 / 5
)

type NodeData struct {
	NodeID uint64
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
	// for sync server time
	t1     int64 // nanoSeconds
	preRtt int64 // nanoSeconds
	dt     int64 // client faster then server dt nanoSeconds
	ddtma  int64
	ddt    int64    // differential of dt
	msgMap sync.Map //key: nodeID
	// msgMap     map[uint64]chan pushMsg //key: nodeID
	allTunnels           sync.Map // key: tid
	apps                 sync.Map //key: config.ID(); value: *p2pApp
	limiter              *SpeedLimiter
	nodeData             chan *NodeData
	sdwan                *p2pSDWAN
	tunnelCloseCh        chan *P2PTunnel
	loginMaxDelaySeconds int
}

type msgCtx struct {
	data []byte
	ts   time.Time
}

func P2PNetworkInstance() *P2PNetwork {
	if instance == nil {
		onceP2PNetwork.Do(func() {
			instance = &P2PNetwork{
				restartCh:            make(chan bool, 1),
				tunnelCloseCh:        make(chan *P2PTunnel, 100),
				nodeData:             make(chan *NodeData, 10000),
				online:               false,
				running:              true,
				limiter:              newSpeedLimiter(gConf.Network.ShareBandwidth*1024*1024/8, 1),
				dt:                   0,
				ddt:                  0,
				loginMaxDelaySeconds: DefaultLoginMaxDelaySeconds,
			}
			instance.msgMap.Store(uint64(0), make(chan msgCtx, 50)) // for gateway
			instance.StartSDWAN()
			instance.init()
			go instance.run()
			go func() {
				for {
					instance.refreshIPv6()
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
	for {
		select {
		case <-heartbeatTimer.C:
			pn.t1 = time.Now().UnixNano()
			pn.write(MsgHeartbeat, 0, "")
		case <-pn.restartCh:
			gLog.Printf(LvDEBUG, "got restart channel")
			GNetwork.sdwan.reset()
			pn.online = false
			pn.wgReconnect.Wait() // wait read/autorunapp goroutine end
			delay := ClientAPITimeout + time.Duration(rand.Int()%pn.loginMaxDelaySeconds)*time.Second
			time.Sleep(delay)
			err := pn.init()
			if err != nil {
				gLog.Println(LvERROR, "P2PNetwork init error:", err)
			}
			gConf.retryAllApp()

		case t := <-pn.tunnelCloseCh:
			gLog.Printf(LvDEBUG, "got tunnelCloseCh %s", t.config.LogPeerNode())
			pn.apps.Range(func(id, i interface{}) bool {
				app := i.(*p2pApp)
				if app.DirectTunnel() == t {
					app.setDirectTunnel(nil)
				}
				if app.RelayTunnel() == t {
					app.setRelayTunnel(nil)
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
	gConf.mtx.Lock() // lock for copy gConf.Apps and the modification of config(it's pointer)
	defer gConf.mtx.Unlock()
	allApps := gConf.Apps // read a copy, other thread will modify the gConf.Apps
	for _, config := range allApps {
		if config.AppName == "" {
			config.AppName = fmt.Sprintf("%d", config.ID())
		}
		if config.Enabled == 0 {
			continue
		}
		if _, ok := pn.apps.Load(config.ID()); ok {
			continue
		}

		config.peerToken = gConf.Network.Token
		gConf.mtx.Unlock() // AddApp will take a period of time, let outside modify gConf
		pn.AddApp(*config)
		gConf.mtx.Lock()

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
	gLog.Printf(LvINFO, "addRelayTunnel to %s start", config.LogPeerNode())
	defer gLog.Printf(LvINFO, "addRelayTunnel to %s end", config.LogPeerNode())
	relayConfig := AppConfig{
		PeerNode:  config.RelayNode,
		peerToken: config.peerToken,
		relayMode: "private"}
	if relayConfig.PeerNode == "" {
		// find existing relay tunnel
		pn.apps.Range(func(id, i interface{}) bool {
			app := i.(*p2pApp)
			if app.config.PeerNode != config.PeerNode {
				return true
			}
			if app.RelayTunnel() == nil {
				return true
			}
			relayConfig.PeerNode = app.RelayTunnel().config.PeerNode
			gLog.Printf(LvDEBUG, "found existing relay tunnel %s", relayConfig.LogPeerNode())
			return false
		})
		if relayConfig.PeerNode == "" { // request relay node
			pn.reqGatewayMtx.Lock()
			pn.write(MsgRelay, MsgRelayNodeReq, &RelayNodeReq{config.PeerNode})
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
				gLog.Printf(LvERROR, "MsgRelayNodeReq error")
				return nil, 0, "", errors.New("MsgRelayNodeReq error")
			}
			gLog.Printf(LvDEBUG, "got relay node:%s", relayConfig.LogPeerNode())

			relayConfig.PeerNode = rsp.RelayName
			relayConfig.peerToken = rsp.RelayToken
			relayConfig.relayMode = rsp.Mode
		}

	}
	///
	t, err := pn.addDirectTunnel(relayConfig, 0)
	if err != nil {
		gLog.Println(LvERROR, "direct connect error:", err)
		return nil, 0, "", ErrConnectRelayNode // relay offline will stop retry
	}
	// notify peer addRelayTunnel
	req := AddRelayTunnelReq{
		From:          gConf.Network.Node,
		RelayName:     relayConfig.PeerNode,
		RelayToken:    relayConfig.peerToken,
		RelayMode:     relayConfig.relayMode,
		RelayTunnelID: t.id,
	}
	gLog.Printf(LvDEBUG, "push %s the relay node(%s)", config.LogPeerNode(), relayConfig.LogPeerNode())
	pn.push(config.PeerNode, MsgPushAddRelayTunnelReq, &req)

	// wait relay ready
	head, body := pn.read(config.PeerNode, MsgPush, MsgPushAddRelayTunnelRsp, PeerAddRelayTimeount)
	if head == nil {
		gLog.Printf(LvERROR, "read MsgPushAddRelayTunnelRsp error")
		return nil, 0, "", errors.New("read MsgPushAddRelayTunnelRsp error")
	}
	rspID := TunnelMsg{}
	if err = json.Unmarshal(body, &rspID); err != nil {
		gLog.Println(LvDEBUG, ErrPeerConnectRelay)
		return nil, 0, "", ErrPeerConnectRelay
	}
	return t, rspID.ID, relayConfig.relayMode, err
}

// use *AppConfig to save status
func (pn *P2PNetwork) AddApp(config AppConfig) error {
	gLog.Printf(LvINFO, "addApp %s to %s:%s:%d start", config.AppName, config.LogPeerNode(), config.DstHost, config.DstPort)
	defer gLog.Printf(LvINFO, "addApp %s to %s:%s:%d end", config.AppName, config.LogPeerNode(), config.DstHost, config.DstPort)
	if !pn.online {
		return errors.New("P2PNetwork offline")
	}
	if _, ok := pn.msgMap.Load(NodeNameToID(config.PeerNode)); !ok {
		pn.msgMap.Store(NodeNameToID(config.PeerNode), make(chan msgCtx, 50))
	}
	// check if app already exist?
	if _, ok := pn.apps.Load(config.ID()); ok {
		return errors.New("P2PApp already exist")
	}

	app := p2pApp{
		// tunnel:    t,
		id:          rand.Uint64(),
		key:         rand.Uint64(),
		config:      config,
		iptree:      NewIPTree(config.Whitelist),
		running:     true,
		hbTimeRelay: time.Now(),
	}
	if _, ok := pn.msgMap.Load(NodeNameToID(config.PeerNode)); !ok {
		pn.msgMap.Store(NodeNameToID(config.PeerNode), make(chan msgCtx, 50))
	}
	pn.apps.Store(config.ID(), &app)
	gLog.Printf(LvDEBUG, "Store app %d", config.ID())
	go app.checkP2PTunnel()
	return nil
}

func (pn *P2PNetwork) DeleteApp(config AppConfig) {
	gLog.Printf(LvINFO, "DeleteApp %s to %s:%s:%d start", config.AppName, config.LogPeerNode(), config.DstHost, config.DstPort)
	defer gLog.Printf(LvINFO, "DeleteApp %s to %s:%s:%d end", config.AppName, config.LogPeerNode(), config.DstHost, config.DstPort)
	// close the apps of this config
	i, ok := pn.apps.Load(config.ID())
	if ok {
		app := i.(*p2pApp)
		gLog.Printf(LvINFO, "app %s exist, delete it", app.config.AppName)
		app.close()
		pn.apps.Delete(config.ID())
	}
}

func (pn *P2PNetwork) findTunnel(peerNode string) (t *P2PTunnel) {
	t = nil
	// find existing tunnel to peer
	pn.allTunnels.Range(func(id, i interface{}) bool {
		tmpt := i.(*P2PTunnel)
		if tmpt.config.PeerNode == peerNode {
			gLog.Println(LvINFO, "tunnel already exist ", peerNode)
			isActive := tmpt.checkActive()
			// inactive, close it
			if !isActive {
				gLog.Println(LvINFO, "but it's not active, close it ", peerNode)
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
	gLog.Printf(LvDEBUG, "addDirectTunnel %s%d to %s:%s:%d tid:%d start", config.Protocol, config.SrcPort, config.LogPeerNode(), config.DstHost, config.DstPort, tid)
	defer gLog.Printf(LvDEBUG, "addDirectTunnel %s%d to %s:%s:%d tid:%d end", config.Protocol, config.SrcPort, config.LogPeerNode(), config.DstHost, config.DstPort, tid)
	isClient := false
	// client side tid=0, assign random uint64
	if tid == 0 {
		tid = rand.Uint64()
		isClient = true
	}
	if _, ok := pn.msgMap.Load(NodeNameToID(config.PeerNode)); !ok {
		pn.msgMap.Store(NodeNameToID(config.PeerNode), make(chan msgCtx, 50))
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
		gLog.Printf(LvERROR, "%s init error:%s", config.LogPeerNode(), initErr)

		return nil, initErr
	}
	gLog.Printf(LvDEBUG, "config.peerNode=%s,config.peerVersion=%s,config.peerIP=%s,config.peerLanIP=%s,gConf.Network.publicIP=%s,config.peerIPv6=%s,config.hasIPv4=%d,config.hasUPNPorNATPMP=%d,gConf.Network.hasIPv4=%d,gConf.Network.hasUPNPorNATPMP=%d,config.peerNatType=%d,gConf.Network.natType=%d,",
		config.LogPeerNode(), config.peerVersion, config.peerIP, config.peerLanIP, gConf.Network.publicIP, config.peerIPv6, config.hasIPv4, config.hasUPNPorNATPMP, gConf.Network.hasIPv4, gConf.Network.hasUPNPorNATPMP, config.peerNatType, gConf.Network.natType)
	// try Intranet
	if config.peerIP == gConf.Network.publicIP && compareVersion(config.peerVersion, SupportIntranetVersion) >= 0 { // old version client has no peerLanIP
		gLog.Println(LvINFO, "try Intranet")
		config.linkMode = LinkModeIntranet
		config.isUnderlayServer = 0
		if t, err = pn.newTunnel(config, tid, isClient); err == nil {
			return t, nil
		}
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

	// try UDP6? maybe no

	// try TCP4
	if config.hasIPv4 == 1 || gConf.Network.hasIPv4 == 1 || config.hasUPNPorNATPMP == 1 || gConf.Network.hasUPNPorNATPMP == 1 {
		gLog.Println(LvINFO, "try TCP4")
		config.linkMode = LinkModeTCP4
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
		if config.PunchPriority&PunchPriorityUDPDisable != 0 {
			return
		}
		// try UDPPunch
		for i := 0; i < Cone2ConeUDPPunchMaxRetry; i++ { // when both 2 nats has restrict firewall, simultaneous punching needs to be very precise, it takes a few tries
			if config.peerNatType == NATCone || gConf.Network.natType == NATCone {
				gLog.Println(LvINFO, "try UDP4 Punch")
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
		if config.PunchPriority&PunchPriorityTCPDisable != 0 {
			return
		}
		// try TCPPunch
		for i := 0; i < Cone2ConeTCPPunchMaxRetry; i++ { // when both 2 nats has restrict firewall, simultaneous punching needs to be very precise, it takes a few tries
			if config.peerNatType == NATCone || gConf.Network.natType == NATCone {
				gLog.Println(LvINFO, "try TCP4 Punch")
				config.linkMode = LinkModeTCPPunch
				config.isUnderlayServer = 0
				if t, err = pn.newTunnel(config, tid, isClient); err == nil {
					gLog.Println(LvINFO, "TCP4 Punch ok")
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
	if isClient { // only client side find existing tunnel
		if existTunnel := pn.findTunnel(config.PeerNode); existTunnel != nil {
			return existTunnel, nil
		}
	}

	t = &P2PTunnel{
		config:         config,
		id:             tid,
		writeData:      make(chan []byte, WriteDataChanSize),
		writeDataSmall: make(chan []byte, WriteDataChanSize/30),
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
	gLog.Println(LvINFO, "P2PNetwork init start")
	defer gLog.Println(LvINFO, "P2PNetwork init end")
	pn.wgReconnect.Add(1)
	defer pn.wgReconnect.Done()
	var err error
	for {
		// detect nat type
		gConf.Network.publicIP, gConf.Network.natType, err = getNATType(gConf.Network.ServerHost, gConf.Network.UDPPort1, gConf.Network.UDPPort2)
		if err != nil {
			gLog.Println(LvDEBUG, "detect NAT type error:", err)
			break
		}
		if gConf.Network.hasIPv4 == 0 && gConf.Network.hasUPNPorNATPMP == 0 { // if already has ipv4 or upnp no need test again
			gConf.Network.hasIPv4, gConf.Network.hasUPNPorNATPMP = publicIPTest(gConf.Network.publicIP, gConf.Network.TCPPort)
		}

		// for testcase
		if strings.Contains(gConf.Network.Node, "openp2pS2STest") {
			gConf.Network.natType = NATSymmetric
			gConf.Network.hasIPv4 = 0
			gConf.Network.hasUPNPorNATPMP = 0
			gLog.Println(LvINFO, "openp2pS2STest debug")

		}
		if strings.Contains(gConf.Network.Node, "openp2pC2CTest") {
			gConf.Network.natType = NATCone
			gConf.Network.hasIPv4 = 0
			gConf.Network.hasUPNPorNATPMP = 0
			gLog.Println(LvINFO, "openp2pC2CTest debug")
		}

		if gConf.Network.hasIPv4 == 1 || gConf.Network.hasUPNPorNATPMP == 1 {
			onceV4Listener.Do(func() {
				v4l = &v4Listener{port: gConf.Network.TCPPort}
				go v4l.start()
			})
		}
		gLog.Printf(LvINFO, "hasIPv4:%d, UPNP:%d, NAT type:%d, publicIP:%s", gConf.Network.hasIPv4, gConf.Network.hasUPNPorNATPMP, gConf.Network.natType, gConf.Network.publicIP)
		gatewayURL := fmt.Sprintf("%s:%d", gConf.Network.ServerHost, gConf.Network.ServerPort)
		uri := "/api/v1/login"
		caCertPool, errCert := x509.SystemCertPool()
		if errCert != nil {
			gLog.Println(LvERROR, "Failed to load system root CAs:", errCert)
			caCertPool = x509.NewCertPool()
		}
		caCertPool.AppendCertsFromPEM([]byte(rootCA))
		caCertPool.AppendCertsFromPEM([]byte(ISRGRootX1))
		config := tls.Config{
			RootCAs:            caCertPool,
			InsecureSkipVerify: false} // let's encrypt root cert "DST Root CA X3" expired at 2021/09/29. many old system(windows server 2008 etc) will not trust our cert
		websocket.DefaultDialer.TLSClientConfig = &config
		websocket.DefaultDialer.HandshakeTimeout = ClientAPITimeout
		u := url.URL{Scheme: "wss", Host: gatewayURL, Path: uri}
		q := u.Query()
		q.Add("node", gConf.Network.Node)
		q.Add("token", fmt.Sprintf("%d", gConf.Network.Token))
		q.Add("version", OpenP2PVersion)
		q.Add("nattype", fmt.Sprintf("%d", gConf.Network.natType))
		q.Add("sharebandwidth", fmt.Sprintf("%d", gConf.Network.ShareBandwidth))
		u.RawQuery = q.Encode()
		var ws *websocket.Conn
		ws, _, err = websocket.DefaultDialer.Dial(u.String(), nil)
		if err != nil {
			gLog.Println(LvERROR, "Dial error:", err)
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
			gLog.Println(LvDEBUG, "netinfo:", rsp)
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
		gLog.Println(LvDEBUG, "P2PNetwork init ok")
		break
	}
	if err != nil {
		// init failed, retry
		pn.close()
		gLog.Println(LvERROR, "P2PNetwork init error:", err)
	}
	return err
}

func (pn *P2PNetwork) handleMessage(msg []byte) {
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
			gConf.setToken(rsp.Token)
			gConf.setUser(rsp.User)
			if len(rsp.Node) >= MinNodeNameLen {
				gConf.setNode(rsp.Node)
			}
			if rsp.LoginMaxDelay > 0 {
				pn.loginMaxDelaySeconds = rsp.LoginMaxDelay
			}
			gLog.Printf(LvINFO, "login ok. user=%s,node=%s", rsp.User, rsp.Node)
		}
	case MsgHeartbeat:
		gLog.Printf(LvDev, "P2PNetwork heartbeat ok")
		pn.hbTime = time.Now()
		rtt := pn.hbTime.UnixNano() - pn.t1
		if rtt > int64(PunchTsDelay) || (pn.preRtt > 0 && rtt > pn.preRtt*5) {
			gLog.Printf(LvINFO, "rtt=%d too large ignore", rtt)
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
		gLog.Printf(LvDEBUG, "synctime thisdt=%dms dt=%dms ddt=%dns ddtma=%dns rtt=%dms ", thisdt/int64(time.Millisecond), pn.dt/int64(time.Millisecond), pn.ddt, pn.ddtma, rtt/int64(time.Millisecond))
	case MsgPush:
		handlePush(head.SubType, msg)
	case MsgSDWAN:
		handleSDWAN(head.SubType, msg)
	default:
		i, ok := pn.msgMap.Load(uint64(0))
		if ok {
			ch := i.(chan msgCtx)
			ch <- msgCtx{data: msg, ts: time.Now()}
		}

		return
	}
}

func (pn *P2PNetwork) readLoop() {
	gLog.Printf(LvDEBUG, "P2PNetwork readLoop start")
	pn.wgReconnect.Add(1)
	defer pn.wgReconnect.Done()
	for pn.running {
		pn.conn.SetReadDeadline(time.Now().Add(NetworkHeartbeatTime + 10*time.Second))
		_, msg, err := pn.conn.ReadMessage()
		if err != nil {
			gLog.Printf(LvERROR, "P2PNetwork read error:%s", err)
			pn.close()
			break
		}
		pn.handleMessage(msg)
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
		pn.close()
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
		gLog.Printf(LvERROR, "relay to %d len=%d error:%s", to, len(body), err)
	}
	return err
}

func (pn *P2PNetwork) push(to string, subType uint16, packet interface{}) error {
	// gLog.Printf(LvDEBUG, "push msgType %d to %s", subType, to)
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
	if err = pn.conn.WriteMessage(websocket.BinaryMessage, pushMsg); err != nil {
		gLog.Printf(LvERROR, "push to %s error:%s", to, err)
		pn.close()
	}
	return err
}

func (pn *P2PNetwork) close() {
	if pn.running {
		if pn.conn != nil {
			pn.conn.Close()
		}
		pn.running = false
	}
	select {
	case pn.restartCh <- true:
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
		gLog.Printf(LvERROR, "read msg error: %s not found", node)
		return
	}
	ch := i.(chan msgCtx)
	for {
		select {
		case <-time.After(timeout):
			gLog.Printf(LvERROR, "read msg error %d:%d timeout", mainType, subType)
			return
		case msg := <-ch:
			head = &openP2PHeader{}
			err := binary.Read(bytes.NewReader(msg.data[:openP2PHeaderSize]), binary.LittleEndian, head)
			if err != nil {
				gLog.Println(LvERROR, "read msg error:", err)
				break
			}
			if time.Since(msg.ts) > ReadMsgTimeout {
				gLog.Printf(LvDEBUG, "read msg error expired %d:%d", head.MainType, head.SubType)
				continue
			}
			if head.MainType != mainType || head.SubType != subType {
				gLog.Printf(LvDEBUG, "read msg error type %d:%d, requeue it", head.MainType, head.SubType)
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

func (pn *P2PNetwork) updateAppHeartbeat(appID uint64) {
	pn.apps.Range(func(id, i interface{}) bool {
		app := i.(*p2pApp)
		if app.id == appID {
			app.updateHeartbeat()
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
		if IsIPv6(string(buf[:n])) {
			gConf.setIPv6(string(buf[:n]))
		}
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
		gLog.Println(LvERROR, "requestPeerInfo error")
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
	if gConf.nodeID() < NodeNameToID(node) {
		return errors.New("only the bigger nodeid connect")
	}
	peerNodeID := fmt.Sprintf("%d", NodeNameToID(node))
	config := AppConfig{Enabled: 1}
	config.AppName = peerNodeID
	config.SrcPort = 0
	config.PeerNode = node
	sdwan := gConf.getSDWAN()
	config.PunchPriority = int(sdwan.PunchPriority)
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
	if app.Tunnel() == nil {
		return errors.New("peer tunnel nil")
	}
	// TODO: move to app.write
	gLog.Printf(LvDev, "%d tunnel write node data bodylen=%d, relay=%t", app.Tunnel().id, len(buff), !app.isDirect())
	if app.isDirect() { // direct
		app.Tunnel().asyncWriteNodeData(MsgP2P, MsgNodeData, buff)
	} else { // relay
		fromNodeIDHead := new(bytes.Buffer)
		binary.Write(fromNodeIDHead, binary.LittleEndian, gConf.nodeID())
		all := app.RelayHead().Bytes()
		all = append(all, encodeHeader(MsgP2P, MsgRelayNodeData, uint32(len(buff)+overlayHeaderSize))...)
		all = append(all, fromNodeIDHead.Bytes()...)
		all = append(all, buff...)
		app.Tunnel().asyncWriteNodeData(MsgP2P, MsgRelayData, all)
	}

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
		if app.Tunnel() == nil {
			return true
		}
		if app.config.SrcPort != 0 { // normal portmap app
			return true
		}
		if app.config.peerIP == gConf.Network.publicIP { // mostly in a lan
			return true
		}
		if app.isDirect() { // direct
			app.Tunnel().conn.WriteBytes(MsgP2P, MsgNodeData, buff)
		} else { // relay
			fromNodeIDHead := new(bytes.Buffer)
			binary.Write(fromNodeIDHead, binary.LittleEndian, gConf.nodeID())
			all := app.RelayHead().Bytes()
			all = append(all, encodeHeader(MsgP2P, MsgRelayNodeData, uint32(len(buff)+overlayHeaderSize))...)
			all = append(all, fromNodeIDHead.Bytes()...)
			all = append(all, buff...)
			app.Tunnel().conn.WriteBytes(MsgP2P, MsgRelayData, all)
		}
		return true
	})
	return nil
}

func (pn *P2PNetwork) ReadNode(tm time.Duration) *NodeData {
	select {
	case nd := <-pn.nodeData:
		return nd
	case <-time.After(tm):
	}
	return nil
}
