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
	conn          p2pConn
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
	isServer      bool // 0:server 1:client
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
		_, _, port1, _ := natTest(t.pn.config.ServerHost, t.pn.config.UDPPort1, localPort, 0)
		t.coneLocalPort = localPort
		t.coneNatPort = port1
		t.la = &net.UDPAddr{IP: net.ParseIP(t.pn.config.localIP), Port: t.coneLocalPort}
	} else {
		t.coneLocalPort = localPort
		t.coneNatPort = localPort // NATNONE or symmetric doesn't need coneNatPort
		t.la = &net.UDPAddr{IP: net.ParseIP(t.pn.config.localIP), Port: t.coneLocalPort}
	}
	gLog.Printf(LevelDEBUG, "prepare punching port %d:%d", t.coneLocalPort, t.coneNatPort)
}

func (t *P2PTunnel) connect() error {
	gLog.Printf(LevelDEBUG, "start p2pTunnel to %s ", t.config.PeerNode)
	t.isServer = false
	req := PushConnectReq{
		Token:       t.config.peerToken,
		From:        t.pn.config.Node,
		FromToken:   t.pn.config.Token,
		FromIP:      t.pn.config.publicIP,
		ConeNatPort: t.coneNatPort,
		NatType:     t.pn.config.natType,
		ID:          t.id}
	t.pn.push(t.config.PeerNode, MsgPushConnectReq, req)
	head, body := t.pn.read(t.config.PeerNode, MsgPush, MsgPushConnectRsp, time.Second*10)
	if head == nil {
		return errors.New("connect error")
	}
	rsp := PushConnectRsp{}
	err := json.Unmarshal(body, &rsp)
	if err != nil {
		gLog.Printf(LevelERROR, "wrong MsgPushConnectRsp:%s", err)
		return err
	}
	// gLog.Println(LevelINFO, rsp)
	if rsp.Error != 0 {
		return errors.New(rsp.Detail)
	}
	t.config.peerNatType = int(rsp.NatType)
	t.config.peerConeNatPort = rsp.ConeNatPort
	t.config.peerIP = rsp.FromIP
	err = t.handshake()
	if err != nil {
		gLog.Println(LevelERROR, "handshake error:", err)
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

func (t *P2PTunnel) handshake() error {
	if t.config.peerConeNatPort > 0 {
		var err error
		t.ra, err = net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", t.config.peerIP, t.config.peerConeNatPort))
		if err != nil {
			return err
		}
	}
	gLog.Println(LevelDEBUG, "handshake to ", t.config.PeerNode)
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
		gLog.Println(LevelERROR, "punch handshake error:", err)
		return err
	}
	gLog.Printf(LevelDEBUG, "handshake to %s ok", t.config.PeerNode)
	err = t.run()
	if err != nil {
		gLog.Println(LevelERROR, err)
		return err
	}
	return nil
}

func (t *P2PTunnel) run() error {
	if t.isServer {
		qConn, e := listenQuic(t.la.String(), TunnelIdleTimeout)
		if e != nil {
			gLog.Println(LevelINFO, "listen quic error:", e, ", retry...")
			time.Sleep(time.Millisecond * 10)
			qConn, e = listenQuic(t.la.String(), TunnelIdleTimeout)
			if e != nil {
				return fmt.Errorf("listen quic error:%s", e)
			}
		}
		t.pn.push(t.config.PeerNode, MsgPushQuicConnect, nil)
		e = qConn.Accept()
		if e != nil {
			qConn.CloseListener()
			return fmt.Errorf("accept quic error:%s", e)
		}
		_, buff, err := qConn.ReadMessage()
		if e != nil {
			qConn.listener.Close()
			return fmt.Errorf("read start msg error:%s", err)
		}
		if buff != nil {
			gLog.Println(LevelDEBUG, string(buff))
		}
		qConn.WriteBytes(MsgP2P, MsgTunnelHandshakeAck, []byte("OpenP2P,hello2"))
		gLog.Println(LevelDEBUG, "quic connection ok")
		t.conn = qConn
		t.setRun(true)
		go t.readLoop()
		go t.writeLoop()
		return nil
	}

	//else
	conn, e := net.ListenUDP("udp", t.la)
	if e != nil {
		time.Sleep(time.Millisecond * 10)
		conn, e = net.ListenUDP("udp", t.la)
		if e != nil {
			return fmt.Errorf("quic listen error:%s", e)
		}
	}
	t.pn.read(t.config.PeerNode, MsgPush, MsgPushQuicConnect, time.Second*5)
	gLog.Println(LevelDEBUG, "quic dial to ", t.ra.String())
	qConn, e := dialQuic(conn, t.ra, TunnelIdleTimeout)
	if e != nil {
		return fmt.Errorf("quic dial to %s error:%s", t.ra.String(), e)
	}
	handshakeBegin := time.Now()
	qConn.WriteBytes(MsgP2P, MsgTunnelHandshake, []byte("OpenP2P,hello"))
	_, buff, err := qConn.ReadMessage()
	if e != nil {
		qConn.listener.Close()
		return fmt.Errorf("read MsgTunnelHandshake error:%s", err)
	}
	if buff != nil {
		gLog.Println(LevelDEBUG, string(buff))
	}

	gLog.Println(LevelINFO, "rtt=", time.Since(handshakeBegin))
	gLog.Println(LevelDEBUG, "quic connection ok")
	t.conn = qConn
	t.setRun(true)
	go t.readLoop()
	go t.writeLoop()
	return nil
}

func (t *P2PTunnel) readLoop() {
	decryptData := make([]byte, ReadBuffLen+PaddingSize) // 16 bytes for padding
	gLog.Printf(LevelDEBUG, "%d tunnel readloop start", t.id)
	for t.isRuning() {
		t.conn.SetReadDeadline(time.Now().Add(TunnelIdleTimeout))
		head, body, err := t.conn.ReadMessage()
		if err != nil {
			if t.isRuning() {
				gLog.Printf(LevelERROR, "%d tunnel read error:%s", t.id, err)
			}
			break
		}
		if head.MainType != MsgP2P {
			continue
		}
		switch head.SubType {
		case MsgTunnelHeartbeat:
			t.conn.WriteBytes(MsgP2P, MsgTunnelHeartbeatAck, nil)
			gLog.Printf(LevelDEBUG, "%d read tunnel heartbeat", t.id)
		case MsgTunnelHeartbeatAck:
			t.hbMtx.Lock()
			t.hbTime = time.Now()
			t.hbMtx.Unlock()
			gLog.Printf(LevelDEBUG, "%d read tunnel heartbeat ack", t.id)
		case MsgOverlayData:
			if len(body) < overlayHeaderSize {
				continue
			}
			overlayID := binary.LittleEndian.Uint64(body[:8])
			gLog.Printf(LevelDEBUG, "%d tunnel read overlay data %d", t.id, overlayID)
			s, ok := t.overlayConns.Load(overlayID)
			if !ok {
				// debug level, when overlay connection closed, always has some packet not found tunnel
				gLog.Printf(LevelDEBUG, "%d tunnel not found overlay connection %d", t.id, overlayID)
				continue
			}
			overlayConn, ok := s.(*overlayTCP)
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
				gLog.Println(LevelERROR, "overlay write error:", err)
			}
		case MsgRelayData:
			gLog.Printf(LevelDEBUG, "got relay data datalen=%d", head.DataLen)
			if len(body) < 8 {
				continue
			}
			tunnelID := binary.LittleEndian.Uint64(body[:8])
			t.pn.relay(tunnelID, body[8:])
		case MsgRelayHeartbeat:
			req := RelayHeartbeat{}
			err := json.Unmarshal(body, &req)
			if err != nil {
				gLog.Printf(LevelERROR, "wrong RelayHeartbeat:%s", err)
				continue
			}
			gLog.Printf(LevelDEBUG, "got MsgRelayHeartbeat from %d:%d", req.RelayTunnelID, req.AppID)
			relayHead := new(bytes.Buffer)
			binary.Write(relayHead, binary.LittleEndian, req.RelayTunnelID)
			msg, _ := newMessage(MsgP2P, MsgRelayHeartbeatAck, &req)
			msgWithHead := append(relayHead.Bytes(), msg...)
			t.conn.WriteBytes(MsgP2P, MsgRelayData, msgWithHead)
		case MsgRelayHeartbeatAck:
			req := RelayHeartbeat{}
			err := json.Unmarshal(body, &req)
			if err != nil {
				gLog.Printf(LevelERROR, "wrong RelayHeartbeat:%s", err)
				continue
			}
			gLog.Printf(LevelDEBUG, "got MsgRelayHeartbeatAck to %d", req.AppID)
			t.pn.updateAppHeartbeat(req.AppID)
		case MsgOverlayConnectReq:
			req := OverlayConnectReq{}
			err := json.Unmarshal(body, &req)
			if err != nil {
				gLog.Printf(LevelERROR, "wrong MsgOverlayConnectReq:%s", err)
				continue
			}
			// app connect only accept token(not relay totp token), avoid someone using the share relay node's token
			if req.Token != t.pn.config.Token {
				gLog.Println(LevelERROR, "Access Denied:", req.Token)
				continue
			}

			overlayID := req.ID
			gLog.Printf(LevelDEBUG, "App:%d overlayID:%d connect %+v", req.AppID, overlayID, req)
			var conn net.Conn
			if req.Protocol == "udp" {
				conn, err = net.DialTimeout("udp", fmt.Sprintf("%s:%d", req.DstIP, req.DstPort), time.Second*5)
			} else {
				conn, err = net.DialTimeout("tcp", fmt.Sprintf("%s:%d", req.DstIP, req.DstPort), time.Second*5)
			}
			if err != nil {
				gLog.Println(LevelERROR, err)
				continue
			}
			otcp := overlayTCP{
				tunnel:   t,
				conn:     conn,
				id:       overlayID,
				isClient: false,
				rtid:     req.RelayTunnelID,
				appID:    req.AppID,
				appKey:   GetKey(req.AppID),
			}
			// calc key bytes for encrypt
			if otcp.appKey != 0 {
				encryptKey := make([]byte, 16)
				binary.LittleEndian.PutUint64(encryptKey, otcp.appKey)
				binary.LittleEndian.PutUint64(encryptKey[8:], otcp.appKey)
				otcp.appKeyBytes = encryptKey
			}

			t.overlayConns.Store(otcp.id, &otcp)
			go otcp.run()
		case MsgOverlayDisconnectReq:
			req := OverlayDisconnectReq{}
			err := json.Unmarshal(body, &req)
			if err != nil {
				gLog.Printf(LevelERROR, "wrong OverlayDisconnectRequest:%s", err)
				continue
			}
			overlayID := req.ID
			gLog.Printf(LevelDEBUG, "%d disconnect overlay connection %d", t.id, overlayID)
			i, ok := t.overlayConns.Load(overlayID)
			if ok {
				otcp := i.(*overlayTCP)
				otcp.running = false
			}
		default:
		}
	}
	t.setRun(false)
	t.conn.Close()
	gLog.Printf(LevelDEBUG, "%d tunnel readloop end", t.id)
}

func (t *P2PTunnel) writeLoop() {
	tc := time.NewTicker(TunnelHeartbeatTime)
	defer tc.Stop()
	defer gLog.Printf(LevelDEBUG, "%d tunnel writeloop end", t.id)
	for t.isRuning() {
		select {
		case <-tc.C:
			// tunnel send
			err := t.conn.WriteBytes(MsgP2P, MsgTunnelHeartbeat, nil)
			if err != nil {
				gLog.Printf(LevelERROR, "%d write tunnel heartbeat error %s", t.id, err)
				t.setRun(false)
				return
			}
			gLog.Printf(LevelDEBUG, "%d write tunnel heartbeat ok", t.id)
		}
	}
}

func (t *P2PTunnel) listen() error {
	gLog.Printf(LevelDEBUG, "p2ptunnel wait for connecting")
	t.isServer = true
	return t.handshake()
}

func (t *P2PTunnel) closeOverlayConns(appID uint64) {
	t.overlayConns.Range(func(_, i interface{}) bool {
		otcp := i.(*overlayTCP)
		if otcp.appID == appID {
			otcp.conn.Close()
		}
		return true
	})
}
