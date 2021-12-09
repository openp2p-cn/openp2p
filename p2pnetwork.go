package main

import (
	"bytes"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var (
	instance *P2PNetwork
	once     sync.Once
)

type P2PNetwork struct {
	conn      *websocket.Conn
	online    bool
	running   bool
	restartCh chan bool
	wg        sync.WaitGroup
	writeMtx  sync.Mutex
	serverTs  uint64
	// msgMap    sync.Map
	msgMap     map[uint64]chan []byte //key: nodeID
	msgMapMtx  sync.Mutex
	config     NetworkConfig
	allTunnels sync.Map
	apps       sync.Map
	limiter    *BandwidthLimiter
}

func P2PNetworkInstance(config *NetworkConfig) *P2PNetwork {
	if instance == nil {
		once.Do(func() {
			instance = &P2PNetwork{
				restartCh: make(chan bool, 2),
				online:    false,
				running:   true,
				msgMap:    make(map[uint64]chan []byte),
				limiter:   newBandwidthLimiter(config.shareBandwidth),
			}
			instance.msgMap[0] = make(chan []byte) // for gateway
			if config != nil {
				instance.config = *config
			}
			instance.init()
			go instance.run()
		})
	}
	return instance
}

func (pn *P2PNetwork) run() {
	go pn.autoReconnectApp()
	heartbeatTimer := time.NewTicker(NetworkHeartbeatTime)
	for pn.running {
		select {
		case <-heartbeatTimer.C: // TODO: deal with connect failed, no send hb
			pn.write(MsgHeartbeat, 0, "")

		case <-pn.restartCh:
			pn.online = false
			pn.wg.Wait() // wait read/write goroutine exited
			time.Sleep(NetworkHeartbeatTime)
			err := pn.init()
			if err != nil {
				gLog.Println(LevelERROR, "P2PNetwork init error:", err)
			}
		}
	}
}

func (pn *P2PNetwork) Connect(timeout int) bool {
	// waiting for login response
	for i := 0; i < (timeout / 1000); i++ {
		if pn.serverTs != 0 {
			return true
		}
		time.Sleep(time.Second)
	}
	return false
}

func (pn *P2PNetwork) autoReconnectApp() {
	gLog.Println(LevelINFO, "autoReconnectApp start")
	retryApps := make([]AppConfig, 0)
	for pn.running {
		time.Sleep(time.Second)
		if !pn.online {
			continue
		}
		if len(retryApps) > 0 {
			gLog.Printf(LevelINFO, "retryApps len=%d", len(retryApps))
			thisRound := make([]AppConfig, 0)
			for i := 0; i < len(retryApps); i++ {
				// reset retryNum when running 15min continuously
				if retryApps[i].retryTime.Add(time.Minute * 15).Before(time.Now()) {
					retryApps[i].retryNum = 0
				}
				retryApps[i].retryNum++
				retryApps[i].retryTime = time.Now()
				if retryApps[i].retryNum > MaxRetry {
					gLog.Printf(LevelERROR, "app %s%d retry more than %d times, exit.", retryApps[i].Protocol, retryApps[i].SrcPort, MaxRetry)
					continue
				}
				pn.DeleteApp(retryApps[i])
				if err := pn.AddApp(retryApps[i]); err != nil {
					gLog.Printf(LevelERROR, "AddApp %s%d error:%s", retryApps[i].Protocol, retryApps[i].SrcPort, err)
					thisRound = append(thisRound, retryApps[i])
					time.Sleep(RetryInterval)
				}
			}
			retryApps = thisRound
		}
		pn.apps.Range(func(_, i interface{}) bool {
			app := i.(*p2pApp)
			if app.isActive() {
				return true
			}
			gLog.Printf(LevelINFO, "detect app %s%d disconnect,last hb %s reconnecting...", app.config.Protocol, app.config.SrcPort, app.hbTime)
			config := app.config
			// clear peerinfo
			config.peerConeNatPort = 0
			config.peerIP = ""
			config.peerNatType = 0
			config.peerToken = 0
			pn.DeleteApp(config)
			retryApps = append(retryApps, config)
			return true
		})
	}
	gLog.Println(LevelINFO, "autoReconnectApp end")
}

func (pn *P2PNetwork) addRelayTunnel(config AppConfig, appid uint64, appkey uint64) (*P2PTunnel, uint64, error) {
	gLog.Printf(LevelINFO, "addRelayTunnel to %s start", config.PeerNode)
	defer gLog.Printf(LevelINFO, "addRelayTunnel to %s end", config.PeerNode)
	pn.write(MsgRelay, MsgRelayNodeReq, &RelayNodeReq{config.PeerNode})
	head, body := pn.read("", MsgRelay, MsgRelayNodeRsp, time.Second*10)
	if head == nil {
		return nil, 0, errors.New("read MsgRelayNodeRsp error")
	}
	rsp := RelayNodeRsp{}
	err := json.Unmarshal(body, &rsp)
	if err != nil {
		gLog.Printf(LevelERROR, "wrong RelayNodeRsp:%s", err)
		return nil, 0, errors.New("unmarshal MsgRelayNodeRsp error")
	}
	if rsp.RelayName == "" || rsp.RelayToken == 0 {
		gLog.Printf(LevelERROR, "MsgRelayNodeReq error")
		return nil, 0, errors.New("MsgRelayNodeReq error")
	}
	gLog.Printf(LevelINFO, "got relay node:%s", rsp.RelayName)
	relayConfig := config
	relayConfig.PeerNode = rsp.RelayName
	relayConfig.peerToken = rsp.RelayToken
	t, err := pn.addDirectTunnel(relayConfig, 0)
	if err != nil {
		gLog.Println(LevelERROR, "direct connect error:", err)
		return nil, 0, err
	}
	// notify peer addRelayTunnel
	req := AddRelayTunnelReq{
		From:       pn.config.Node,
		RelayName:  rsp.RelayName,
		RelayToken: rsp.RelayToken,
		AppID:      appid,
		AppKey:     appkey,
	}
	gLog.Printf(LevelINFO, "push relay %s---------%s", config.PeerNode, rsp.RelayName)
	pn.push(config.PeerNode, MsgPushAddRelayTunnelReq, &req)

	// wait relay ready
	head, body = pn.read(config.PeerNode, MsgPush, MsgPushAddRelayTunnelRsp, PeerAddRelayTimeount) // TODO: const value
	if head == nil {
		gLog.Printf(LevelERROR, "read MsgPushAddRelayTunnelRsp error")
		return nil, 0, errors.New("read MsgPushAddRelayTunnelRsp error")
	}
	rspID := TunnelMsg{}
	err = json.Unmarshal(body, &rspID)
	if err != nil {
		gLog.Printf(LevelERROR, "wrong RelayNodeRsp:%s", err)
		return nil, 0, errors.New("unmarshal MsgRelayNodeRsp error")
	}
	return t, rspID.ID, err
}

func (pn *P2PNetwork) AddApp(config AppConfig) error {
	gLog.Printf(LevelINFO, "addApp %s%d to %s:%s:%d start", config.Protocol, config.SrcPort, config.PeerNode, config.DstHost, config.DstPort)
	defer gLog.Printf(LevelINFO, "addApp %s%d to %s:%s:%d end", config.Protocol, config.SrcPort, config.PeerNode, config.DstHost, config.DstPort)
	if !pn.online {
		return errors.New("P2PNetwork offline")
	}
	// check if app already exist?
	appExist := false
	pn.apps.Range(func(_, i interface{}) bool {
		app := i.(*p2pApp)
		if app.config.Protocol == config.Protocol && app.config.SrcPort == config.SrcPort {
			appExist = true
			return false
		}
		return true
	})
	if appExist {
		return errors.New("P2PApp already exist")
	}
	appID := rand.Uint64()
	appKey := uint64(0)
	t, err := pn.addDirectTunnel(config, 0)
	var rtid uint64
	relayNode := ""
	peerNatType := 100
	peerIP := ""
	errMsg := ""
	if err != nil && err == ErrorHandshake {
		gLog.Println(LevelERROR, "direct connect failed, try to relay")
		appKey = rand.Uint64()
		t, rtid, err = pn.addRelayTunnel(config, appID, appKey)
		if t != nil {
			relayNode = t.config.PeerNode
		}
	}
	if t != nil {
		peerNatType = t.config.peerNatType
		peerIP = t.config.peerIP
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
		PeerUser:       config.PeerUser,
		PeerNatType:    peerNatType,
		PeerIP:         peerIP,
		ShareBandwidth: pn.config.shareBandwidth,
		RelayNode:      relayNode,
		Version:        OpenP2PVersion,
	}
	pn.write(MsgReport, MsgReportConnect, &req)

	app := p2pApp{
		id:     appID,
		key:    appKey,
		tunnel: t,
		config: config,
		rtid:   rtid,
		hbTime: time.Now()}
	pn.apps.Store(appID, &app)
	if err == nil {
		go app.listen()
	}
	return err
}

func (pn *P2PNetwork) DeleteApp(config AppConfig) {
	gLog.Printf(LevelINFO, "DeleteApp %s%d start", config.Protocol, config.SrcPort)
	defer gLog.Printf(LevelINFO, "DeleteApp %s%d end", config.Protocol, config.SrcPort)
	// close the apps of this config
	pn.apps.Range(func(_, i interface{}) bool {
		app := i.(*p2pApp)
		if app.config.Protocol == config.Protocol && app.config.SrcPort == config.SrcPort {
			gLog.Printf(LevelINFO, "app %s exist, delete it", fmt.Sprintf("%s%d", config.Protocol, config.SrcPort))
			app := i.(*p2pApp)
			app.close()
			pn.apps.Delete(app.id)
			return false
		}
		return true
	})
}

func (pn *P2PNetwork) addDirectTunnel(config AppConfig, tid uint64) (*P2PTunnel, error) {
	gLog.Printf(LevelINFO, "addDirectTunnel %s%d to %s:%s:%d start", config.Protocol, config.SrcPort, config.PeerNode, config.DstHost, config.DstPort)
	defer gLog.Printf(LevelINFO, "addDirectTunnel %s%d to %s:%s:%d end", config.Protocol, config.SrcPort, config.PeerNode, config.DstHost, config.DstPort)
	isClient := false
	// client side tid=0, assign random uint64
	if tid == 0 {
		tid = rand.Uint64()
		isClient = true
	}
	exist := false
	// find existing tunnel to peer
	var t *P2PTunnel
	pn.allTunnels.Range(func(id, i interface{}) bool {
		t = i.(*P2PTunnel)
		if t.config.PeerNode == config.PeerNode {
			// server side force close existing tunnel
			if !isClient {
				t.close()
				return false
			}

			// client side checking
			gLog.Println(LevelINFO, "tunnel already exist ", config.PeerNode)
			isActive := t.checkActive()
			// inactive, close it
			if !isActive {
				gLog.Println(LevelINFO, "but it's not active, close it ", config.PeerNode)
				t.close()
			} else {
				// active
				exist = true
			}
			return false
		}
		return true
	})
	// create tunnel if not exist
	if !exist {
		t = &P2PTunnel{pn: pn,
			config: config,
			id:     tid,
		}
		pn.msgMapMtx.Lock()
		pn.msgMap[nodeNameToID(config.PeerNode)] = make(chan []byte, 50)
		pn.msgMapMtx.Unlock()
		t.init()
		if isClient {
			if err := t.connect(); err != nil {
				gLog.Println(LevelERROR, "p2pTunnel connect error:", err)
				return t, err
			}
		} else {
			rsp := PushConnectRsp{
				Error:       0,
				Detail:      "connect ok",
				To:          t.config.PeerNode,
				From:        pn.config.Node,
				NatType:     pn.config.natType,
				FromIP:      pn.config.publicIP,
				ConeNatPort: t.coneNatPort,
				ID:          t.id}
			t.pn.push(t.config.PeerNode, MsgPushConnectRsp, rsp)
			if err := t.listen(); err != nil {
				gLog.Println(LevelERROR, "p2pTunnel listen error:", err)
				return t, err
			}
		}
	}
	// store it when success
	gLog.Printf(LevelDEBUG, "store tunnel %d", tid)
	pn.allTunnels.Store(tid, t)
	return t, nil
}

func (pn *P2PNetwork) init() error {
	gLog.Println(LevelINFO, "init start")
	var err error
	for {
		pn.config.hostName, err = os.Hostname()
		if err != nil {
			break
		}

		// detect nat type
		pn.config.publicIP, pn.config.natType, err = getNATType(pn.config.ServerHost, pn.config.UDPPort1, pn.config.UDPPort2)
		// TODO rm test s2s
		if strings.Contains(pn.config.Node, "openp2pS2STest") {
			pn.config.natType = NATSymmetric
		}
		if err != nil {
			gLog.Println(LevelINFO, "detect NAT type error:", err)
			break
		}
		gLog.Println(LevelINFO, "detect NAT type:", pn.config.natType, " publicIP:", pn.config.publicIP)
		gatewayURL := fmt.Sprintf("%s:%d", pn.config.ServerHost, pn.config.ServerPort)
		forwardPath := "/openp2p/v1/login"
		config := tls.Config{InsecureSkipVerify: true} // let's encrypt root cert "DST Root CA X3" expired at 2021/09/29. many old system(windows server 2008 etc) will not trust our cert
		websocket.DefaultDialer.TLSClientConfig = &config
		u := url.URL{Scheme: "wss", Host: gatewayURL, Path: forwardPath}
		q := u.Query()
		q.Add("node", pn.config.Node)
		q.Add("user", pn.config.User)
		q.Add("password", pn.config.Password)
		q.Add("version", OpenP2PVersion)
		q.Add("nattype", fmt.Sprintf("%d", pn.config.natType))
		q.Add("timestamp", fmt.Sprintf("%d", time.Now().Unix()))

		noShareStr := "false"
		if pn.config.NoShare {
			noShareStr = "true"
		}
		q.Add("noshare", noShareStr)
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

		req := ReportBasic{
			Mac:     pn.config.mac,
			LanIP:   pn.config.localIP,
			OS:      pn.config.os,
			Version: OpenP2PVersion,
		}
		rsp := netInfo()
		gLog.Println(LevelINFO, rsp)
		if rsp != nil && rsp.Country != "" {
			if len(rsp.IP) == net.IPv6len {
				pn.config.ipv6 = rsp.IP.String()
				req.IPv6 = rsp.IP.String()
			}
			req.NetInfo = *rsp
		}
		pn.write(MsgReport, MsgReportBasic, &req)
		gLog.Println(LevelINFO, "P2PNetwork init ok")
		break
	}
	if err != nil {
		// init failed, retry
		pn.restartCh <- true
		gLog.Println(LevelERROR, "P2PNetwork init error:", err)
	}
	return err
}

func (pn *P2PNetwork) handleMessage(t int, msg []byte) {
	head := openP2PHeader{}
	err := binary.Read(bytes.NewReader(msg[:openP2PHeaderSize]), binary.LittleEndian, &head)
	if err != nil {
		gLog.Println(LevelERROR, "handleMessage error:", err)
		return
	}
	switch head.MainType {
	case MsgLogin:
		// gLog.Println(LevelINFO,string(msg))
		rsp := LoginRsp{}
		err = json.Unmarshal(msg[openP2PHeaderSize:], &rsp)
		if err != nil {
			gLog.Printf(LevelERROR, "wrong login response:%s", err)
			return
		}
		if rsp.Error != 0 {
			gLog.Printf(LevelERROR, "login error:%d, detail:%s", rsp.Error, rsp.Detail)
			pn.running = false
		} else {
			gLog.Printf(LevelINFO, "login ok. Server ts=%d, local ts=%d", rsp.Ts, time.Now().Unix())
			pn.serverTs = rsp.Ts
		}
	case MsgHeartbeat:
		gLog.Printf(LevelDEBUG, "P2PNetwork heartbeat ok")
	case MsgPush:
		pn.handlePush(head.SubType, msg)
	default:
		pn.msgMapMtx.Lock()
		ch := pn.msgMap[0]
		pn.msgMapMtx.Unlock()
		ch <- msg
		return
	}
}

func (pn *P2PNetwork) readLoop() {
	gLog.Printf(LevelINFO, "P2PNetwork readLoop start")
	pn.wg.Add(1)
	defer pn.wg.Done()
	for pn.running {
		pn.conn.SetReadDeadline(time.Now().Add(NetworkHeartbeatTime + 10*time.Second))
		t, msg, err := pn.conn.ReadMessage()
		if err != nil {
			gLog.Printf(LevelERROR, "P2PNetwork read error:%s", err)
			pn.conn.Close()
			pn.restartCh <- true
			break
		}
		pn.handleMessage(t, msg)
	}
	gLog.Printf(LevelINFO, "P2PNetwork readLoop end")
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
		gLog.Printf(LevelERROR, "write msgType %d,%d error:%s", mainType, subType, err)
		pn.conn.Close()
	}
	return err
}

