package openp2p

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/rand"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

type p2pApp struct {
	config       AppConfig
	listener     net.Listener
	listenerUDP  *net.UDPConn
	directTunnel *P2PTunnel
	relayTunnel  *P2PTunnel
	tunnelMtx    sync.Mutex
	iptree       *IPTree // for whitelist
	rtid         uint64  // relay tunnelID
	relayNode    string
	relayMode    string // public/private
	hbTimeRelay  time.Time
	hbMtx        sync.Mutex
	running      bool
	id           uint64
	key          uint64 // aes
	wg           sync.WaitGroup
	relayHead    *bytes.Buffer
	once         sync.Once
	// for relayTunnel
	retryRelayNum      int
	retryRelayTime     time.Time
	nextRetryRelayTime time.Time
	errMsg             string
	connectTime        time.Time
}

func (app *p2pApp) Tunnel() *P2PTunnel {
	app.tunnelMtx.Lock()
	defer app.tunnelMtx.Unlock()
	if app.directTunnel != nil {
		return app.directTunnel
	}
	return app.relayTunnel
}

func (app *p2pApp) DirectTunnel() *P2PTunnel {
	app.tunnelMtx.Lock()
	defer app.tunnelMtx.Unlock()
	return app.directTunnel
}

func (app *p2pApp) setDirectTunnel(t *P2PTunnel) {
	app.tunnelMtx.Lock()
	defer app.tunnelMtx.Unlock()
	app.directTunnel = t
}

func (app *p2pApp) RelayTunnel() *P2PTunnel {
	app.tunnelMtx.Lock()
	defer app.tunnelMtx.Unlock()
	return app.relayTunnel
}

func (app *p2pApp) setRelayTunnel(t *P2PTunnel) {
	app.tunnelMtx.Lock()
	defer app.tunnelMtx.Unlock()
	app.relayTunnel = t
}

func (app *p2pApp) isDirect() bool {
	return app.directTunnel != nil
}

func (app *p2pApp) RelayTunnelID() uint64 {
	if app.isDirect() {
		return 0
	}
	return app.rtid
}

func (app *p2pApp) ConnectTime() time.Time {
	if app.isDirect() {
		return app.config.connectTime
	}
	return app.connectTime
}

func (app *p2pApp) RetryTime() time.Time {
	if app.isDirect() {
		return app.config.retryTime
	}
	return app.retryRelayTime
}

func (app *p2pApp) checkP2PTunnel() error {
	for app.running {
		app.checkDirectTunnel()
		app.checkRelayTunnel()
		time.Sleep(time.Second * 3)
	}
	return nil
}

