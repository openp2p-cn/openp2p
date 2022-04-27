package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"sync"
	"time"
)

type P2PTunnel struct {
	pn            *P2PNetwork
	conn          underlay
	hbTime        time.Time
	hbMtx         sync.Mutex
	hbTimeRelay   time.Time
	config        AppConfig
	la            *net.UDPAddr // local hole address
	ra            *net.UDPAddr // remote hole address
	overlayConns  sync.Map     // both TCP and UDP
	id            uint64
	running       bool
	runMtx        sync.Mutex
	tunnelServer  bool // different from underlayServer
	coneLocalPort int
	coneNatPort   int
}

func (t *P2PTunnel) init() {
	t.running = true
	t.hbMtx.Lock()
	t.hbTime = time.Now()
	t.hbMtx.Unlock()
	t.hbTimeRelay = time.Now().Add(time.Second * 600) // TODO: test fake time
	localPort := int(rand.Uint32()%10000 + 50000)
	if t.pn.config.natType == NATCone {
		// prepare one random cone hole
		_, _, _, port1, _ := natTest(t.pn.config.ServerHost, t.pn.config.UDPPort1, localPort, 0)
		t.coneLocalPort = localPort
		t.coneNatPort = port1
		t.la = &net.UDPAddr{IP: net.ParseIP(t.pn.config.localIP), Port: t.coneLocalPort}
	} else {
		t.coneLocalPort = localPort
		t.coneNatPort = localPort // symmetric doesn't need coneNatPort
		if t.pn.config.hasUPNPorNATPMP == 1 {
			nat, err := Discover()
			if err != nil {
				gLog.Println(LvDEBUG, "could not perform UPNP discover:", err)
			} else {
				externalPort, err := nat.AddPortMapping("tcp", localPort, localPort, "openp2p", 30) // timeout the connection still alive, make the timeout short
				if err != nil {
					gLog.Println(LvDEBUG, "could not add udp UPNP port mapping", externalPort)
				}
			}
		}
		t.la = &net.UDPAddr{IP: net.ParseIP(t.pn.config.localIP), Port: t.coneLocalPort}
	}
	gLog.Printf(LvDEBUG, "prepare punching port %d:%d", t.coneLocalPort, t.coneNatPort)
}

