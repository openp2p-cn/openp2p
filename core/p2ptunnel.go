package openp2p

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"reflect"
	"sync"
	"sync/atomic"
	"time"
)

const WriteDataChanSize int = 3000

var buildTunnelMtx sync.Mutex

type P2PTunnel struct {
	conn           underlay
	hbTime         time.Time
	hbMtx          sync.Mutex
	config         AppConfig
	localHoleAddr  *net.UDPAddr // local hole address
	remoteHoleAddr *net.UDPAddr // remote hole address
	overlayConns   sync.Map     // both TCP and UDP
	id             uint64       // client side alloc rand.uint64 = server side
	running        bool
	runMtx         sync.Mutex
	tunnelServer   bool // different from underlayServer
	coneLocalPort  int
	coneNatPort    int
	linkModeWeb    string // use config.linkmode
	punchTs        uint64
	writeData      chan []byte
	writeDataSmall chan []byte
}

func (t *P2PTunnel) initPort() {
	t.running = true
	localPort := int(rand.Uint32()%15000 + 50000) // if the process has bug, will add many upnp port. use specify p2p port by param
	if t.config.linkMode == LinkModeTCP6 || t.config.linkMode == LinkModeTCP4 || t.config.linkMode == LinkModeIntranet {
		t.coneLocalPort = gConf.Network.TCPPort
		t.coneNatPort = gConf.Network.TCPPort // symmetric doesn't need coneNatPort
	}
	if t.config.linkMode == LinkModeUDPPunch {
		// prepare one random cone hole manually
		_, natPort, _ := natTest(gConf.Network.ServerHost, gConf.Network.UDPPort1, localPort)
		t.coneLocalPort = localPort
		t.coneNatPort = natPort
	}
	if t.config.linkMode == LinkModeTCPPunch {
		// prepare one random cone hole by system automatically
		_, natPort, localPort2 := natTCP(gConf.Network.ServerHost, IfconfigPort1)
		t.coneLocalPort = localPort2
		t.coneNatPort = natPort
	}
	t.localHoleAddr = &net.UDPAddr{IP: net.ParseIP(gConf.Network.localIP), Port: t.coneLocalPort}
	gLog.Printf(LvDEBUG, "prepare punching port %d:%d", t.coneLocalPort, t.coneNatPort)
}

func (t *P2PTunnel) connect() error {
	gLog.Printf(LvDEBUG, "start p2pTunnel to %s ", t.config.LogPeerNode())
	t.tunnelServer = false
	appKey := uint64(0)
	req := PushConnectReq{
		Token:            t.config.peerToken,
		From:             gConf.Network.Node,
		FromIP:           gConf.Network.publicIP,
		ConeNatPort:      t.coneNatPort,
		NatType:          gConf.Network.natType,
		HasIPv4:          gConf.Network.hasIPv4,
		IPv6:             gConf.IPv6(),
		HasUPNPorNATPMP:  gConf.Network.hasUPNPorNATPMP,
		ID:               t.id,
		AppKey:           appKey,
		Version:          OpenP2PVersion,
		LinkMode:         t.config.linkMode,
		IsUnderlayServer: t.config.isUnderlayServer ^ 1, // peer
		UnderlayProtocol: t.config.UnderlayProtocol,
	}
	if req.Token == 0 { // no relay token
		req.Token = gConf.Network.Token
	}
	GNetwork.push(t.config.PeerNode, MsgPushConnectReq, req)
	head, body := GNetwork.read(t.config.PeerNode, MsgPush, MsgPushConnectRsp, UnderlayConnectTimeout*3)
	if head == nil {
		return errors.New("connect error")
	}
	rsp := PushConnectRsp{}
	if err := json.Unmarshal(body, &rsp); err != nil {
		gLog.Printf(LvERROR, "wrong %v:%s", reflect.TypeOf(rsp), err)
		return err
	}
	// gLog.Println(LevelINFO, rsp)
	if rsp.Error != 0 {
		return errors.New(rsp.Detail)
	}
	t.config.peerNatType = rsp.NatType
	t.config.hasIPv4 = rsp.HasIPv4
	t.config.peerIPv6 = rsp.IPv6
	t.config.hasUPNPorNATPMP = rsp.HasUPNPorNATPMP
	t.config.peerVersion = rsp.Version
	t.config.peerConeNatPort = rsp.ConeNatPort
	t.config.peerIP = rsp.FromIP
	t.punchTs = rsp.PunchTs
	err := t.start()
	if err != nil {
		gLog.Println(LvERROR, "handshake error:", err)
	}
	return err
}