func (app *p2pApp) directRetryLimit() int {
	if app.config.peerIP == gConf.Network.publicIP && compareVersion(app.config.peerVersion, SupportIntranetVersion) >= 0 {
		return retryLimit
	}
	if IsIPv6(app.config.peerIPv6) && IsIPv6(gConf.IPv6()) {
		return retryLimit
	}
	if app.config.hasIPv4 == 1 || gConf.Network.hasIPv4 == 1 || app.config.hasUPNPorNATPMP == 1 || gConf.Network.hasUPNPorNATPMP == 1 {
		return retryLimit
	}
	if gConf.Network.natType == NATCone && app.config.peerNatType == NATCone {
		return retryLimit
	}
	if app.config.peerNatType == NATSymmetric && gConf.Network.natType == NATSymmetric {
		return 0
	}
	return retryLimit / 10 // c2s or s2c
}
func (app *p2pApp) checkDirectTunnel() error {
	if app.config.ForceRelay == 1 && app.config.RelayNode != app.config.PeerNode {
		return nil
	}
	if app.DirectTunnel() != nil && app.DirectTunnel().isActive() {
		return nil
	}
	if app.config.nextRetryTime.After(time.Now()) || app.config.Enabled == 0 || app.config.retryNum >= app.directRetryLimit() {
		return nil
	}
	if time.Now().Add(-time.Minute * 15).After(app.config.retryTime) { // run normally 15min, reset retrynum
		app.config.retryNum = 1
	}
	if app.config.retryNum > 0 { // first time not show reconnect log
		gLog.Printf(LvINFO, "detect app %s appid:%d disconnect, reconnecting the %d times...", app.config.LogPeerNode(), app.id, app.config.retryNum)
	}
	app.config.retryNum++
	app.config.retryTime = time.Now()
	app.config.nextRetryTime = time.Now().Add(retryInterval)
	app.config.connectTime = time.Now()
	err := app.buildDirectTunnel()
	if err != nil {
		app.config.errMsg = err.Error()
		if err == ErrPeerOffline && app.config.retryNum > 2 { // stop retry, waiting for online
			app.config.retryNum = retryLimit
			gLog.Printf(LvINFO, " %s offline, it will auto reconnect when peer node online", app.config.LogPeerNode())
		}
		if err == ErrBuildTunnelBusy {
			app.config.retryNum--
		}
	}
	if app.Tunnel() != nil {
		app.once.Do(func() {
			go app.listen()
			// memapp also need
			go app.relayHeartbeatLoop()
		})
	}
	return nil
}
func (app *p2pApp) buildDirectTunnel() error {
	relayNode := ""
	peerNatType := NATUnknown
	peerIP := ""
	errMsg := ""
	var t *P2PTunnel
	var err error
	pn := GNetwork
	initErr := pn.requestPeerInfo(&app.config)
	if initErr != nil {
		gLog.Printf(LvERROR, "%s requestPeerInfo error:%s", app.config.LogPeerNode(), initErr)
		return initErr
	}
	t, err = pn.addDirectTunnel(app.config, 0)
	if t != nil {
		peerNatType = t.config.peerNatType
		peerIP = t.config.peerIP
	}
	if err != nil {
		errMsg = err.Error()
	}
	req := ReportConnect{
		Error:          errMsg,
		Protocol:       app.config.Protocol,
		SrcPort:        app.config.SrcPort,
		NatType:        gConf.Network.natType,
		PeerNode:       app.config.PeerNode,
		DstPort:        app.config.DstPort,
		DstHost:        app.config.DstHost,
		PeerNatType:    peerNatType,
		PeerIP:         peerIP,
		ShareBandwidth: gConf.Network.ShareBandwidth,
		RelayNode:      relayNode,
		Version:        OpenP2PVersion,
	}
	pn.write(MsgReport, MsgReportConnect, &req)
	if err != nil {
		return err
	}
	// if rtid != 0 || t.conn.Protocol() == "tcp" {
	// sync appkey
	if t == nil {
		return err
	}
	syncKeyReq := APPKeySync{
		AppID:  app.id,
		AppKey: app.key,
	}
	gLog.Printf(LvDEBUG, "sync appkey direct to %s", app.config.LogPeerNode())
	pn.push(app.config.PeerNode, MsgPushAPPKey, &syncKeyReq)
	app.setDirectTunnel(t)

	// if memapp notify peer addmemapp
	if app.config.SrcPort == 0 {
		req := ServerSideSaveMemApp{From: gConf.Network.Node, Node: gConf.Network.Node, TunnelID: t.id, RelayTunnelID: 0, AppID: app.id}
		pn.push(app.config.PeerNode, MsgPushServerSideSaveMemApp, &req)
		gLog.Printf(LvDEBUG, "push %s ServerSideSaveMemApp: %s", app.config.LogPeerNode(), prettyJson(req))
	}
	gLog.Printf(LvDEBUG, "%s use tunnel %d", app.config.AppName, t.id)
	return nil
}