func (pn *P2PNetwork) relay(to uint64, body []byte) error {
	gLog.Printf(LevelDEBUG, "relay data to %d", to)
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
	gLog.Printf(LevelDEBUG, "push msgType %d to %s", subType, to)
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
		gLog.Printf(LevelERROR, "push to %s error:%s", to, err)
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
	for {
		select {
		case <-time.After(timeout):
			gLog.Printf(LevelERROR, "wait msg%d:%d timeout", mainType, subType)
			return
		case msg := <-pn.msgMap[nodeID]:
			head = &openP2PHeader{}
			err := binary.Read(bytes.NewReader(msg[:openP2PHeaderSize]), binary.LittleEndian, head)
			if err != nil {
				gLog.Println(LevelERROR, "read msg error:", err)
				break
			}
			if head.MainType != mainType || head.SubType != subType {
				continue
			}
			if mainType == MsgPush {
				body = msg[openP2PHeaderSize+PushHeaderSize:]
			} else {
				body = msg[openP2PHeaderSize:]
			}
			return
		}
	}
}

func (pn *P2PNetwork) handlePush(subType uint16, msg []byte) error {
	pushHead := PushHeader{}
	err := binary.Read(bytes.NewReader(msg[openP2PHeaderSize:openP2PHeaderSize+PushHeaderSize]), binary.LittleEndian, &pushHead)
	if err != nil {
		return err
	}
	gLog.Printf(LevelDEBUG, "handle push msg type:%d, push header:%+v", subType, pushHead)
	switch subType {
	case MsgPushConnectReq:
		req := PushConnectReq{}
		err := json.Unmarshal(msg[openP2PHeaderSize+PushHeaderSize:], &req)
		if err != nil {
			gLog.Printf(LevelERROR, "wrong MsgPushConnectReq:%s", err)
			return err
		}
		gLog.Printf(LevelINFO, "%s is connecting...", req.From)
		gLog.Println(LevelDEBUG, "push connect response to ", req.From)
		// verify token or name&password
		if VerifyTOTP(req.Token, pn.config.User, pn.config.Password, time.Now().Unix()) || (req.User == pn.config.User && req.Password == pn.config.Password) {
			gLog.Printf(LevelINFO, "Access Granted\n")
			config := AppConfig{}
			config.peerNatType = req.NatType
			config.peerConeNatPort = req.ConeNatPort
			config.peerIP = req.FromIP
			config.PeerNode = req.From
			// share relay node will limit bandwidth
			if req.User != pn.config.User || req.Password != pn.config.Password {
				gLog.Printf(LevelINFO, "set share bandwidth %d mbps", pn.config.shareBandwidth)
				config.shareBandwidth = pn.config.shareBandwidth
			}
			// go pn.AddTunnel(config, req.ID)
			go pn.addDirectTunnel(config, req.ID)
			break
		}
		gLog.Println(LevelERROR, "Access Denied:", req.From)
		rsp := PushConnectRsp{
			Error:  1,
			Detail: fmt.Sprintf("connect to %s error: Access Denied", pn.config.Node),
			To:     req.From,
			From:   pn.config.Node,
		}
		pn.push(req.From, MsgPushConnectRsp, rsp)
	case MsgPushRsp:
		rsp := PushRsp{}
		err := json.Unmarshal(msg[openP2PHeaderSize:], &rsp)
		if err != nil {
			gLog.Printf(LevelERROR, "wrong pushRsp:%s", err)
			return err
		}
		if rsp.Error == 0 {
			gLog.Printf(LevelDEBUG, "push ok, detail:%s", rsp.Detail)
		} else {
			gLog.Printf(LevelERROR, "push error:%d, detail:%s", rsp.Error, rsp.Detail)
		}
	case MsgPushAddRelayTunnelReq:
		req := AddRelayTunnelReq{}
		err := json.Unmarshal(msg[openP2PHeaderSize+PushHeaderSize:], &req)
		if err != nil {
			gLog.Printf(LevelERROR, "wrong RelayNodeRsp:%s", err)
			return err
		}
		config := AppConfig{}
		config.PeerNode = req.RelayName
		config.peerToken = req.RelayToken
		// set user password, maybe the relay node is your private node
		config.PeerUser = pn.config.User
		config.PeerPassword = pn.config.Password
		go func(r AddRelayTunnelReq) {
			t, errDt := pn.addDirectTunnel(config, 0)
			if errDt == nil {
				// notify peer relay ready
				msg := TunnelMsg{ID: t.id}
				pn.push(r.From, MsgPushAddRelayTunnelRsp, msg)
				SaveKey(req.AppID, req.AppKey)
			}

		}(req)
	case MsgPushUpdate:
		update()
		if gConf.daemonMode {
			os.Exit(0)
		}
	default:
		pn.msgMapMtx.Lock()
		ch := pn.msgMap[pushHead.From]
		pn.msgMapMtx.Unlock()
		ch <- msg
	}
	return nil
}

func (pn *P2PNetwork) updateAppHeartbeat(appID uint64) {
	pn.apps.Range(func(id, i interface{}) bool {
		key := id.(uint64)
		if key != appID {
			return true
		}
		app := i.(*p2pApp)
		app.updateHeartbeat()
		return false
	})
}