func (t *P2PTunnel) isRuning() bool {
	t.runMtx.Lock()
	defer t.runMtx.Unlock()
	return t.running
}

func (t *P2PTunnel) setRun(running bool) {
	t.runMtx.Lock()
	defer t.runMtx.Unlock()
	t.running = running
}

func (t *P2PTunnel) isActive() bool {
	if !t.isRuning() || t.conn == nil {
		return false
	}
	t.hbMtx.Lock()
	defer t.hbMtx.Unlock()
	res := time.Now().Before(t.hbTime.Add(TunnelHeartbeatTime * 2))
	if !res {
		gLog.Printf(LvDEBUG, "%d tunnel isActive false", t.id)
	}
	return res
}

func (t *P2PTunnel) checkActive() bool {
	if !t.isActive() {
		return false
	}
	hbt := time.Now()
	t.conn.WriteBytes(MsgP2P, MsgTunnelHeartbeat, nil)
	isActive := false
	// wait at most 5s
	for i := 0; i < 50 && !isActive; i++ {
		t.hbMtx.Lock()
		if t.hbTime.After(hbt) {
			isActive = true
		}
		t.hbMtx.Unlock()
		time.Sleep(time.Millisecond * 100)
	}
	gLog.Printf(LvINFO, "checkActive %t. hbtime=%d", isActive, t.hbTime)
	return isActive
}

// call when user delete tunnel
func (t *P2PTunnel) close() {
	GNetwork.NotifyTunnelClose(t)
	if !t.running {
		return
	}
	t.setRun(false)
	if t.conn != nil {
		t.conn.Close()
	}
	GNetwork.allTunnels.Delete(t.id)
	gLog.Printf(LvINFO, "%d p2ptunnel close %s ", t.id, t.config.LogPeerNode())
}

func (t *P2PTunnel) start() error {
	if t.config.linkMode == LinkModeUDPPunch {
		if err := t.handshake(); err != nil {
			return err
		}
	}
	err := t.connectUnderlay()
	if err != nil {
		gLog.Println(LvERROR, err)
		return err
	}
	return nil
}

func (t *P2PTunnel) handshake() error {
	if t.config.peerConeNatPort > 0 { // only peer is cone should prepare t.ra
		var err error
		t.remoteHoleAddr, err = net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", t.config.peerIP, t.config.peerConeNatPort))
		if err != nil {
			return err
		}
	}
	if compareVersion(t.config.peerVersion, SyncServerTimeVersion) < 0 {
		gLog.Printf(LvDEBUG, "peer version %s less than %s", t.config.peerVersion, SyncServerTimeVersion)
	} else {
		ts := time.Duration(int64(t.punchTs) + GNetwork.dt + GNetwork.ddtma*int64(time.Since(GNetwork.hbTime)+PunchTsDelay)/int64(NetworkHeartbeatTime) - time.Now().UnixNano())
		if ts > PunchTsDelay || ts < 0 {
			ts = PunchTsDelay
		}
		gLog.Printf(LvDEBUG, "sleep %d ms", ts/time.Millisecond)
		time.Sleep(ts)
	}
	gLog.Println(LvDEBUG, "handshake to ", t.config.LogPeerNode())
	var err error
	if gConf.Network.natType == NATCone && t.config.peerNatType == NATCone {
		err = handshakeC2C(t)
	} else if t.config.peerNatType == NATSymmetric && gConf.Network.natType == NATSymmetric {
		err = ErrorS2S
		t.close()
	} else if t.config.peerNatType == NATSymmetric && gConf.Network.natType == NATCone {
		err = handshakeC2S(t)
	} else if t.config.peerNatType == NATCone && gConf.Network.natType == NATSymmetric {
		err = handshakeS2C(t)
	} else {
		return errors.New("unknown error")
	}
	if err != nil {
		gLog.Println(LvERROR, "punch handshake error:", err)
		return err
	}
	gLog.Printf(LvDEBUG, "handshake to %s ok", t.config.LogPeerNode())
	return nil
}