func (app *p2pApp) checkRelayTunnel() error {
	// if app.config.ForceRelay == 1 && (gConf.sdwan.CentralNode == app.config.PeerNode && compareVersion(app.config.peerVersion, SupportDualTunnelVersion) < 0) {
	if app.config.SrcPort == 0 && (gConf.sdwan.CentralNode == app.config.PeerNode || gConf.sdwan.CentralNode == gConf.Network.Node) { // memapp central node not build relay tunnel
		return nil
	}
	app.hbMtx.Lock()
	if app.RelayTunnel() != nil && time.Now().Before(app.hbTimeRelay.Add(TunnelHeartbeatTime*2)) { // must check app.hbtime instead of relayTunnel
		app.hbMtx.Unlock()
		return nil
	}
	app.hbMtx.Unlock()
	if app.nextRetryRelayTime.After(time.Now()) || app.config.Enabled == 0 || app.retryRelayNum >= retryLimit {
		return nil
	}
	if time.Now().Add(-time.Minute * 15).After(app.retryRelayTime) { // run normally 15min, reset retrynum
		app.retryRelayNum = 1
	}
	if app.retryRelayNum > 0 { // first time not show reconnect log
		gLog.Printf(LvINFO, "detect app %s appid:%d relay disconnect, reconnecting the %d times...", app.config.LogPeerNode(), app.id, app.retryRelayNum)
	}
	app.setRelayTunnel(nil) // reset relayTunnel
	app.retryRelayNum++
	app.retryRelayTime = time.Now()
	app.nextRetryRelayTime = time.Now().Add(retryInterval)
	app.connectTime = time.Now()
	err := app.buildRelayTunnel()
	if err != nil {
		app.errMsg = err.Error()
		if err == ErrPeerOffline && app.retryRelayNum > 2 { // stop retry, waiting for online
			app.retryRelayNum = retryLimit
			gLog.Printf(LvINFO, " %s offline, it will auto reconnect when peer node online", app.config.LogPeerNode())
		}
	}
	if app.Tunnel() != nil {
		app.once.Do(func() {
			go app.listen()
			// memapp also need
			go app.relayHeartbeatLoop()
		})
	}
	return nil
}

func (app *p2pApp) buildRelayTunnel() error {
	var rtid uint64
	relayNode := ""
	relayMode := ""
	peerNatType := NATUnknown
	peerIP := ""
	errMsg := ""
	var t *P2PTunnel
	var err error
	pn := GNetwork
	config := app.config
	initErr := pn.requestPeerInfo(&config)
	if initErr != nil {
		gLog.Printf(LvERROR, "%s init error:%s", config.LogPeerNode(), initErr)
		return initErr
	}

	t, rtid, relayMode, err = pn.addRelayTunnel(config)
	if t != nil {
		relayNode = t.config.PeerNode
	}

	if err != nil {
		errMsg = err.Error()
	}
	req := ReportConnect{
		Error:          errMsg,
		Protocol:       config.Protocol,
		SrcPort:        config.SrcPort,
		NatType:        gConf.Network.natType,
		PeerNode:       config.PeerNode,
		DstPort:        config.DstPort,
		DstHost:        config.DstHost,
		PeerNatType:    peerNatType,
		PeerIP:         peerIP,
		ShareBandwidth: gConf.Network.ShareBandwidth,
		RelayNode:      relayNode,
		Version:        OpenP2PVersion,
	}
	pn.write(MsgReport, MsgReportConnect, &req)
	if err != nil {
		return err
	}
	// if rtid != 0 || t.conn.Protocol() == "tcp" {
	// sync appkey
	syncKeyReq := APPKeySync{
		AppID:  app.id,
		AppKey: app.key,
	}
	gLog.Printf(LvDEBUG, "sync appkey relay to %s", config.LogPeerNode())
	pn.push(config.PeerNode, MsgPushAPPKey, &syncKeyReq)
	app.setRelayTunnelID(rtid)
	app.setRelayTunnel(t)
	app.relayNode = relayNode
	app.relayMode = relayMode
	app.hbTimeRelay = time.Now()

	// if memapp notify peer addmemapp
	if config.SrcPort == 0 {
		req := ServerSideSaveMemApp{From: gConf.Network.Node, Node: relayNode, TunnelID: rtid, RelayTunnelID: t.id, AppID: app.id, RelayMode: relayMode}
		pn.push(config.PeerNode, MsgPushServerSideSaveMemApp, &req)
		gLog.Printf(LvDEBUG, "push %s relay ServerSideSaveMemApp: %s", config.LogPeerNode(), prettyJson(req))
	}
	gLog.Printf(LvDEBUG, "%s use tunnel %d", app.config.AppName, t.id)
	return nil
}