func (t *P2PTunnel) connect() error {
	gLog.Printf(LvDEBUG, "start p2pTunnel to %s ", t.config.PeerNode)
	t.tunnelServer = false
	appKey := uint64(0)
	req := PushConnectReq{
		Token:           t.config.peerToken,
		From:            t.pn.config.Node,
		FromToken:       t.pn.config.Token,
		FromIP:          t.pn.config.publicIP,
		ConeNatPort:     t.coneNatPort,
		NatType:         t.pn.config.natType,
		HasIPv4:         t.pn.config.hasIPv4,
		IPv6:            t.pn.config.IPv6,
		HasUPNPorNATPMP: t.pn.config.hasUPNPorNATPMP,
		ID:              t.id,
		AppKey:          appKey,
		Version:         OpenP2PVersion,
	}
	t.pn.push(t.config.PeerNode, MsgPushConnectReq, req)
	head, body := t.pn.read(t.config.PeerNode, MsgPush, MsgPushConnectRsp, time.Second*10)
	if head == nil {
		return errors.New("connect error")
	}
	rsp := PushConnectRsp{}
	err := json.Unmarshal(body, &rsp)
	if err != nil {
		gLog.Printf(LvERROR, "wrong MsgPushConnectRsp:%s", err)
		return err
	}
	// gLog.Println(LevelINFO, rsp)
	if rsp.Error != 0 {
		return errors.New(rsp.Detail)
	}
	t.config.peerNatType = rsp.NatType
	t.config.hasIPv4 = rsp.HasIPv4
	t.config.IPv6 = rsp.IPv6
	t.config.hasUPNPorNATPMP = rsp.HasUPNPorNATPMP
	t.config.peerVersion = rsp.Version
	t.config.peerConeNatPort = rsp.ConeNatPort
	t.config.peerIP = rsp.FromIP
	err = t.start()
	if err != nil {
		gLog.Println(LvERROR, "handshake error:", err)
		err = ErrorHandshake
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
	t.hbMtx.Lock()
	defer t.hbMtx.Unlock()
	return time.Now().Before(t.hbTime.Add(TunnelIdleTimeout))
}

func (t *P2PTunnel) checkActive() bool {
	hbt := time.Now()
	t.hbMtx.Lock()
	if t.hbTime.Before(time.Now().Add(-TunnelHeartbeatTime)) {
		t.hbMtx.Unlock()
		return false
	}
	t.hbMtx.Unlock()
	// hbtime within TunnelHeartbeatTime, check it now
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
	return isActive
}

// call when user delete tunnel
func (t *P2PTunnel) close() {
	t.setRun(false)
	t.pn.allTunnels.Delete(t.id)
}

func (t *P2PTunnel) start() error {
	if !t.isSupportTCP() {
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
	if t.config.peerConeNatPort > 0 {
		var err error
		t.ra, err = net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", t.config.peerIP, t.config.peerConeNatPort))
		if err != nil {
			return err
		}
	}
	gLog.Println(LvDEBUG, "handshake to ", t.config.PeerNode)
	var err error
	// TODO: handle NATNone, nodes with public ip has no punching
	if (t.pn.config.natType == NATCone && t.config.peerNatType == NATCone) || (t.pn.config.natType == NATNone || t.config.peerNatType == NATNone) {
		err = handshakeC2C(t)
	} else if t.config.peerNatType == NATSymmetric && t.pn.config.natType == NATSymmetric {
		err = ErrorS2S
		t.close()
	} else if t.config.peerNatType == NATSymmetric && t.pn.config.natType == NATCone {
		err = handshakeC2S(t)
	} else if t.config.peerNatType == NATCone && t.pn.config.natType == NATSymmetric {
		err = handshakeS2C(t)
	} else {
		return errors.New("unknown error")
	}
	if err != nil {
		gLog.Println(LvERROR, "punch handshake error:", err)
		return err
	}
	gLog.Printf(LvDEBUG, "handshake to %s ok", t.config.PeerNode)
	return nil
}

func (t *P2PTunnel) connectUnderlay() (err error) {
	if !t.isSupportTCP() {
		t.conn, err = t.connectUnderlayQuic()
		if err != nil {
			return err
		}
	} else {
		// TODO: udp or tcp first?
		// prepare a la ra for udp
		// t.conn, err = t.connectUnderlayQuic()
		// TODO: support ipv6
		// if t.pn.config.hasIPv4 == 1 || t.config.hasIPv4 == 1 {
		t.conn, err = t.connectUnderlayTCP()
		if err != nil {
			return err
		}
		// }
		// if IsIPv6(t.pn.config.IPv6) && IsIPv6(t.config.IPv6) { // both have ipv6
		// 	t.conn, err = t.connectUnderlayTCP6()
		// 	if err != nil {
		// 		return err
		// 	}
		// }
	}
	t.setRun(true)
	go t.readLoop()
	go t.heartbeatLoop()
	return nil
}

func (t *P2PTunnel) connectUnderlayQuic() (c underlay, err error) {
	gLog.Println(LvINFO, "connectUnderlayQuic start")
	defer gLog.Println(LvINFO, "connectUnderlayQuic end")
	var qConn *underlayQUIC
	if t.isUnderlayServer() {
		time.Sleep(time.Millisecond * 10) // punching udp port will need some times in some env
		qConn, err = listenQuic(t.la.String(), TunnelIdleTimeout)
		if err != nil {
			gLog.Println(LvINFO, "listen quic error:", err, ", retry...")
		}
		t.pn.push(t.config.PeerNode, MsgPushUnderlayConnect, nil)
		err = qConn.Accept()
		if err != nil {
			qConn.CloseListener()
			return nil, fmt.Errorf("accept quic error:%s", err)
		}
		_, buff, err := qConn.ReadBuffer()
		if err != nil {
			qConn.listener.Close()
			return nil, fmt.Errorf("read start msg error:%s", err)
		}
		if buff != nil {
			gLog.Println(LvDEBUG, string(buff))
		}
		qConn.WriteBytes(MsgP2P, MsgTunnelHandshakeAck, []byte("OpenP2P,hello2"))
		gLog.Println(LvDEBUG, "quic connection ok")
		return qConn, nil
	}

	//else
	conn, e := net.ListenUDP("udp", t.la)
	if e != nil {
		time.Sleep(time.Millisecond * 10)
		conn, e = net.ListenUDP("udp", t.la)
		if e != nil {
			return nil, fmt.Errorf("quic listen error:%s", e)
		}
	}
	t.pn.read(t.config.PeerNode, MsgPush, MsgPushUnderlayConnect, time.Second*5)
	gLog.Println(LvDEBUG, "quic dial to ", t.ra.String())
	qConn, e = dialQuic(conn, t.ra, TunnelIdleTimeout)
	if e != nil {
		return nil, fmt.Errorf("quic dial to %s error:%s", t.ra.String(), e)
	}
	handshakeBegin := time.Now()
	qConn.WriteBytes(MsgP2P, MsgTunnelHandshake, []byte("OpenP2P,hello"))
	_, buff, err := qConn.ReadBuffer()
	if e != nil {
		qConn.listener.Close()
		return nil, fmt.Errorf("read MsgTunnelHandshake error:%s", err)
	}
	if buff != nil {
		gLog.Println(LvDEBUG, string(buff))
	}

	gLog.Println(LvINFO, "rtt=", time.Since(handshakeBegin))
	gLog.Println(LvDEBUG, "quic connection ok")
	return qConn, nil
}

// websocket
func (t *P2PTunnel) connectUnderlayTCP() (c underlay, err error) {
	gLog.Println(LvINFO, "connectUnderlayTCP start")
	defer gLog.Println(LvINFO, "connectUnderlayTCP end")
	var qConn *underlayTCP
	if t.isUnderlayServer() {
		t.pn.push(t.config.PeerNode, MsgPushUnderlayConnect, nil)
		qConn, err = listenTCP(t.coneNatPort, TunnelIdleTimeout)
		if err != nil {
			return nil, fmt.Errorf("listen TCP error:%s", err)
		}
		_, buff, err := qConn.ReadBuffer()
		if err != nil {
			qConn.listener.Close()
			return nil, fmt.Errorf("read start msg error:%s", err)
		}
		if buff != nil {
			gLog.Println(LvDEBUG, string(buff))
		}
		qConn.WriteBytes(MsgP2P, MsgTunnelHandshakeAck, []byte("OpenP2P,hello2"))
		gLog.Println(LvDEBUG, "TCP connection ok")
		return qConn, nil
	}

	//else
	t.pn.read(t.config.PeerNode, MsgPush, MsgPushUnderlayConnect, time.Second*5)
	gLog.Println(LvDEBUG, "TCP dial to ", t.ra.String())
	qConn, err = dialTCP(t.config.peerIP, t.config.peerConeNatPort)
	if err != nil {
		return nil, fmt.Errorf("TCP dial to %s error:%s", t.ra.String(), err)
	}
	handshakeBegin := time.Now()
	qConn.WriteBytes(MsgP2P, MsgTunnelHandshake, []byte("OpenP2P,hello"))
	_, buff, err := qConn.ReadBuffer()
	if err != nil {
		qConn.listener.Close()
		return nil, fmt.Errorf("read MsgTunnelHandshake error:%s", err)
	}
	if buff != nil {
		gLog.Println(LvDEBUG, string(buff))
	}

	gLog.Println(LvINFO, "rtt=", time.Since(handshakeBegin))
	gLog.Println(LvDEBUG, "TCP connection ok")
	return qConn, nil
}

func (t *P2PTunnel) connectUnderlayTCP6() (c underlay, err error) {
	gLog.Println(LvINFO, "connectUnderlayTCP start")
	defer gLog.Println(LvINFO, "connectUnderlayTCP end")
	var qConn *underlayTCP6
	if t.isUnderlayServer() {
		t.pn.push(t.config.PeerNode, MsgPushUnderlayConnect, nil)
		qConn, err = listenTCP6(t.coneNatPort, TunnelIdleTimeout)
		if err != nil {
			return nil, fmt.Errorf("listen TCP error:%s", err)
		}
		_, buff, err := qConn.ReadBuffer()
		if err != nil {
			qConn.listener.Close()
			return nil, fmt.Errorf("read start msg error:%s", err)
		}
		if buff != nil {
			gLog.Println(LvDEBUG, string(buff))
		}
		qConn.WriteBytes(MsgP2P, MsgTunnelHandshakeAck, []byte("OpenP2P,hello2"))
		gLog.Println(LvDEBUG, "TCP connection ok")
		return qConn, nil
	}

	//else
	t.pn.read(t.config.PeerNode, MsgPush, MsgPushUnderlayConnect, time.Second*5)
	gLog.Println(LvDEBUG, "TCP dial to ", t.ra.String())
	qConn, err = dialTCP6(t.config.IPv6, t.config.peerConeNatPort)
	if err != nil {
		return nil, fmt.Errorf("TCP dial to %s error:%s", t.ra.String(), err)
	}
	handshakeBegin := time.Now()
	qConn.WriteBytes(MsgP2P, MsgTunnelHandshake, []byte("OpenP2P,hello"))
	_, buff, err := qConn.ReadBuffer()
	if err != nil {
		qConn.listener.Close()
		return nil, fmt.Errorf("read MsgTunnelHandshake error:%s", err)
	}
	if buff != nil {
		gLog.Println(LvDEBUG, string(buff))
	}

	gLog.Println(LvINFO, "rtt=", time.Since(handshakeBegin))
	gLog.Println(LvDEBUG, "TCP connection ok")
	return qConn, nil
}

func (t *P2PTunnel) readLoop() {
	decryptData := make([]byte, ReadBuffLen+PaddingSize) // 16 bytes for padding
	gLog.Printf(LvDEBUG, "%d tunnel readloop start", t.id)
	for t.isRuning() {
		t.conn.SetReadDeadline(time.Now().Add(TunnelIdleTimeout))
		head, body, err := t.conn.ReadBuffer()
		if err != nil {
			if t.isRuning() {
				gLog.Printf(LvERROR, "%d tunnel read error:%s", t.id, err)
			}
			break
		}
		if head.MainType != MsgP2P {
			continue
		}
		switch head.SubType {
		case MsgTunnelHeartbeat:
			t.conn.WriteBytes(MsgP2P, MsgTunnelHeartbeatAck, nil)
			gLog.Printf(LvDEBUG, "%d read tunnel heartbeat", t.id)
		case MsgTunnelHeartbeatAck:
			t.hbMtx.Lock()
			t.hbTime = time.Now()
			t.hbMtx.Unlock()
			gLog.Printf(LvDEBUG, "%d read tunnel heartbeat ack", t.id)
		case MsgOverlayData:
			if len(body) < overlayHeaderSize {
				continue
			}
			overlayID := binary.LittleEndian.Uint64(body[:8])
			gLog.Printf(LvDEBUG, "%d tunnel read overlay data %d", t.id, overlayID)
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
		case MsgRelayData:
			gLog.Printf(LvDEBUG, "got relay data datalen=%d", head.DataLen)
			if len(body) < 8 {
				continue
			}
			tunnelID := binary.LittleEndian.Uint64(body[:8])
			t.pn.relay(tunnelID, body[8:])
		case MsgRelayHeartbeat:
			req := RelayHeartbeat{}
			err := json.Unmarshal(body, &req)
			if err != nil {
				gLog.Printf(LvERROR, "wrong RelayHeartbeat:%s", err)
				continue
			}
			gLog.Printf(LvDEBUG, "got MsgRelayHeartbeat from %d:%d", req.RelayTunnelID, req.AppID)
			relayHead := new(bytes.Buffer)
			binary.Write(relayHead, binary.LittleEndian, req.RelayTunnelID)
			msg, _ := newMessage(MsgP2P, MsgRelayHeartbeatAck, &req)
			msgWithHead := append(relayHead.Bytes(), msg...)
			t.conn.WriteBytes(MsgP2P, MsgRelayData, msgWithHead)
		case MsgRelayHeartbeatAck:
			req := RelayHeartbeat{}
			err := json.Unmarshal(body, &req)
			if err != nil {
				gLog.Printf(LvERROR, "wrong RelayHeartbeat:%s", err)
				continue
			}
			gLog.Printf(LvDEBUG, "got MsgRelayHeartbeatAck to %d", req.AppID)
			t.pn.updateAppHeartbeat(req.AppID)
		case MsgOverlayConnectReq:
			req := OverlayConnectReq{}
			err := json.Unmarshal(body, &req)
			if err != nil {
				gLog.Printf(LvERROR, "wrong MsgOverlayConnectReq:%s", err)
				continue
			}
			// app connect only accept token(not relay totp token), avoid someone using the share relay node's token
			if req.Token != t.pn.config.Token {
				gLog.Println(LvERROR, "Access Denied:", req.Token)
				continue
			}

			overlayID := req.ID
			gLog.Printf(LvDEBUG, "App:%d overlayID:%d connect %+v", req.AppID, overlayID, req)
			oConn := overlayConn{
				tunnel:   t,
				id:       overlayID,
				isClient: false,
				rtid:     req.RelayTunnelID,
				appID:    req.AppID,
				appKey:   GetKey(req.AppID),
			}
			if req.Protocol == "udp" {
				oConn.connUDP, err = net.DialUDP("udp", nil, &net.UDPAddr{IP: net.ParseIP(req.DstIP), Port: req.DstPort})
			} else {
				oConn.connTCP, err = net.DialTimeout("tcp", fmt.Sprintf("%s:%d", req.DstIP, req.DstPort), time.Second*5)
			}
			if err != nil {
				gLog.Println(LvERROR, err)
				continue
			}

			// calc key bytes for encrypt
			if oConn.appKey != 0 {
				encryptKey := make([]byte, 16)
				binary.LittleEndian.PutUint64(encryptKey, oConn.appKey)
				binary.LittleEndian.PutUint64(encryptKey[8:], oConn.appKey)
				oConn.appKeyBytes = encryptKey
			}

			t.overlayConns.Store(oConn.id, &oConn)
			go oConn.run()
		case MsgOverlayDisconnectReq:
			req := OverlayDisconnectReq{}
			err := json.Unmarshal(body, &req)
			if err != nil {
				gLog.Printf(LvERROR, "wrong OverlayDisconnectRequest:%s", err)
				continue
			}
			overlayID := req.ID
			gLog.Printf(LvDEBUG, "%d disconnect overlay connection %d", t.id, overlayID)
			i, ok := t.overlayConns.Load(overlayID)
			if ok {
				oConn := i.(*overlayConn)
				oConn.running = false
			}
		default:
		}
	}
	t.setRun(false)
	t.conn.Close()
	gLog.Printf(LvDEBUG, "%d tunnel readloop end", t.id)
}

func (t *P2PTunnel) heartbeatLoop() {
	tc := time.NewTicker(TunnelHeartbeatTime)
	defer tc.Stop()
	gLog.Printf(LvDEBUG, "%d tunnel heartbeatLoop start", t.id)
	defer gLog.Printf(LvDEBUG, "%d tunnel heartbeatLoop end", t.id)
	for t.isRuning() {
		select {
		case <-tc.C:
			// tunnel send
			err := t.conn.WriteBytes(MsgP2P, MsgTunnelHeartbeat, nil)
			if err != nil {
				gLog.Printf(LvERROR, "%d write tunnel heartbeat error %s", t.id, err)
				t.setRun(false)
				return
			}
			gLog.Printf(LvDEBUG, "%d write tunnel heartbeat ok", t.id)
		}
	}
}

func (t *P2PTunnel) listen() error {
	// notify client to connect
	rsp := PushConnectRsp{
		Error:           0,
		Detail:          "connect ok",
		To:              t.config.PeerNode,
		From:            t.pn.config.Node,
		NatType:         t.pn.config.natType,
		HasIPv4:         t.pn.config.hasIPv4,
		IPv6:            t.pn.config.IPv6,
		HasUPNPorNATPMP: t.pn.config.hasUPNPorNATPMP,
		FromIP:          t.pn.config.publicIP,
		ConeNatPort:     t.coneNatPort,
		ID:              t.id,
		Version:         OpenP2PVersion,
	}
	t.pn.push(t.config.PeerNode, MsgPushConnectRsp, rsp)
	gLog.Printf(LvDEBUG, "p2ptunnel wait for connecting")
	t.tunnelServer = true
	return t.start()
}

func (t *P2PTunnel) closeOverlayConns(appID uint64) {
	t.overlayConns.Range(func(_, i interface{}) bool {
		oConn := i.(*overlayConn)
		if oConn.appID == appID {
			if oConn.connTCP != nil {
				oConn.connTCP.Close()
				oConn.connTCP = nil
			}
			if oConn.connUDP != nil {
				oConn.connUDP.Close()
				oConn.connUDP = nil
			}
		}
		return true
	})
}

func (t *P2PTunnel) isUnderlayServer() bool {
	if t.pn.config.natType == NATNone && t.config.peerNatType != NATNone {
		return true
	}
	if t.pn.config.natType != NATNone && t.config.peerNatType == NATNone {
		return false
	}
	// NAT or both has public IP
	return t.tunnelServer
}

func (t *P2PTunnel) isSupportTCP() bool {
	if t.config.peerVersion == "" || compareVersion(t.config.peerVersion, LeastSupportTCPVersion) == LESS {
		return false
	}
	if t.pn.config.natType == NATNone || t.config.peerNatType == NATNone {
		return true
	}
	return false
}