func (t *P2PTunnel) connectUnderlay() (err error) {
	switch t.config.linkMode {
	case LinkModeTCP6:
		t.conn, err = t.connectUnderlayTCP6()
	case LinkModeTCP4:
		t.conn, err = t.connectUnderlayTCP()
	case LinkModeTCPPunch:
		if gConf.Network.natType == NATSymmetric || t.config.peerNatType == NATSymmetric {
			t.conn, err = t.connectUnderlayTCPSymmetric()
		} else {
			t.conn, err = t.connectUnderlayTCP()
		}
	case LinkModeIntranet:
		t.conn, err = t.connectUnderlayTCP()
	case LinkModeUDPPunch:
		t.conn, err = t.connectUnderlayUDP()

	}
	if err != nil {
		return err
	}
	if t.conn == nil {
		return errors.New("connect underlay error")
	}
	t.setRun(true)
	go t.readLoop()
	go t.writeLoop()
	return nil
}

func (t *P2PTunnel) connectUnderlayUDP() (c underlay, err error) {
	gLog.Printf(LvDEBUG, "connectUnderlayUDP %s start ", t.config.LogPeerNode())
	defer gLog.Printf(LvDEBUG, "connectUnderlayUDP %s end ", t.config.LogPeerNode())
	var ul underlay
	underlayProtocol := t.config.UnderlayProtocol
	if underlayProtocol == "" {
		underlayProtocol = "quic"
	}
	if t.config.isUnderlayServer == 1 {
		time.Sleep(time.Millisecond * 10) // punching udp port will need some times in some env
		go GNetwork.push(t.config.PeerNode, MsgPushUnderlayConnect, nil)
		if t.config.UnderlayProtocol == "kcp" {
			ul, err = listenKCP(t.localHoleAddr.String(), TunnelIdleTimeout)
		} else {
			ul, err = listenQuic(t.localHoleAddr.String(), TunnelIdleTimeout)
		}

		if err != nil {
			gLog.Printf(LvINFO, "listen %s error:%s", underlayProtocol, err)
			return nil, err
		}

		_, buff, err := ul.ReadBuffer()
		if err != nil {
			ul.Close()
			return nil, fmt.Errorf("read start msg error:%s", err)
		}
		if buff != nil {
			gLog.Println(LvDEBUG, string(buff))
		}
		ul.WriteBytes(MsgP2P, MsgTunnelHandshakeAck, []byte("OpenP2P,hello2"))
		gLog.Printf(LvDEBUG, "%s connection ok", underlayProtocol)
		return ul, nil
	}

	//else
	conn, errL := net.ListenUDP("udp", t.localHoleAddr)
	if errL != nil {
		time.Sleep(time.Millisecond * 10)
		conn, errL = net.ListenUDP("udp", t.localHoleAddr)
		if errL != nil {
			return nil, fmt.Errorf("%s listen error:%s", underlayProtocol, errL)
		}
	}
	GNetwork.read(t.config.PeerNode, MsgPush, MsgPushUnderlayConnect, ReadMsgTimeout)
	gLog.Printf(LvDEBUG, "%s dial to %s", underlayProtocol, t.remoteHoleAddr.String())
	if t.config.UnderlayProtocol == "kcp" {
		ul, errL = dialKCP(conn, t.remoteHoleAddr, TunnelIdleTimeout)
	} else {
		ul, errL = dialQuic(conn, t.remoteHoleAddr, TunnelIdleTimeout)
	}

	if errL != nil {
		return nil, fmt.Errorf("%s dial to %s error:%s", underlayProtocol, t.remoteHoleAddr.String(), errL)
	}
	handshakeBegin := time.Now()
	ul.WriteBytes(MsgP2P, MsgTunnelHandshake, []byte("OpenP2P,hello"))
	_, buff, err := ul.ReadBuffer() // TODO: kcp need timeout
	if err != nil {
		ul.Close()
		return nil, fmt.Errorf("read MsgTunnelHandshake error:%s", err)
	}
	if buff != nil {
		gLog.Println(LvDEBUG, string(buff))
	}

	gLog.Println(LvINFO, "rtt=", time.Since(handshakeBegin))
	gLog.Printf(LvINFO, "%s connection ok", underlayProtocol)
	t.linkModeWeb = LinkModeUDPPunch
	return ul, nil
}