func (app *p2pApp) buildOfficialTunnel() error {
	return nil
}

// cache relayHead, refresh when rtid change
func (app *p2pApp) RelayHead() *bytes.Buffer {
	if app.relayHead == nil {
		app.relayHead = new(bytes.Buffer)
		binary.Write(app.relayHead, binary.LittleEndian, app.rtid)
	}
	return app.relayHead
}

func (app *p2pApp) setRelayTunnelID(rtid uint64) {
	app.rtid = rtid
	app.relayHead = new(bytes.Buffer)
	binary.Write(app.relayHead, binary.LittleEndian, app.rtid)
}

func (app *p2pApp) isActive() bool {
	if app.Tunnel() == nil {
		// gLog.Printf(LvDEBUG, "isActive app.tunnel==nil")
		return false
	}
	if app.isDirect() { // direct mode app heartbeat equals to tunnel heartbeat
		return app.Tunnel().isActive()
	}
	// relay mode calc app heartbeat
	app.hbMtx.Lock()
	defer app.hbMtx.Unlock()
	res := time.Now().Before(app.hbTimeRelay.Add(TunnelHeartbeatTime * 2))
	// if !res {
	// 	gLog.Printf(LvDEBUG, "%d app isActive false. peer=%s", app.id, app.config.PeerNode)
	// }
	return res
}

func (app *p2pApp) updateHeartbeat() {
	app.hbMtx.Lock()
	defer app.hbMtx.Unlock()
	app.hbTimeRelay = time.Now()
}

func (app *p2pApp) listenTCP() error {
	gLog.Printf(LvDEBUG, "tcp accept on port %d start", app.config.SrcPort)
	defer gLog.Printf(LvDEBUG, "tcp accept on port %d end", app.config.SrcPort)
	var err error
	listenAddr := ""
	if IsLocalhost(app.config.Whitelist) { // not expose port
		listenAddr = "127.0.0.1"
	}
	app.listener, err = net.Listen("tcp", fmt.Sprintf("%s:%d", listenAddr, app.config.SrcPort))
	if err != nil {
		gLog.Printf(LvERROR, "listen error:%s", err)
		return err
	}
	defer app.listener.Close()
	for app.running {
		conn, err := app.listener.Accept()
		if err != nil {
			if app.running {
				gLog.Printf(LvERROR, "%d accept error:%s", app.id, err)
			}
			break
		}
		if app.Tunnel() == nil {
			gLog.Printf(LvDEBUG, "srcPort=%d, app.Tunnel()==nil, not ready", app.config.SrcPort)
			time.Sleep(time.Second)
			continue
		}
		// check white list
		if app.config.Whitelist != "" {
			remoteIP := conn.RemoteAddr().(*net.TCPAddr).IP.String()
			if !app.iptree.Contains(remoteIP) && !IsLocalhost(remoteIP) {
				conn.Close()
				gLog.Printf(LvERROR, "%s not in whitelist, access denied", remoteIP)
				continue
			}
		}
		oConn := overlayConn{
			tunnel:   app.Tunnel(),
			app:      app,
			connTCP:  conn,
			id:       rand.Uint64(),
			isClient: true,
			appID:    app.id,
			appKey:   app.key,
			running:  true,
		}
		if !app.isDirect() {
			oConn.rtid = app.rtid
		}
		// pre-calc key bytes for encrypt
		if oConn.appKey != 0 {
			encryptKey := make([]byte, AESKeySize)
			binary.LittleEndian.PutUint64(encryptKey, oConn.appKey)
			binary.LittleEndian.PutUint64(encryptKey[8:], oConn.appKey)
			oConn.appKeyBytes = encryptKey
		}
		app.Tunnel().overlayConns.Store(oConn.id, &oConn)
		gLog.Printf(LvDEBUG, "Accept TCP overlayID:%d, %s", oConn.id, oConn.connTCP.RemoteAddr())
		// tell peer connect
		req := OverlayConnectReq{ID: oConn.id,
			Token:    gConf.Network.Token,
			DstIP:    app.config.DstHost,
			DstPort:  app.config.DstPort,
			Protocol: app.config.Protocol,
			AppID:    app.id,
		}
		if !app.isDirect() {
			req.RelayTunnelID = app.Tunnel().id
		}
		app.Tunnel().WriteMessage(app.RelayTunnelID(), MsgP2P, MsgOverlayConnectReq, &req)
		// TODO: wait OverlayConnectRsp instead of sleep
		time.Sleep(time.Second) // waiting remote node connection ok
		go oConn.run()
	}
	return nil
}

