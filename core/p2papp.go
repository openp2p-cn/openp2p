package openp2p

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"math/rand"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const DefaultRtt int32 = 1000
const MaxWindowSize = 1024 * 128               // max 32k packets in flight
const MergeAckDelay = 40                       // 40ms linux kernel tcp
const RetransmissonTime = MergeAckDelay + 2000 // ms

type appMsgCtx struct {
	head *openP2PHeader
	body []byte
	ts   time.Time
}

type p2pApp struct {
	config      AppConfig
	listener    net.Listener
	listenerUDP *net.UDPConn

	tunnelMtx sync.Mutex
	iptree    *IPTree // for whitelist

	hbMtx         sync.Mutex
	running       bool
	id            uint64
	key           uint64 // aes
	appKeyBytes   []byte // pre-calc
	wg            sync.WaitGroup
	msgChan       chan appMsgCtx
	once          sync.Once
	tunnelNum     int
	allTunnels    []*P2PTunnel
	retryNum      []int
	retryTime     []time.Time
	nextRetryTime []time.Time
	rtt           []atomic.Int32
	relayHead     []*bytes.Buffer
	rtid          []uint64 // peer relay tunnelID
	relayNode     []string
	relayMode     []string // public/private
	hbTime        []time.Time
	whbTime       []time.Time     // calc each tunnel rtt by hb
	unAckSeqStart []atomic.Uint64 // record unack packet for retransmission
	unAckSeqEnd   []atomic.Uint64

	errMsg      string
	connectTime time.Time
	// asyncWriteChan chan []byte
	maxWindowSize uint64

	unAckTs     []atomic.Int64
	writeTs     []atomic.Int64
	readCacheTs atomic.Int64

	seqW         uint64
	seqR         uint64
	seqRMtx      sync.Mutex
	handleAckMtx sync.Mutex
	mergeAckSeq  []atomic.Uint64
	mergeAckTs   []atomic.Int64

	preDirectSuccessIP string
}


func (app *p2pApp) Tunnel(idx int) *P2PTunnel {
	if idx > app.tunnelNum-1 {
		return nil
	}
	app.tunnelMtx.Lock()
	defer app.tunnelMtx.Unlock()
	return app.allTunnels[idx]
}

func (app *p2pApp) SetTunnel(t *P2PTunnel, idx int) {
	app.tunnelMtx.Lock()
	defer app.tunnelMtx.Unlock()
	app.allTunnels[idx] = t

	app.rtt[idx].Store(DefaultRtt)
	app.unAckTs[idx].Store(0)
	app.writeTs[idx].Store(0)
}

func (app *p2pApp) ConnectTime() time.Time {
	if app.allTunnels[0] != nil {
		return app.config.connectTime
	}
	return app.connectTime
}

func (app *p2pApp) RetryTime() time.Time {
	if app.allTunnels[0] != nil {
		return app.config.retryTime
	}
	return app.retryTime[1]
}

func (app *p2pApp) Init(tunnelNum int) {
	app.tunnelNum = tunnelNum
	app.allTunnels = make([]*P2PTunnel, tunnelNum)
	app.retryNum = make([]int, tunnelNum)
	app.retryTime = make([]time.Time, tunnelNum)
	app.nextRetryTime = make([]time.Time, tunnelNum)
	app.rtt = make([]atomic.Int32, tunnelNum)
	app.relayHead = make([]*bytes.Buffer, tunnelNum)
	app.rtid = make([]uint64, tunnelNum)
	app.relayNode = make([]string, tunnelNum)
	app.relayMode = make([]string, tunnelNum)
	app.hbTime = make([]time.Time, tunnelNum)
	app.whbTime = make([]time.Time, tunnelNum)
	app.unAckSeqEnd = make([]atomic.Uint64, tunnelNum)
	app.unAckTs = make([]atomic.Int64, tunnelNum)
	app.writeTs = make([]atomic.Int64, tunnelNum)
	app.unAckSeqStart = make([]atomic.Uint64, tunnelNum)
	app.mergeAckSeq = make([]atomic.Uint64, tunnelNum)
	app.mergeAckTs = make([]atomic.Int64, tunnelNum)

	app.msgChan = make(chan appMsgCtx, 50)
	for i := 0; i < tunnelNum; i++ {
		app.hbTime[i] = time.Now()
	}
	// app.unAckSeqStart.Store(0)
	// app.mergeAckTs.Store(0)
	// for i := 0; i < relayNum; i++ {
	// 	app.mergeAckTsRelay[i].Store(0)
	// }
}