func (t *P2PTunnel) connectUnderlayTCP() (c underlay, err error) {
	gLog.Printf(LvDEBUG, "connectUnderlayTCP %s start ", t.config.LogPeerNode())
	defer gLog.Printf(LvDEBUG, "connectUnderlayTCP %s end ", t.config.LogPeerNode())
	var ul *underlayTCP
	peerIP := t.config.peerIP
	if t.config.linkMode == LinkModeIntranet {
		peerIP = t.config.peerLanIP
	}
	// server side
	if t.config.isUnderlayServer == 1 {
		ul, err = listenTCP(peerIP, t.config.peerConeNatPort, t.coneLocalPort, t.config.linkMode, t)
		if err != nil {
			return nil, fmt.Errorf("listen TCP error:%s", err)
		}
		gLog.Println(LvINFO, "TCP connection ok")
		t.linkModeWeb = LinkModeIPv4
		if t.config.linkMode == LinkModeIntranet {
			t.linkModeWeb = LinkModeIntranet
		}
		return ul, nil
	}

	// client side
	if t.config.linkMode == LinkModeTCP4 {
		GNetwork.read(t.config.PeerNode, MsgPush, MsgPushUnderlayConnect, ReadMsgTimeout)
	} else { //tcp punch should sleep for punch the same time
		if compareVersion(t.config.peerVersion, SyncServerTimeVersion) < 0 {
			gLog.Printf(LvDEBUG, "peer version %s less than %s", t.config.peerVersion, SyncServerTimeVersion)
		} else {
			ts := time.Duration(int64(t.punchTs) + GNetwork.dt + GNetwork.ddtma*int64(time.Since(GNetwork.hbTime)+PunchTsDelay)/int64(NetworkHeartbeatTime) - time.Now().UnixNano())
			if ts > PunchTsDelay || ts < 0 {
				ts = PunchTsDelay
			}
			gLog.Printf(LvDEBUG, "sleep %d ms", ts/time.Millisecond)
			time.Sleep(ts)
		}
	}
	ul, err = dialTCP(peerIP, t.config.peerConeNatPort, t.coneLocalPort, t.config.linkMode)
	if err != nil {
		return nil, fmt.Errorf("TCP dial to %s:%d error:%s", t.config.peerIP, t.config.peerConeNatPort, err)
	}
	handshakeBegin := time.Now()
	tidBuff := new(bytes.Buffer)
	binary.Write(tidBuff, binary.LittleEndian, t.id)
	ul.WriteBytes(MsgP2P, MsgTunnelHandshake, tidBuff.Bytes()) //  tunnelID
	_, buff, err := ul.ReadBuffer()
	if err != nil {
		return nil, fmt.Errorf("read MsgTunnelHandshake error:%s", err)
	}
	if buff != nil {
		gLog.Println(LvDEBUG, "hello ", string(buff))
	}

	gLog.Println(LvINFO, "rtt=", time.Since(handshakeBegin))
	gLog.Println(LvINFO, "TCP connection ok")
	t.linkModeWeb = LinkModeIPv4
	if t.config.linkMode == LinkModeIntranet {
		t.linkModeWeb = LinkModeIntranet
	}
	return ul, nil
}