func (app *p2pApp) listenUDP() error {
	gLog.Printf(LvDEBUG, "udp accept on port %d start", app.config.SrcPort)
	defer gLog.Printf(LvDEBUG, "udp accept on port %d end", app.config.SrcPort)
	var err error
	app.listenerUDP, err = net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: app.config.SrcPort})
	if err != nil {
		gLog.Printf(LvERROR, "listen error:%s", err)
		return err
	}
	defer app.listenerUDP.Close()
	buffer := make([]byte, 64*1024+PaddingSize)
	udpID := make([]byte, 8)
	for {
		app.listenerUDP.SetReadDeadline(time.Now().Add(UDPReadTimeout))
		len, remoteAddr, err := app.listenerUDP.ReadFrom(buffer)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			} else {
				gLog.Printf(LvERROR, "udp read failed:%s", err)
				break
			}
		} else {
			if app.Tunnel() == nil {
				gLog.Printf(LvDEBUG, "srcPort=%d, app.Tunnel()==nil, not ready", app.config.SrcPort)
				time.Sleep(time.Second)
				continue
			}
			dupData := bytes.Buffer{} // should uses memory pool
			dupData.Write(buffer[:len+PaddingSize])
			// load from app.tunnel.overlayConns by remoteAddr error, new udp connection
			remoteIP := strings.Split(remoteAddr.String(), ":")[0]
			port, _ := strconv.Atoi(strings.Split(remoteAddr.String(), ":")[1])
			a := net.ParseIP(remoteIP)
			udpID[0] = a[0]
			udpID[1] = a[1]
			udpID[2] = a[2]
			udpID[3] = a[3]
			udpID[4] = byte(port)
			udpID[5] = byte(port >> 8)
			id := binary.LittleEndian.Uint64(udpID) // convert remoteIP:port to uint64
			s, ok := app.Tunnel().overlayConns.Load(id)
			if !ok {
				oConn := overlayConn{
					tunnel:     app.Tunnel(),
					connUDP:    app.listenerUDP,
					remoteAddr: remoteAddr,
					udpData:    make(chan []byte, 1000),
					id:         id,
					isClient:   true,
					appID:      app.id,
					appKey:     app.key,
					running:    true,
				}
				if !app.isDirect() {
					oConn.rtid = app.rtid
				}
				// calc key bytes for encrypt
				if oConn.appKey != 0 {
					encryptKey := make([]byte, AESKeySize)
					binary.LittleEndian.PutUint64(encryptKey, oConn.appKey)
					binary.LittleEndian.PutUint64(encryptKey[8:], oConn.appKey)
					oConn.appKeyBytes = encryptKey
				}
				app.Tunnel().overlayConns.Store(oConn.id, &oConn)
				gLog.Printf(LvDEBUG, "Accept UDP overlayID:%d", oConn.id)
				// tell peer connect
				req := OverlayConnectReq{ID: oConn.id,
					Token:    gConf.Network.Token,
					DstIP:    app.config.DstHost,
					DstPort:  app.config.DstPort,
					Protocol: app.config.Protocol,
					AppID:    app.id,
				}
				if !app.isDirect() {
					req.RelayTunnelID = app.Tunnel().id
				}
				app.Tunnel().WriteMessage(app.RelayTunnelID(), MsgP2P, MsgOverlayConnectReq, &req)
				// TODO: wait OverlayConnectRsp instead of sleep
				time.Sleep(time.Second) // waiting remote node connection ok
				go oConn.run()
				oConn.udpData <- dupData.Bytes()
			}

			// load from app.tunnel.overlayConns by remoteAddr ok, write relay data
			overlayConn, ok := s.(*overlayConn)
			if !ok {
				continue
			}
			overlayConn.udpData <- dupData.Bytes()
		}
	}
	return nil
}