func (app *p2pApp) Start(isClient bool) {
	app.maxWindowSize = MaxWindowSize

	app.PreCalcKeyBytes()
	if isClient {
		go app.daemonP2PTunnel()
	}

}

func (app *p2pApp) daemonP2PTunnel() error {
	for app.running {
		app.daemonDirectTunnel()
		if app.config.peerIP == gConf.Network.publicIP {
			time.Sleep(time.Second * 10) // if peerIP is local IP, delay relay tunnel
		}
		for i := 1; i < app.tunnelNum; i++ {
			app.daemonRelayTunnel(i)
		}

		time.Sleep(time.Second * 3)
	}
	return nil
}

func (app *p2pApp) daemonDirectTunnel() error {
	if !GNetwork.online {
		return nil
	}
	if app.config.ForceRelay == 1 && app.config.RelayNode != app.config.PeerNode {
		return nil
	}
	if app.Tunnel(0) != nil && app.Tunnel(0).isActive() {
		return nil
	}
	if app.config.nextRetryTime.After(time.Now()) || app.config.Enabled == 0 {
		return nil
	}
	if time.Now().Add(-time.Minute * 15).After(app.config.retryTime) { // run normally 15min, reset retrynum
		app.config.retryNum = 1
	}
	if app.config.retryNum > 0 { // first time not show reconnect log
		gLog.i("appid:%d checkDirectTunnel detect peer %s disconnect, reconnecting the %d times...", app.id, app.config.LogPeerNode(), app.config.retryNum)
	}
	app.config.retryNum++
	app.config.retryTime = time.Now()

	app.config.connectTime = time.Now()
	err := app.buildDirectTunnel()
	if err != nil {
		app.config.errMsg = err.Error()
		if err == ErrPeerOffline && app.config.retryNum > 2 { // stop retry, waiting for online
			app.config.retryNum = retryLimit
			gLog.i("appid:%d checkDirectTunnel %s offline, it will auto reconnect when peer node online", app.id, app.config.LogPeerNode())
		}
		if err == ErrBuildTunnelBusy {
			app.config.retryNum--
		}
	}
	interval := calcRetryTimeRelay(float64(app.config.retryNum))
	if app.preDirectSuccessIP == app.config.peerIP {
		interval = math.Min(interval, 1800) // if peerIP has been direct link succeed, retry 30min max
	}
	app.config.nextRetryTime = time.Now().Add(time.Duration(interval) * time.Second)
	if app.Tunnel(0) != nil {
		app.preDirectSuccessIP = app.config.peerIP
		app.once.Do(func() {
			go app.listen()
			// memapp also need
			for i := 1; i < app.tunnelNum; i++ {
				go app.relayHeartbeatLoop(i)
			}

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
		gLog.w("appid:%d buildDirectTunnel %s requestPeerInfo error:%s", app.id, app.config.LogPeerNode(), initErr)
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
	gLog.d("appid:%d buildDirectTunnel sync appkey to %s", app.id, app.config.LogPeerNode())
	pn.push(app.config.PeerNode, MsgPushAPPKey, &syncKeyReq)
	app.SetTunnel(t, 0)

	// if memapp notify peer addmemapp
	// if app.config.SrcPort == 0 {
	req2 := ServerSideSaveMemApp{From: gConf.Network.Node, Node: gConf.Network.Node, TunnelID: t.id, RelayTunnelID: 0, TunnelNum: uint32(app.tunnelNum), AppID: app.id, AppKey: app.key, SrcPort: uint32(app.config.SrcPort)}
	pn.push(app.config.PeerNode, MsgPushServerSideSaveMemApp, &req2)
	gLog.d("appid:%d buildDirectTunnel push %s ServerSideSaveMemApp: %s", app.id, app.config.LogPeerNode(), prettyJson(req2))

	// }
	gLog.d("appid:%d buildDirectTunnel ok. %s use tid %d", app.id, app.config.AppName, t.id)
	return nil
}

func (app *p2pApp) daemonRelayTunnel(idx int) error {
	if !GNetwork.online {
		return nil
	}
	if app.Tunnel(0) != nil && app.Tunnel(0).linkModeWeb == LinkModeIntranet { //  in the same Lan, no relay
		return nil
	}
	// if app.config.ForceRelay == 1 && (gConf.sdwan.CentralNode == app.config.PeerNode && compareVersion(app.config.peerVersion, SupportDualTunnelVersion) < 0) {
	if app.config.SrcPort == 0 && (gConf.sdwan.CentralNode == app.config.PeerNode || gConf.sdwan.CentralNode == gConf.Network.Node) { // memapp central node not build relay tunnel
		return nil
	}
	if gConf.sdwan.CentralNode != "" && idx > 1 { // if central node exist only need one relayTunnel
		return nil
	}
	app.hbMtx.Lock()
	if app.Tunnel(idx) != nil && time.Now().Before(app.hbTime[idx].Add(TunnelHeartbeatTime*2)) { // must check app.hbtime instead of relayTunnel
		app.hbMtx.Unlock()
		return nil
	}
	app.hbMtx.Unlock()
	if app.nextRetryTime[idx].After(time.Now()) || app.config.Enabled == 0 {
		return nil
	}
	if time.Now().Add(-time.Minute * 15).After(app.retryTime[idx]) { // run normally 15min, reset retrynum
		app.retryNum[idx] = 1
	}
	if app.retryNum[idx] > 0 { // first time not show reconnect log
		gLog.i("appid:%d checkRelayTunnel detect peer %s relay disconnect, reconnecting the %d times...", app.id, app.config.LogPeerNode(), app.retryNum[idx])
	}
	app.SetTunnel(nil, idx) // reset relayTunnel
	app.retryNum[idx]++
	app.retryTime[idx] = time.Now()
	app.connectTime = time.Now()
	err := app.buildRelayTunnel(idx)
	if err != nil {
		app.errMsg = err.Error()
		if err == ErrPeerOffline && app.retryNum[idx] > 2 { // stop retry, waiting for online
			app.retryNum[idx] = retryLimit
			gLog.i("appid:%d checkRelayTunnel %s offline, it will auto reconnect when peer node online", app.id, app.config.LogPeerNode())
		}
	}
	interval := calcRetryTimeRelay(float64(app.retryNum[idx]))
	app.nextRetryTime[idx] = time.Now().Add(time.Duration(interval) * time.Second)
	if app.Tunnel(idx) != nil {
		app.once.Do(func() {
			go app.listen()
			// memapp also need
			for i := 1; i < app.tunnelNum; i++ {
				go app.relayHeartbeatLoop(i)
			}
		})
	}
	return nil
}

func (app *p2pApp) buildRelayTunnel(idx int) error {
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
		gLog.w("appid:%d buildRelayTunnel %s init error:%s", app.id, config.LogPeerNode(), initErr)
		return initErr
	}
	ExcludeNodes := ""
	kk := 1 + ((idx - 1) ^ 1)
	if app.tunnelNum > 2 && app.allTunnels[kk] != nil {
		ExcludeNodes = app.allTunnels[1+((idx-1)^1)].config.PeerNode
	}
	t, rtid, relayMode, err = pn.addRelayTunnel(config, ExcludeNodes)
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
	gLog.d("appid:%d buildRelayTunnel sync appkey relay to %s", app.id, config.LogPeerNode())
	pn.push(config.PeerNode, MsgPushAPPKey, &syncKeyReq)
	app.SetRelayTunnelID(rtid, idx)
	app.SetTunnel(t, idx)
	app.relayNode[idx] = relayNode
	app.relayMode[idx] = relayMode
	app.hbTime[idx] = time.Now()

	// if memapp notify peer addmemapp
	// if config.SrcPort == 0 {
	req2 := ServerSideSaveMemApp{From: gConf.Network.Node, Node: relayNode, TunnelID: rtid, RelayTunnelID: t.id, AppID: app.id, AppKey: app.key, RelayMode: relayMode, RelayIndex: uint32(idx), TunnelNum: uint32(app.tunnelNum), SrcPort: uint32(app.config.SrcPort)}
	pn.push(config.PeerNode, MsgPushServerSideSaveMemApp, &req2)
	gLog.d("appid:%d buildRelayTunnel push %s relay ServerSideSaveMemApp: %s", app.id, config.LogPeerNode(), prettyJson(req2))
	// }
	gLog.d("appid:%d buildRelayTunnel %s use tunnel %d", app.id, app.config.AppName, t.id)
	return nil
}

func (app *p2pApp) buildOfficialTunnel() error {
	return nil
}

// cache relayHead, refresh when rtid change
func (app *p2pApp) RelayHead(idx int) *bytes.Buffer {
	if app.relayHead[idx] == nil {
		app.relayHead[idx] = new(bytes.Buffer)
		binary.Write(app.relayHead[idx], binary.LittleEndian, app.rtid[idx])
	}
	return app.relayHead[idx]
}

func (app *p2pApp) SetRelayTunnelID(rtid uint64, idx int) {
	app.rtid[idx] = rtid
	app.relayHead[idx] = new(bytes.Buffer)
	binary.Write(app.relayHead[idx], binary.LittleEndian, app.rtid[idx])
}

func (app *p2pApp) IsActive() bool {
	if t, _ := app.AvailableTunnel(); t == nil {
		// gLog.d("isActive app.tunnel==nil")
		return false
	}
	if app.Tunnel(0) != nil { // direct mode app heartbeat equals to tunnel heartbeat
		return app.Tunnel(0).isActive()
	}
	// relay mode calc app heartbeat
	app.hbMtx.Lock()
	defer app.hbMtx.Unlock()
	if app.Tunnel(1) != nil {
		return time.Now().Before(app.hbTime[1].Add(TunnelHeartbeatTime * 2))
	}
	res := time.Now().Before(app.hbTime[2].Add(TunnelHeartbeatTime * 2))
	// if !res {
	// 	gLog.d("%d app isActive false. peer=%s", app.id, app.config.PeerNode)
	// }
	return res
}

func (app *p2pApp) UpdateHeartbeat(rtid uint64) {
	app.hbMtx.Lock()
	defer app.hbMtx.Unlock()
	tidx := 1
	if app.tunnelNum > 2 && rtid == app.rtid[2] || (app.Tunnel(2) != nil && app.Tunnel(2).id == rtid) { // ack return rtid!=
		tidx = 2
	}
	app.hbTime[tidx] = time.Now()
	rtt := int32(time.Since(app.whbTime[tidx]) / time.Millisecond)
	preRtt := app.rtt[tidx].Load()
	if preRtt != DefaultRtt {
		rtt = int32(float64(preRtt)*(1-ma20) + float64(rtt)*ma20)
	}
	app.rtt[tidx].Store(rtt)
	gLog.dev("appid:%d relay heartbeat %d store rtt %d", app.id, tidx, rtt)
}

func (app *p2pApp) UpdateRelayHeartbeatTs(rtid uint64) {
	app.hbMtx.Lock()
	defer app.hbMtx.Unlock()
	relayIdx := 1
	if app.tunnelNum > 2 && rtid == app.rtid[2] || (app.Tunnel(2) != nil && app.Tunnel(2).id == rtid) { // ack return rtid!=
		relayIdx = 2
	}
	app.whbTime[relayIdx] = time.Now() // one side did not write relay hb, so write whbtime in this.
}

func (app *p2pApp) listenTCP() error {
	gLog.d("appid:%d tcp accept on port %d start", app.id, app.config.SrcPort)
	defer gLog.d("appid:%d tcp accept on port %d end", app.id, app.config.SrcPort)
	var err error
	listenAddr := ""
	if IsLocalhost(app.config.Whitelist) { // not expose port
		listenAddr = "127.0.0.1"
	}
	app.listener, err = net.Listen("tcp", fmt.Sprintf("%s:%d", listenAddr, app.config.SrcPort))
	if err != nil {
		gLog.e("appid:%d listen tcp error:%s", app.id, err)
		return err
	}
	defer app.listener.Close()
	for app.running {
		conn, err := app.listener.Accept()
		if err != nil {
			if app.running {
				gLog.e("appid:%d  accept error:%s", app.id, err)
			}
			break
		}
		t, tidx := app.AvailableTunnel()
		if t == nil {
			gLog.d("appid:%d srcPort=%d, app.Tunnel()==nil, not ready", app.id, app.config.SrcPort)
			time.Sleep(time.Second)
			continue
		}
		// check white list
		if app.config.Whitelist != "" {
			remoteIP := conn.RemoteAddr().(*net.TCPAddr).IP.String()
			if !app.iptree.Contains(remoteIP) && !IsLocalhost(remoteIP) {
				conn.Close()
				gLog.e("appid:%d %s not in whitelist, access denied", app.id, remoteIP)
				continue
			}
		}
		oConn := overlayConn{
			app:      app,
			connTCP:  conn,
			id:       rand.Uint64(),
			isClient: true,
			running:  true,
		}

		overlayConns.Store(oConn.id, &oConn)
		gLog.d("appid:%d Accept TCP overlayID:%d, %s", app.id, oConn.id, oConn.connTCP.RemoteAddr())
		// tell peer connect
		req := OverlayConnectReq{ID: oConn.id,
			Token:    gConf.Network.Token,
			DstIP:    app.config.DstHost,
			DstPort:  app.config.DstPort,
			Protocol: app.config.Protocol,
			AppID:    app.id,
		}

		if tidx != 0 {
			req.RelayTunnelID = t.id
		}
		app.WriteMessage(MsgP2P, MsgOverlayConnectReq, &req)
		head, _ := app.ReadMessage(MsgP2P, MsgOverlayConnectRsp, time.Second*3)
		if head == nil {
			gLog.w("appid:%d read MsgOverlayConnectRsp error", app.id)
		}
		go oConn.run()
	}
	return nil
}

func (app *p2pApp) listenUDP() error {
	gLog.d("appid:%d udp accept on port %d start", app.id, app.config.SrcPort)
	defer gLog.d("appid:%d udp accept on port %d end", app.id, app.config.SrcPort)
	var err error
	app.listenerUDP, err = net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: app.config.SrcPort})
	if err != nil {
		gLog.e("appid:%d listen udp error:%s", app.id, err)
		return err
	}
	defer app.listenerUDP.Close()
	buffer := make([]byte, 64*1024+PaddingSize)
	udpID := make([]byte, 8)
	for app.running {
		app.listenerUDP.SetReadDeadline(time.Now().Add(UDPReadTimeout))
		len, remoteAddr, err := app.listenerUDP.ReadFrom(buffer)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			} else {
				gLog.e("appid:%d udp read failed:%s", app.id, err)
				break
			}
		} else {
			t, tidx := app.AvailableTunnel()
			if t == nil {
				gLog.d("appid:%d srcPort=%d, app.Tunnel()==nil, not ready", app.id, app.config.SrcPort)
				time.Sleep(time.Second)
				continue
			}
			dupData := bytes.Buffer{} // should uses memory pool
			dupData.Write(buffer[:len+PaddingSize])
			// load from app.overlayConns by remoteAddr error, new udp connection
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
			s, ok := overlayConns.Load(id)
			if !ok {
				oConn := overlayConn{
					app:        app,
					connUDP:    app.listenerUDP,
					remoteAddr: remoteAddr,
					udpData:    make(chan []byte, 1000),
					id:         id,
					isClient:   true,
					running:    true,
				}
				overlayConns.Store(oConn.id, &oConn)
				gLog.d("appid:%d Accept UDP overlayID:%d", app.id, oConn.id)
				// tell peer connect
				req := OverlayConnectReq{ID: oConn.id,
					Token:    gConf.Network.Token,
					DstIP:    app.config.DstHost,
					DstPort:  app.config.DstPort,
					Protocol: app.config.Protocol,
					AppID:    app.id,
				}
				if tidx != 0 {
					req.RelayTunnelID = t.id
				}
				app.WriteMessage(MsgP2P, MsgOverlayConnectReq, &req)
				head, _ := app.ReadMessage(MsgP2P, MsgOverlayConnectRsp, time.Second*3)
				if head == nil {
					gLog.w("appid:%d read MsgOverlayConnectRsp error", app.id)
				}
				go oConn.run()
				oConn.udpData <- dupData.Bytes()
			}

			// load from overlayConns by remoteAddr ok, write relay data
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
	gLog.i("appid:%d LISTEN ON PORT %s:%d START", app.id, app.config.Protocol, app.config.SrcPort)
	defer gLog.i("appid:%d LISTEN ON PORT %s:%d END", app.id, app.config.Protocol, app.config.SrcPort)
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

func (app *p2pApp) Close() {
	app.running = false
	if app.listener != nil {
		app.listener.Close()
	}
	if app.listenerUDP != nil {
		app.listenerUDP.Close()
	}
	closeOverlayConns(app.id)
	app.wg.Wait()
}

// TODO: many relay app on the same P2PTunnel will send a lot of relay heartbeat
func (app *p2pApp) relayHeartbeatLoop(idx int) {
	app.wg.Add(1)
	defer app.wg.Done()
	gLog.d("appid:%d %s relayHeartbeat to rtid:%d start", app.id, app.config.LogPeerNode(), app.rtid[idx])
	defer gLog.d("appid:%d %s relayHeartbeat to rtid%d end", app.id, app.config.LogPeerNode(), app.rtid[idx])

	for app.running {
		if app.Tunnel(idx) == nil || !app.Tunnel(idx).isRuning() {
			time.Sleep(TunnelHeartbeatTime)
			continue
		}
		req := RelayHeartbeat{From: gConf.Network.Node, RelayTunnelID: app.Tunnel(idx).id, RelayTunnelID2: app.rtid[idx],
			AppID: app.id}
		err := app.Tunnel(idx).WriteMessage(app.rtid[idx], MsgP2P, MsgRelayHeartbeat, &req)
		if err != nil {
			gLog.e("appid:%d %s rtid:%d write relay tunnel heartbeat error %s", app.id, app.config.LogPeerNode(), app.rtid[idx], err)
			app.SetTunnel(nil, idx)
			continue
		}
		app.whbTime[idx] = time.Now()
		// TODO: debug relay heartbeat
		gLog.dev("appid:%d %s rtid:%d write relay tunnel heartbeat ok", app.id, app.config.LogPeerNode(), app.rtid[idx])
		time.Sleep(TunnelHeartbeatTime)
	}
}

func (app *p2pApp) WriteMessage(mainType uint16, subType uint16, req interface{}) error {
	t, tidx := app.AvailableTunnel()
	if t == nil {
		return ErrAppWithoutTunnel
	}
	return t.WriteMessage(app.rtid[tidx], mainType, subType, req)
}

func (app *p2pApp) WriteMessageWithAppID(mainType uint16, subType uint16, req interface{}) error {
	t, tidx := app.AvailableTunnel()
	if t == nil {
		return ErrAppWithoutTunnel
	}
	appID := app.id
	if app.config.SrcPort == 0 {
		appID = NodeNameToID(app.config.PeerNode)
	}
	return t.WriteMessageWithAppID(appID, app.rtid[tidx], mainType, subType, req)
}

func (app *p2pApp) WriteBytes(data []byte) error {
	t, tidx := app.AvailableTunnel()
	if t == nil {
		return ErrAppWithoutTunnel
	}
	if tidx == 0 {
		return t.conn.WriteBytes(MsgP2P, MsgOverlayData, data)
	}
	all := append(app.relayHead[tidx].Bytes(), encodeHeader(MsgP2P, MsgOverlayData, uint32(len(data)))...)
	all = append(all, data...)
	t.conn.WriteBytes(MsgP2P, MsgRelayData, all)

	return nil
}

func (app *p2pApp) PreCalcKeyBytes() {
	// pre-calc key bytes for encrypt
	if app.key != 0 {
		encryptKey := make([]byte, AESKeySize)
		binary.LittleEndian.PutUint64(encryptKey, app.key)
		binary.LittleEndian.PutUint64(encryptKey[8:], app.key)
		app.appKeyBytes = encryptKey
	}
}

func (app *p2pApp) WriteNodeDataMP(IPPacket []byte) (err error) {
	t, tidx := app.fastestTunnel()
	if t == nil {
		return ErrAppWithoutTunnel
	}
	dataWithSeq := new(bytes.Buffer)
	binary.Write(dataWithSeq, binary.LittleEndian, gConf.nodeID())
	binary.Write(dataWithSeq, binary.LittleEndian, app.seqW)
	dataWithSeq.Write(IPPacket)
	// gLog.d("DEBUG writeTs=%d, unAckSeqStart=%d", wu.writeTs.UnixMilli(), app.unAckSeqStart[tidx].Load())

	if tidx == 0 {
		t.asyncWriteNodeData(gConf.nodeID(), app.seqW, IPPacket, nil)
		gLog.dev("appid:%d asyncWriteDirect IPPacket len=%d", app.id, len(IPPacket))
	} else {
		t.asyncWriteNodeData(gConf.nodeID(), app.seqW, IPPacket, app.RelayHead(tidx).Bytes())
		gLog.dev("appid:%d asyncWriteRelay%d IPPacket len=%d", app.id, tidx, len(IPPacket))
	}
	app.seqW++
	return err
}

func (app *p2pApp) handleNodeDataMP(seq uint64, data []byte, t *P2PTunnel) {
		GNetwork.nodeData <- data

}

func (app *p2pApp) isReliable() bool {
	// return app.config.SrcPort != 0
	return true
}

func (app *p2pApp) AvailableTunnel() (*P2PTunnel, int) {
	for i := 0; i < app.tunnelNum; i++ {
		if app.allTunnels[i] != nil {
			return app.allTunnels[i], i
		}
	}
	return nil, 0
}

func (app *p2pApp) fastestTunnel() (t *P2PTunnel, idx int) {
	// gLog.d("appid:%d fastestTunnel %d %d",app.id, app.DirectRTT(), app.MinRelayRTT())
	if gConf.Network.specTunnel > 0 {
		if app.Tunnel(gConf.Network.specTunnel) != nil {
			return app.Tunnel(gConf.Network.specTunnel), gConf.Network.specTunnel
		}
	}
	t = app.Tunnel(0)
	idx = 0

	if app.Tunnel(1) != nil {
		t = app.Tunnel(1)
		idx = 1
	}
	return
}

func (app *p2pApp) ResetWindow() {
	app.seqW = 0
	app.seqR = 0
	for i := 0; i < app.tunnelNum; i++ {
		app.unAckSeqEnd[i].Store(0)
		app.unAckTs[i].Store(0)
		app.writeTs[i].Store(0)
	}

}

func (app *p2pApp) Retry(all bool) {
	gLog.d("appid:%d retry app %s", app.id, app.config.LogPeerNode())
	for i := 0; i < app.tunnelNum; i++ {
		app.retryNum[i] = 0
		app.nextRetryTime[i] = time.Now()
		if all && i == 0 {
			app.hbMtx.Lock()
			app.hbTime[i] = time.Now().Add(-TunnelHeartbeatTime * 3)
			app.hbMtx.Unlock()
			app.config.retryNum = 0
			app.config.nextRetryTime = time.Now()
			app.ResetWindow()
		}
	}
}

func (app *p2pApp) StoreMessage(head *openP2PHeader, body []byte) {
	app.msgChan <- appMsgCtx{head, body, time.Now()}
}

func (app *p2pApp) ReadMessage(mainType uint16, subType uint16, timeout time.Duration) (head *openP2PHeader, body []byte) {
	for {
		select {
		case <-time.After(timeout):
			gLog.e("appid:%d app.ReadMessage error %d:%d timeout", app.id, mainType, subType)
			return
		case msg := <-app.msgChan:
			if time.Since(msg.ts) > ReadMsgTimeout {
				gLog.d("appid:%d app.ReadMessage error expired %d:%d", app.id, mainType, subType)
				continue
			}
			if msg.head.MainType != mainType || msg.head.SubType != subType {
				gLog.d("appid:%d app.ReadMessage error type %d:%d, requeue it", app.id, msg.head.MainType, msg.head.SubType)
				app.msgChan <- msg
				time.Sleep(time.Second)
				continue
			}
			head = msg.head
			body = msg.body[8:]
			return
		}
	}
}