func (t *P2PTunnel) connectUnderlayTCPSymmetric() (c underlay, err error) {
	gLog.Printf(LvDEBUG, "connectUnderlayTCPSymmetric %s start ", t.config.LogPeerNode())
	defer gLog.Printf(LvDEBUG, "connectUnderlayTCPSymmetric %s end ", t.config.LogPeerNode())
	ts := time.Duration(int64(t.punchTs) + GNetwork.dt + GNetwork.ddtma*int64(time.Since(GNetwork.hbTime)+PunchTsDelay)/int64(NetworkHeartbeatTime) - time.Now().UnixNano())
	if ts > PunchTsDelay || ts < 0 {
		ts = PunchTsDelay
	}
	gLog.Printf(LvDEBUG, "sleep %d ms", ts/time.Millisecond)
	time.Sleep(ts)
	startTime := time.Now()
	t.linkModeWeb = LinkModeTCPPunch
	gotCh := make(chan *underlayTCP, 1)
	var wg sync.WaitGroup
	var success atomic.Int32
	if t.config.peerNatType == NATSymmetric { // c2s
		randPorts := rand.Perm(65532)
		for i := 0; i < SymmetricHandshakeNum; i++ {
			wg.Add(1)
			go func(port int) {
				defer wg.Done()
				ul, err := dialTCP(t.config.peerIP, port, t.coneLocalPort, LinkModeTCPPunch)
				if err != nil {
					return
				}
				if !success.CompareAndSwap(0, 1) {
					ul.Close() // only cone side close
					return
				}
				err = ul.WriteMessage(MsgP2P, MsgPunchHandshakeAck, P2PHandshakeReq{ID: t.id})
				if err != nil {
					ul.Close()
					return
				}
				_, buff, err := ul.ReadBuffer()
				if err != nil {
					gLog.Println(LvDEBUG, "c2s ul.ReadBuffer error:", err)
					return
				}
				req := P2PHandshakeReq{}
				if err = json.Unmarshal(buff, &req); err != nil {
					return
				}
				if req.ID != t.id {
					return
				}
				gLog.Printf(LvINFO, "handshakeS2C TCP ok. cost %dms", time.Since(startTime)/time.Millisecond)

				gotCh <- ul
				close(gotCh)
			}(randPorts[i] + 2)
		}

	} else { // s2c
		for i := 0; i < SymmetricHandshakeNum; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ul, err := dialTCP(t.config.peerIP, t.config.peerConeNatPort, 0, LinkModeTCPPunch)
				if err != nil {
					return
				}

				_, buff, err := ul.ReadBuffer()
				if err != nil {
					gLog.Println(LvDEBUG, "s2c ul.ReadBuffer error:", err)
					return
				}
				req := P2PHandshakeReq{}
				if err = json.Unmarshal(buff, &req); err != nil {
					return
				}
				if req.ID != t.id {
					return
				}
				err = ul.WriteMessage(MsgP2P, MsgPunchHandshakeAck, P2PHandshakeReq{ID: t.id})
				if err != nil {
					ul.Close()
					return
				}
				if success.CompareAndSwap(0, 1) {
					gotCh <- ul
					close(gotCh)
				}
			}()
		}
	}
	select {
	case <-time.After(HandshakeTimeout):
		return nil, fmt.Errorf("wait tcp handshake timeout")
	case ul := <-gotCh:
		return ul, nil
	}
}

func (t *P2PTunnel) connectUnderlayTCP6() (c underlay, err error) {
	gLog.Printf(LvDEBUG, "connectUnderlayTCP6 %s start ", t.config.LogPeerNode())
	defer gLog.Printf(LvDEBUG, "connectUnderlayTCP6 %s end ", t.config.LogPeerNode())
	var ul *underlayTCP6
	if t.config.isUnderlayServer == 1 {
		GNetwork.push(t.config.PeerNode, MsgPushUnderlayConnect, nil)
		ul, err = listenTCP6(t.coneNatPort, UnderlayConnectTimeout)
		if err != nil {
			return nil, fmt.Errorf("listen TCP6 error:%s", err)
		}
		_, buff, err := ul.ReadBuffer()
		if err != nil {
			return nil, fmt.Errorf("read start msg error:%s", err)
		}
		if buff != nil {
			gLog.Println(LvDEBUG, string(buff))
		}
		ul.WriteBytes(MsgP2P, MsgTunnelHandshakeAck, []byte("OpenP2P,hello2"))
		gLog.Println(LvDEBUG, "TCP6 connection ok")
		t.linkModeWeb = LinkModeIPv6
		return ul, nil
	}

	//else
	GNetwork.read(t.config.PeerNode, MsgPush, MsgPushUnderlayConnect, ReadMsgTimeout)
	gLog.Println(LvDEBUG, "TCP6 dial to ", t.config.peerIPv6)
	ul, err = dialTCP6(t.config.peerIPv6, t.config.peerConeNatPort)
	if err != nil || ul == nil {
		return nil, fmt.Errorf("TCP6 dial to %s:%d error:%s", t.config.peerIPv6, t.config.peerConeNatPort, err)
	}
	handshakeBegin := time.Now()
	ul.WriteBytes(MsgP2P, MsgTunnelHandshake, []byte("OpenP2P,hello"))
	_, buff, errR := ul.ReadBuffer()
	if errR != nil {
		return nil, fmt.Errorf("read MsgTunnelHandshake error:%s", errR)
	}
	if buff != nil {
		gLog.Println(LvDEBUG, string(buff))
	}

	gLog.Println(LvINFO, "rtt=", time.Since(handshakeBegin))
	gLog.Println(LvINFO, "TCP6 connection ok")
	t.linkModeWeb = LinkModeIPv6
	return ul, nil
}