func (app *p2pApp) listen() error {
	if app.config.SrcPort == 0 {
		return nil
	}
	gLog.Printf(LvINFO, "LISTEN ON PORT %s:%d START", app.config.Protocol, app.config.SrcPort)
	defer gLog.Printf(LvINFO, "LISTEN ON PORT %s:%d END", app.config.Protocol, app.config.SrcPort)
	app.wg.Add(1)
	defer app.wg.Done()
	for app.running {
		if app.config.Protocol == "udp" {
			app.listenUDP()
		} else {
			app.listenTCP()
		}
		if !app.running {
			break
		}
		time.Sleep(time.Second * 10)
	}
	return nil
}

func (app *p2pApp) close() {
	app.running = false
	if app.listener != nil {
		app.listener.Close()
	}
	if app.listenerUDP != nil {
		app.listenerUDP.Close()
	}
	if app.DirectTunnel() != nil {
		app.DirectTunnel().closeOverlayConns(app.id)
	}
	if app.RelayTunnel() != nil {
		app.RelayTunnel().closeOverlayConns(app.id)
	}
	app.wg.Wait()
}

// TODO: many relay app on the same P2PTunnel will send a lot of relay heartbeat
func (app *p2pApp) relayHeartbeatLoop() {
	app.wg.Add(1)
	defer app.wg.Done()
	gLog.Printf(LvDEBUG, "%s appid:%d relayHeartbeat to rtid:%d start", app.config.LogPeerNode(), app.id, app.rtid)
	defer gLog.Printf(LvDEBUG, "%s appid:%d relayHeartbeat to rtid%d end", app.config.LogPeerNode(), app.id, app.rtid)

	for app.running {
		if app.RelayTunnel() == nil || !app.RelayTunnel().isRuning() {
			time.Sleep(TunnelHeartbeatTime)
			continue
		}
		req := RelayHeartbeat{From: gConf.Network.Node, RelayTunnelID: app.RelayTunnel().id,
			AppID: app.id}
		err := app.RelayTunnel().WriteMessage(app.rtid, MsgP2P, MsgRelayHeartbeat, &req)
		if err != nil {
			gLog.Printf(LvERROR, "%s appid:%d rtid:%d write relay tunnel heartbeat error %s", app.config.LogPeerNode(), app.id, app.rtid, err)
			return
		}
		// TODO: debug relay heartbeat
		gLog.Printf(LvDEBUG, "%s appid:%d rtid:%d write relay tunnel heartbeat ok", app.config.LogPeerNode(), app.id, app.rtid)
		time.Sleep(TunnelHeartbeatTime)
	}
}