func (t *P2PTunnel) readLoop() {
	decryptData := make([]byte, ReadBuffLen+PaddingSize) // 16 bytes for padding
	gLog.Printf(LvDEBUG, "%d tunnel readloop start", t.id)
	for t.isRuning() {
		t.conn.SetReadDeadline(time.Now().Add(TunnelHeartbeatTime * 2))
		head, body, err := t.conn.ReadBuffer()
		if err != nil {
			if t.isRuning() {
				gLog.Printf(LvERROR, "%d tunnel read error:%s", t.id, err)
			}
			break
		}
		if head.MainType != MsgP2P {
			gLog.Printf(LvWARN, "%d head.MainType != MsgP2P", t.id)
			continue
		}
		// TODO: replace some case implement to functions
		switch head.SubType {
		case MsgTunnelHeartbeat:
			t.hbMtx.Lock()
			t.hbTime = time.Now()
			t.hbMtx.Unlock()
			t.conn.WriteBytes(MsgP2P, MsgTunnelHeartbeatAck, nil)
			gLog.Printf(LvDev, "%d read tunnel heartbeat", t.id)
		case MsgTunnelHeartbeatAck:
			t.hbMtx.Lock()
			t.hbTime = time.Now()
			t.hbMtx.Unlock()
			gLog.Printf(LvDev, "%d read tunnel heartbeat ack", t.id)
		case MsgOverlayData:
			if len(body) < overlayHeaderSize {
				gLog.Printf(LvWARN, "%d len(body) < overlayHeaderSize", t.id)
				continue
			}
			overlayID := binary.LittleEndian.Uint64(body[:8])
			gLog.Printf(LvDev, "%d tunnel read overlay data %d bodylen=%d", t.id, overlayID, head.DataLen)
			s, ok := t.overlayConns.Load(overlayID)
			if !ok {
				// debug level, when overlay connection closed, always has some packet not found tunnel
				gLog.Printf(LvDEBUG, "%d tunnel not found overlay connection %d", t.id, overlayID)
				continue
			}
			overlayConn, ok := s.(*overlayConn)
			if !ok {
				continue
			}
			payload := body[overlayHeaderSize:]
			var err error
			if overlayConn.appKey != 0 {
				payload, _ = decryptBytes(overlayConn.appKeyBytes, decryptData, body[overlayHeaderSize:], int(head.DataLen-uint32(overlayHeaderSize)))
			}
			_, err = overlayConn.Write(payload)
			if err != nil {
				gLog.Println(LvERROR, "overlay write error:", err)
			}
		case MsgNodeData:
			t.handleNodeData(head, body, false)
		case MsgRelayNodeData:
			t.handleNodeData(head, body, true)
		case MsgRelayData:
			if len(body) < 8 {
				continue
			}
			tunnelID := binary.LittleEndian.Uint64(body[:8])
			gLog.Printf(LvDev, "relay data to %d, len=%d", tunnelID, head.DataLen-RelayHeaderSize)
			if err := GNetwork.relay(tunnelID, body[RelayHeaderSize:]); err != nil {
				gLog.Printf(LvERROR, "%s:%d relay to %d len=%d error:%s", t.config.LogPeerNode(), t.id, tunnelID, len(body), ErrRelayTunnelNotFound)
			}
		case MsgRelayHeartbeat:
			req := RelayHeartbeat{}
			if err := json.Unmarshal(body, &req); err != nil {
				gLog.Printf(LvERROR, "wrong %v:%s", reflect.TypeOf(req), err)
				continue
			}
			// TODO: debug relay heartbeat
			gLog.Printf(LvDEBUG, "read MsgRelayHeartbeat from rtid:%d,appid:%d", req.RelayTunnelID, req.AppID)
			// update app hbtime
			GNetwork.updateAppHeartbeat(req.AppID)
			req.From = gConf.Network.Node
			t.WriteMessage(req.RelayTunnelID, MsgP2P, MsgRelayHeartbeatAck, &req)
		case MsgRelayHeartbeatAck:
			req := RelayHeartbeat{}
			err := json.Unmarshal(body, &req)
			if err != nil {
				gLog.Printf(LvERROR, "wrong RelayHeartbeat:%s", err)
				continue
			}
			// TODO: debug relay heartbeat
			gLog.Printf(LvDEBUG, "read MsgRelayHeartbeatAck to appid:%d", req.AppID)
			GNetwork.updateAppHeartbeat(req.AppID)
		case MsgOverlayConnectReq:
			req := OverlayConnectReq{}
			if err := json.Unmarshal(body, &req); err != nil {
				gLog.Printf(LvERROR, "wrong %v:%s", reflect.TypeOf(req), err)
				continue
			}
			// app connect only accept token(not relay totp token), avoid someone using the share relay node's token
			if req.Token != gConf.Network.Token {
				gLog.Println(LvERROR, "Access Denied:", req.Token)
				continue
			}

			overlayID := req.ID
			gLog.Printf(LvDEBUG, "App:%d overlayID:%d connect %s:%d", req.AppID, overlayID, req.DstIP, req.DstPort)
			oConn := overlayConn{
				tunnel:   t,
				id:       overlayID,
				isClient: false,
				rtid:     req.RelayTunnelID,
				appID:    req.AppID,
				appKey:   GetKey(req.AppID),
				running:  true,
			}
			if req.Protocol == "udp" {
				oConn.connUDP, err = net.DialUDP("udp", nil, &net.UDPAddr{IP: net.ParseIP(req.DstIP), Port: req.DstPort})
			} else {
				oConn.connTCP, err = net.DialTimeout("tcp", fmt.Sprintf("%s:%d", req.DstIP, req.DstPort), ReadMsgTimeout)

			}
			if err != nil {
				gLog.Println(LvERROR, err)
				continue
			}

			// calc key bytes for encrypt
			if oConn.appKey != 0 {
				encryptKey := make([]byte, AESKeySize)
				binary.LittleEndian.PutUint64(encryptKey, oConn.appKey)
				binary.LittleEndian.PutUint64(encryptKey[8:], oConn.appKey)
				oConn.appKeyBytes = encryptKey
			}

			t.overlayConns.Store(oConn.id, &oConn)
			go oConn.run()
		case MsgOverlayDisconnectReq:
			req := OverlayDisconnectReq{}
			if err := json.Unmarshal(body, &req); err != nil {
				gLog.Printf(LvERROR, "wrong %v:%s", reflect.TypeOf(req), err)
				continue
			}
			overlayID := req.ID
			gLog.Printf(LvDEBUG, "%d disconnect overlay connection %d", t.id, overlayID)
			i, ok := t.overlayConns.Load(overlayID)
			if ok {
				oConn := i.(*overlayConn)
				oConn.Close()
			}
		default:
		}
	}
	t.close()
	gLog.Printf(LvDEBUG, "%d tunnel readloop end", t.id)
}

func (t *P2PTunnel) writeLoop() {
	t.hbMtx.Lock()
	t.hbTime = time.Now() // init
	t.hbMtx.Unlock()
	tc := time.NewTicker(TunnelHeartbeatTime)
	defer tc.Stop()
	gLog.Printf(LvDEBUG, "%s:%d tunnel writeLoop start", t.config.LogPeerNode(), t.id)
	defer gLog.Printf(LvDEBUG, "%s:%d tunnel writeLoop end", t.config.LogPeerNode(), t.id)
	for t.isRuning() {
		select {
		case buff := <-t.writeDataSmall:
			t.conn.WriteBuffer(buff)
			// gLog.Printf(LvDEBUG, "write icmp %d", time.Now().Unix())
		default:
			select {
			case buff := <-t.writeDataSmall:
				t.conn.WriteBuffer(buff)
				// gLog.Printf(LvDEBUG, "write icmp %d", time.Now().Unix())
			case buff := <-t.writeData:
				t.conn.WriteBuffer(buff)
			case <-tc.C:
				// tunnel send
				err := t.conn.WriteBytes(MsgP2P, MsgTunnelHeartbeat, nil)
				if err != nil {
					gLog.Printf(LvERROR, "%d write tunnel heartbeat error %s", t.id, err)
					t.close()
					return
				}
				gLog.Printf(LvDev, "%d write tunnel heartbeat ok", t.id)
			}
		}
	}
}

func (t *P2PTunnel) listen() error {
	// notify client to connect
	rsp := PushConnectRsp{
		Error:   0,
		Detail:  "connect ok",
		To:      t.config.PeerNode,
		From:    gConf.Network.Node,
		NatType: gConf.Network.natType,
		HasIPv4: gConf.Network.hasIPv4,
		// IPv6:            gConf.Network.IPv6,
		HasUPNPorNATPMP: gConf.Network.hasUPNPorNATPMP,
		FromIP:          gConf.Network.publicIP,
		ConeNatPort:     t.coneNatPort,
		ID:              t.id,
		PunchTs:         uint64(time.Now().UnixNano() + int64(PunchTsDelay) - GNetwork.dt),
		Version:         OpenP2PVersion,
	}
	t.punchTs = rsp.PunchTs
	// only private node set ipv6
	if t.config.fromToken == gConf.Network.Token {
		rsp.IPv6 = gConf.IPv6()
	}

	GNetwork.push(t.config.PeerNode, MsgPushConnectRsp, rsp)
	gLog.Printf(LvDEBUG, "p2ptunnel wait for connecting")
	t.tunnelServer = true
	return t.start()
}

func (t *P2PTunnel) closeOverlayConns(appID uint64) {
	t.overlayConns.Range(func(_, i interface{}) bool {
		oConn := i.(*overlayConn)
		if oConn.appID == appID {
			oConn.Close()
		}
		return true
	})
}

func (t *P2PTunnel) handleNodeData(head *openP2PHeader, body []byte, isRelay bool) {
	gLog.Printf(LvDev, "%d tunnel read node data bodylen=%d, relay=%t", t.id, head.DataLen, isRelay)
	ch := GNetwork.nodeData
	// if body[9] == 1 { // TODO: deal relay
	// 	ch = GNetwork.nodeDataSmall
	// 	gLog.Printf(LvDEBUG, "read icmp %d", time.Now().Unix())
	// }
	if isRelay {
		fromPeerID := binary.LittleEndian.Uint64(body[:8])
		ch <- &NodeData{fromPeerID, body[8:]} // TODO: cache peerNodeID; encrypt/decrypt
	} else {
		ch <- &NodeData{NodeNameToID(t.config.PeerNode), body} // TODO: cache peerNodeID; encrypt/decrypt
	}
}

func (t *P2PTunnel) asyncWriteNodeData(mainType, subType uint16, data []byte) {
	writeBytes := append(encodeHeader(mainType, subType, uint32(len(data))), data...)
	// if len(data) < 192 {
	if data[9] == 1 { // icmp
		select {
		case t.writeDataSmall <- writeBytes:
			// gLog.Printf(LvWARN, "%s:%d t.writeDataSmall write %d", t.config.PeerNode, t.id, len(t.writeDataSmall))
		default:
			gLog.Printf(LvWARN, "%s:%d t.writeDataSmall is full, drop it", t.config.LogPeerNode(), t.id)
		}
	} else {
		select {
		case t.writeData <- writeBytes:
		default:
			gLog.Printf(LvWARN, "%s:%d t.writeData is full, drop it", t.config.LogPeerNode(), t.id)
		}
	}

}

func (t *P2PTunnel) WriteMessage(rtid uint64, mainType uint16, subType uint16, req interface{}) error {
	if rtid == 0 {
		return t.conn.WriteMessage(mainType, subType, &req)
	}
	relayHead := new(bytes.Buffer)
	binary.Write(relayHead, binary.LittleEndian, rtid)
	msg, _ := newMessage(mainType, subType, &req)
	msgWithHead := append(relayHead.Bytes(), msg...)
	return t.conn.WriteBytes(mainType, MsgRelayData, msgWithHead)

}
