package openp2p

import (
	"bytes"
	"context"
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

const WriteDataChanSize int = 8192

var buildTunnelMtx sync.Mutex

const (
	StatusIdle    = 0
	StatusWriting = 1
)

type P2PTunnel struct {
	conn           underlay
	hbTime         time.Time
	hbMtx          sync.Mutex
	whbTime        time.Time
	config         AppConfig
	localHoleAddr  *net.UDPAddr // local hole address
	remoteHoleAddr *net.UDPAddr // remote hole address
	id             uint64       // client side alloc rand.uint64 = server side

	running        bool
	runMtx         sync.Mutex
	coneLocalPort  int
	coneNatPort    int
	linkModeWeb    string // use config.linkmode
	punchTs        uint64
	writeData      chan []byte
	writeDataSmall chan []byte
}

func (t *P2PTunnel) initPort() {
	t.running = true
	localPort := int(rand.Uint32()%8192 + 1025) // if the process has bug, will add many upnp port. use specify p2p port by param
	if t.config.linkMode == LinkModeTCP6 || t.config.linkMode == LinkModeTCP4 || t.config.linkMode == LinkModeUDP4 || t.config.linkMode == LinkModeIntranet {
		t.coneLocalPort = gConf.Network.PublicIPPort
		t.coneNatPort = gConf.Network.PublicIPPort // symmetric doesn't need coneNatPort
	}
	if t.config.linkMode == LinkModeUDPPunch {
		// prepare one random cone hole manually
		_, natPort, _ := natDetectUDP(gConf.Network.ServerIP, NATDetectPort1, localPort)
		t.coneLocalPort = localPort
		t.coneNatPort = natPort
	}
	if t.config.linkMode == LinkModeTCPPunch {
		// prepare one random cone hole by system automatically
		_, natPort, localPort2, _ := natDetectTCP(gConf.Network.ServerIP, NATDetectPort1, 0)
		t.coneLocalPort = localPort2
		t.coneNatPort = natPort
	}
	if t.config.linkMode == LinkModeTCP6 && compareVersion(t.config.peerVersion, IPv6PunchVersion) >= 0 {
		t.coneLocalPort = localPort
		t.coneNatPort = localPort
	}
	t.localHoleAddr = &net.UDPAddr{IP: net.ParseIP(gConf.Network.localIP), Port: t.coneLocalPort}
	gLog.d("prepare punching port %d:%d", t.coneLocalPort, t.coneNatPort)
}

func (t *P2PTunnel) connect() error {
	gLog.d("start p2pTunnel to %s ", t.config.LogPeerNode())
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
		gLog.e("wrong %v:%s", reflect.TypeOf(rsp), err)
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
		gLog.d("handshake error:%s", err)
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
		gLog.d("%d tunnel isActive false", t.id)
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
	gLog.d("checkActive %t. hbtime=%d", isActive, t.hbTime)
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
	gLog.i("%d p2ptunnel close %s ", t.id, t.config.LogPeerNode())
}

func (t *P2PTunnel) start() error {
	if t.config.linkMode == LinkModeUDPPunch {
		if err := t.handshake(); err != nil {
			return err
		}
	}
	err := t.connectUnderlay()
	if err != nil {
		gLog.d("connectUnderlay error:%s", err)
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
		gLog.d("peer version %s less than %s", t.config.peerVersion, SyncServerTimeVersion)
	} else {
		ts := time.Duration(int64(t.punchTs) + GNetwork.dt + GNetwork.ddtma*int64(time.Since(GNetwork.hbTime)+PunchTsDelay)/int64(NetworkHeartbeatTime) - time.Now().UnixNano())
		if ts > PunchTsDelay || ts < 0 {
			ts = PunchTsDelay
		}
		gLog.d("sleep %d ms", ts/time.Millisecond)
		time.Sleep(ts)
	}
	gLog.d("handshake to %s", t.config.LogPeerNode())
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
		gLog.d("punch handshake error:%s", err)
		return err
	}
	gLog.d("handshake to %s ok", t.config.LogPeerNode())
	return nil
}

func (t *P2PTunnel) connectUnderlay() (err error) {
	switch t.config.linkMode {
	case LinkModeTCP6:
		if compareVersion(t.config.peerVersion, IPv6PunchVersion) >= 0 {
			t.conn, err = t.connectUnderlayTCP()
		} else {
			t.conn, err = t.connectUnderlayTCP6()
		}
	case LinkModeTCP4:
		t.conn, err = t.connectUnderlayTCP()
	case LinkModeUDP4:
		t.conn, err = t.connectUnderlayUDP()
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
	gLog.d("connectUnderlayUDP %s start ", t.config.LogPeerNode())
	defer gLog.d("connectUnderlayUDP %s end ", t.config.LogPeerNode())
	var ul underlay
	underlayProtocol := t.config.UnderlayProtocol
	if underlayProtocol == "" {
		underlayProtocol = "quic"
	}
	if t.config.isUnderlayServer == 1 {
		// TODO: move to a func
		time.Sleep(time.Millisecond * 10) // punching udp port will need some times in some env
		go GNetwork.push(t.config.PeerNode, MsgPushUnderlayConnect, nil)
		if t.config.linkMode == LinkModeUDP4 {
			if v4l != nil {
				ul = v4l.getUnderlay(t.id)
			}
			if ul == nil {
				return nil, fmt.Errorf("listen UDP4 error")
			}
			gLog.d("UDP4 connection ok")
		} else {
			if t.config.UnderlayProtocol == "kcp" {
				ul, err = listenKCP(t.localHoleAddr.String(), TunnelIdleTimeout)
			} else {
				ul, err = listenQuic(t.localHoleAddr.String(), TunnelIdleTimeout)
			}
		}

		if err != nil {
			gLog.i("listen %s error:%s", underlayProtocol, err)
			return nil, err
		}

		_, buff, err := ul.ReadBuffer()
		if err != nil {
			ul.Close()
			return nil, fmt.Errorf("read start msg error:%s", err)
		}
		if buff != nil {
			gLog.d("handshake flag:%s", string(buff))
		}
		ul.WriteBytes(MsgP2P, MsgTunnelHandshakeAck, []byte("OpenP2P,hello2"))
		gLog.d("%s connection ok", underlayProtocol)
		return ul, nil
	}

	//client side
	listenAddr := t.localHoleAddr
	if t.config.linkMode == LinkModeUDP4 {
		listenAddr = &net.UDPAddr{IP: net.ParseIP(gConf.Network.localIP), Port: 0}
		t.remoteHoleAddr, err = net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", t.config.peerIP, t.config.peerConeNatPort))
		if err != nil {
			return nil, err
		}
	}
	conn, errL := net.ListenUDP("udp", listenAddr)
	if errL != nil {
		time.Sleep(time.Millisecond * 10)
		conn, errL = net.ListenUDP("udp", listenAddr)
		if errL != nil {
			return nil, fmt.Errorf("%s listen error:%s", underlayProtocol, errL)
		}
	}
	GNetwork.read(t.config.PeerNode, MsgPush, MsgPushUnderlayConnect, ReadMsgTimeout)
	gLog.d("%s dial to %s", underlayProtocol, t.remoteHoleAddr.String())
	if t.config.UnderlayProtocol == "kcp" {
		ul, errL = dialKCP(conn, t.remoteHoleAddr, UnderlayConnectTimeout)
	} else {
		ul, errL = dialQuic(conn, t.remoteHoleAddr, UnderlayConnectTimeout)
	}

	if errL != nil {
		return nil, fmt.Errorf("%s dial to %s error:%s", underlayProtocol, t.remoteHoleAddr.String(), errL)
	}
	handshakeBegin := time.Now()
	tidBuff := new(bytes.Buffer)
	binary.Write(tidBuff, binary.LittleEndian, t.id)
	ul.WriteBytes(MsgP2P, MsgTunnelHandshake, tidBuff.Bytes())
	_, buff, err := ul.ReadBuffer() // TODO: kcp need timeout
	if err != nil {
		ul.Close()
		return nil, fmt.Errorf("read MsgTunnelHandshake error:%s", err)
	}
	if buff != nil {
		gLog.d("handshake flag:%s", string(buff))
	}

	gLog.i("rtt=%dms", time.Since(handshakeBegin)/time.Millisecond)
	gLog.i("%s connection ok", underlayProtocol)
	t.linkModeWeb = LinkModeUDPPunch
	return ul, nil
}

func (t *P2PTunnel) connectUnderlayTCP() (c underlay, err error) {
	gLog.d("connectUnderlayTCP %s start ", t.config.LogPeerNode())
	defer gLog.d("connectUnderlayTCP %s end ", t.config.LogPeerNode())
	var ul underlay
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

		t.linkModeWeb = LinkModeIPv4
		if t.config.linkMode == LinkModeIntranet {
			t.linkModeWeb = LinkModeIntranet
		}
		if t.config.linkMode == LinkModeTCP6 {
			t.linkModeWeb = LinkModeIPv6
		}
		gLog.i("%s TCP connection ok", t.linkModeWeb)
		return ul, nil
	}

	// client side
	if t.config.linkMode == LinkModeTCP4 {
		GNetwork.read(t.config.PeerNode, MsgPush, MsgPushUnderlayConnect, ReadMsgTimeout)
	} else { //tcp punch should sleep for punch the same time
		if compareVersion(t.config.peerVersion, SyncServerTimeVersion) < 0 {
			gLog.d("peer version %s less than %s", t.config.peerVersion, SyncServerTimeVersion)
		} else {
			ts := time.Duration(int64(t.punchTs) + GNetwork.dt + GNetwork.ddtma*int64(time.Since(GNetwork.hbTime)+PunchTsDelay)/int64(NetworkHeartbeatTime) - time.Now().UnixNano())
			if ts > PunchTsDelay || ts < 0 {
				ts = PunchTsDelay
			}
			gLog.d("sleep %d ms", ts/time.Millisecond)
			time.Sleep(ts)
		}
	}
	host := peerIP
	if t.config.linkMode == LinkModeTCP6 {
		host = t.config.peerIPv6
	}
	ul, err = dialTCP(host, t.config.peerConeNatPort, t.coneLocalPort, t.config.linkMode)
	if err != nil {
		return nil, fmt.Errorf("TCP dial to %s:%d error:%s", host, t.config.peerConeNatPort, err)
	}
	handshakeBegin := time.Now()
	tidBuff := new(bytes.Buffer)
	binary.Write(tidBuff, binary.LittleEndian, t.id)
	// fake_http_hostname := "speedtest.cn"
	// user_agent := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	// ul.WriteMessage(MsgP2P, 100, fmt.Sprintf("GET / HTTP/1.1\r\nHost: %s\r\nUser-Agent: %s\r\nAccept: */*\r\n\r\n",
	// 	fake_http_hostname, user_agent))
	ul.WriteBytes(MsgP2P, MsgTunnelHandshake, tidBuff.Bytes()) //  tunnelID
	_, buff, err := ul.ReadBuffer()
	if err != nil {
		return nil, fmt.Errorf("read MsgTunnelHandshake error:%s", err)
	}
	if buff != nil {
		gLog.d("hello %s", string(buff))
	}

	gLog.i("rtt=%dms", time.Since(handshakeBegin)/time.Millisecond)
	t.linkModeWeb = LinkModeIPv4
	if t.config.linkMode == LinkModeIntranet {
		t.linkModeWeb = LinkModeIntranet
	}
	if t.config.linkMode == LinkModeTCP6 {
		t.linkModeWeb = LinkModeIPv6
	}
	gLog.i("%s TCP connection ok", t.linkModeWeb)
	return ul, nil
}

func (t *P2PTunnel) connectUnderlayTCPSymmetric() (c underlay, err error) {
	gLog.d("connectUnderlayTCPSymmetric %s start ", t.config.LogPeerNode())
	defer gLog.d("connectUnderlayTCPSymmetric %s end ", t.config.LogPeerNode())
	ts := time.Duration(int64(t.punchTs) + GNetwork.dt + GNetwork.ddtma*int64(time.Since(GNetwork.hbTime)+PunchTsDelay)/int64(NetworkHeartbeatTime) - time.Now().UnixNano())
	if ts > PunchTsDelay || ts < 0 {
		ts = PunchTsDelay
	}
	gLog.d("sleep %d ms", ts/time.Millisecond)
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
				if err != nil || buff == nil {
					gLog.d("c2s ul.ReadBuffer error:%s", err)
					return
				}
				req := P2PHandshakeReq{}
				if err = json.Unmarshal(buff, &req); err != nil {
					return
				}
				if req.ID != t.id {
					return
				}
				gLog.i("handshakeS2C TCP ok. cost %dms", time.Since(startTime)/time.Millisecond)

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
				if err != nil || buff == nil {
					gLog.d("s2c ul.ReadBuffer error:%s", err)
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
	gLog.d("connectUnderlayTCP6 %s start ", t.config.LogPeerNode())
	defer gLog.d("connectUnderlayTCP6 %s end ", t.config.LogPeerNode())
	tidBuff := new(bytes.Buffer)
	binary.Write(tidBuff, binary.LittleEndian, t.id)
	if t.config.isUnderlayServer == 1 {
		GNetwork.push(t.config.PeerNode, MsgPushUnderlayConnect, nil)
		// ul, err = listenTCP6(t.coneNatPort, UnderlayConnectTimeout)
		tid := t.id
		if compareVersion(t.config.peerVersion, PublicIPVersion) < 0 { // old version
			ipBytes := net.ParseIP(t.config.peerIP).To4()
			tid = uint64(binary.BigEndian.Uint32(ipBytes))
			gLog.d("compatible with old client, use ip as key:%d", tid)
		}

		if v4l != nil {
			c = v4l.getUnderlay(tid)
		}
		if c == nil {
			return nil, fmt.Errorf("listen TCP6 error:%s", err)
		}
		_, buff, err := c.ReadBuffer()
		if err != nil {
			return nil, fmt.Errorf("read start msg error:%s", err)
		}
		if buff != nil {
			gLog.d("handshake flag:%s", string(buff))
		}
		c.WriteBytes(MsgP2P, MsgTunnelHandshake, tidBuff.Bytes()) //  tunnelID
		// ul.WriteBytes(MsgP2P, MsgTunnelHandshakeAck, []byte("OpenP2P,hello2"))
		gLog.d("TCP6 connection ok")
		t.linkModeWeb = LinkModeIPv6
		return c, nil
	}

	//else
	GNetwork.read(t.config.PeerNode, MsgPush, MsgPushUnderlayConnect, ReadMsgTimeout)
	gLog.d("TCP6 dial to %s", t.config.peerIPv6)
	ul, err := dialTCP(fmt.Sprintf("[%s]", t.config.peerIPv6), t.config.peerConeNatPort, 0, LinkModeTCP6)
	if err != nil || ul == nil {
		return nil, fmt.Errorf("TCP6 dial to %s:%d error:%s", t.config.peerIPv6, t.config.peerConeNatPort, err)
	}
	handshakeBegin := time.Now()
	ul.WriteBytes(MsgP2P, MsgTunnelHandshake, tidBuff.Bytes()) //  tunnelID
	// ul.WriteBytes(MsgP2P, MsgTunnelHandshake, []byte("OpenP2P,hello"))
	_, buff, errR := ul.ReadBuffer()
	if errR != nil {
		return nil, fmt.Errorf("read MsgTunnelHandshake error:%s", errR)
	}
	if buff != nil {
		gLog.d("handshake flag:%s", string(buff))
	}

	gLog.i("rtt=%dms", time.Since(handshakeBegin))
	gLog.i("TCP6 connection ok")
	t.linkModeWeb = LinkModeIPv6
	return ul, nil
}

func (t *P2PTunnel) readLoop() {
	decryptData := make([]byte, ReadBuffLen+PaddingSize) // 16 bytes for padding
	gLog.d("%d tunnel readloop start", t.id)
	for t.isRuning() {
		t.conn.SetReadDeadline(time.Now().Add(TunnelHeartbeatTime * 2))
		head, body, err := t.conn.ReadBuffer()
		if err != nil || head == nil {
			if t.isRuning() {
				gLog.w("%d tunnel read error:%s", t.id, err)
			}
			break
		}
		if head.MainType != MsgP2P {
			gLog.w("%d head.MainType(%d) != MsgP2P", head.MainType, t.id)
			continue
		}
		// gLog.d("%d tunnel read %d:%d len=%d", t.id, head.MainType, head.SubType, head.DataLen)
		// TODO: replace some case implement to functions
		switch head.SubType {
		case MsgTunnelHeartbeat:
			t.hbMtx.Lock()
			t.hbTime = time.Now()
			t.hbMtx.Unlock()
			memAppPeerID := new(bytes.Buffer)
			binary.Write(memAppPeerID, binary.LittleEndian, gConf.Network.nodeID)
			t.conn.WriteBytes(MsgP2P, MsgTunnelHeartbeatAck, memAppPeerID.Bytes())
			gLog.dev("%d read tunnel heartbeat", t.id)
		case MsgTunnelHeartbeatAck:
			t.hbMtx.Lock()
			t.hbTime = time.Now()
			t.hbMtx.Unlock()
			if head.DataLen >= 8 {
				memAppPeerID := binary.LittleEndian.Uint64(body[:8])
				existApp, appok := GNetwork.apps.Load(memAppPeerID)
				if appok {
					app := existApp.(*p2pApp)
					app.rtt[0].Store(int32(time.Since(t.whbTime) / time.Millisecond))
				}
			}

			gLog.dev("%d read tunnel heartbeat ack, rtt=%dms", t.id, time.Since(t.whbTime)/time.Millisecond)
		case MsgOverlayData:
			if len(body) < overlayHeaderSize {
				gLog.w("%d len(body) < overlayHeaderSize", t.id)
				continue
			}
			overlayID := binary.LittleEndian.Uint64(body[:8])
			gLog.dev("%d tunnel read overlay data %d bodylen=%d", t.id, overlayID, head.DataLen)
			s, ok := overlayConns.Load(overlayID)
			if !ok {
				// debug level, when overlay connection closed, always has some packet not found tunnel
				gLog.d("%d tunnel not found overlay connection %d", t.id, overlayID)
				continue
			}
			overlayConn, ok := s.(*overlayConn)
			if !ok {
				continue
			}
			payload := body[overlayHeaderSize:]
			var err error
			if overlayConn.app.key != 0 {
				payload, _ = decryptBytes(overlayConn.app.appKeyBytes, decryptData, body[overlayHeaderSize:], int(head.DataLen-uint32(overlayHeaderSize)))
			}
			_, err = overlayConn.Write(payload)
			if err != nil {
				gLog.e("overlay write error:%s", err)
			}
		case MsgNodeDataMP:
			t.handleNodeDataMP(head, body)
		case MsgNodeDataMPAck:
			t.handleNodeDataMPAck(head, body)
		case MsgNodeData: // unused
			t.handleNodeData(head, body, false)
		case MsgRelayNodeData: // unused
			t.handleNodeData(head, body, true)
		case MsgRelayData:
			if len(body) < 8 {
				continue
			}
			tunnelID := binary.LittleEndian.Uint64(body[:8])
			gLog.dev("relay data to %d, len=%d", tunnelID, head.DataLen-RelayHeaderSize)
			if err := GNetwork.relay(tunnelID, body[RelayHeaderSize:]); err != nil {
				gLog.d("%s:%d relay to %d len=%d error:%s", t.config.LogPeerNode(), t.id, tunnelID, len(body), ErrRelayTunnelNotFound)
			}
		case MsgRelayHeartbeat: // only client side will write relay heartbeat, different with tunnel heartbeat
			req := RelayHeartbeat{}
			if err := json.Unmarshal(body, &req); err != nil {
				gLog.e("wrong %v:%s", reflect.TypeOf(req), err)
				continue
			}
			// TODO: debug relay heartbeat
			gLog.dev("read MsgRelayHeartbeat from rtid:%d,appid:%d", req.RelayTunnelID, req.AppID)
			// update app hbtime
			GNetwork.updateAppHeartbeat(req.AppID, req.RelayTunnelID, true)
			req.From = gConf.Network.Node
			t.WriteMessage(req.RelayTunnelID, MsgP2P, MsgRelayHeartbeatAck, &req)
		case MsgRelayHeartbeatAck:
			req := RelayHeartbeat{}
			err := json.Unmarshal(body, &req)
			if err != nil {
				gLog.e("wrong RelayHeartbeat:%s", err)
				continue
			}
			// TODO: debug relay heartbeat
			gLog.dev("read MsgRelayHeartbeatAck to appid:%d", req.AppID)
			GNetwork.updateAppHeartbeat(req.AppID, req.RelayTunnelID, false)
			req.From = gConf.Network.Node
			t.WriteMessage(req.RelayTunnelID2, MsgP2P, MsgRelayHeartbeatAck2, &req)
		case MsgRelayHeartbeatAck2:
			req := RelayHeartbeat{}
			err := json.Unmarshal(body, &req)
			if err != nil {
				gLog.e("wrong RelayHeartbeat:%s", err)
				continue
			}
			gLog.dev("read MsgRelayHeartbeatAck2 to appid:%d", req.AppID)
			GNetwork.updateAppHeartbeat(req.AppID, req.RelayTunnelID, false)
		case MsgOverlayConnectReq: // TODO: send this msg withAppID, and app handle it
			// app connect only accept token(not relay totp token), avoid someone using the share relay node's token
			// targetApp := GNetwork.GetAPPByID(req.AppID)
			t.handleOverlayConnectReq(body, err)
		case MsgOverlayConnectRsp:
			appID := binary.LittleEndian.Uint64(body[:8])
			i, ok := GNetwork.apps.Load(appID)
			if !ok {
				gLog.e("MsgOverlayConnectRsp app not found %d", appID)
				return
			}
			app := i.(*p2pApp)
			// ndmp := NodeDataMPHeader{fromNodeID: gConf.Network.nodeID, seq: seq}
			app.StoreMessage(head, body)
		case MsgOverlayDisconnectReq:
			req := OverlayDisconnectReq{}
			if err := json.Unmarshal(body, &req); err != nil {
				gLog.e("wrong %v:%s", reflect.TypeOf(req), err)
				continue
			}
			overlayID := req.ID
			gLog.d("%d disconnect overlay connection %d", t.id, overlayID)
			i, ok := overlayConns.Load(overlayID)
			if ok {
				oConn := i.(*overlayConn)
				oConn.Close()
			}
		default:
		}
	}
	t.close()
	gLog.d("%d tunnel readloop end", t.id)
}

func (*P2PTunnel) handleOverlayConnectReq(body []byte, err error) {
	req := OverlayConnectReq{}
	if err := json.Unmarshal(body, &req); err != nil {
		gLog.e("wrong %v:%s", reflect.TypeOf(req), err)
		return
	}

	if req.Token != gConf.Network.Token {
		gLog.e("Access Denied,token=%d", req.Token)
		return
	}

	overlayID := req.ID
	gLog.d("App:%d overlayID:%d connect %s:%d", req.AppID, overlayID, req.DstIP, req.DstPort)

	i, ok := GNetwork.apps.Load(req.AppID)
	if !ok {
		return
	}
	targetApp := i.(*p2pApp)
	oConn := overlayConn{
		app:      targetApp,
		id:       overlayID,
		isClient: false,
		running:  true,
	}
	// connect local service should use sys dns
	sysResolver := &net.Resolver{}
	ips, err := sysResolver.LookupIP(context.Background(), "ip4", req.DstIP)
	if err != nil {
		gLog.e("handleOverlayConnectReq dial error:%s", err)
		return
	}
	if req.Protocol == "udp" {
		oConn.connUDP, err = net.DialUDP("udp", nil, &net.UDPAddr{IP: ips[0], Port: req.DstPort})
	} else {
		oConn.connTCP, err = net.DialTimeout("tcp", fmt.Sprintf("%s:%d", ips[0].String(), req.DstPort), ReadMsgTimeout)

	}
	if err != nil {
		gLog.e("handleOverlayConnectReq dial error:%s", err)
		return
	}
	overlayConns.Store(oConn.id, &oConn)
	go oConn.run()
	targetApp.WriteMessageWithAppID(MsgP2P, MsgOverlayConnectRsp, nil)
}

func (t *P2PTunnel) writeLoop() {
	t.hbMtx.Lock()
	t.hbTime = time.Now() // init
	t.hbMtx.Unlock()
	tc := time.NewTicker(TunnelHeartbeatTime)
	defer tc.Stop()
	gLog.d("%s:%d tunnel writeLoop start", t.config.LogPeerNode(), t.id)
	defer gLog.d("%s:%d tunnel writeLoop end", t.config.LogPeerNode(), t.id)
	writeHb := func() {
		// tunnel send
		t.whbTime = time.Now()
		err := t.conn.WriteBytes(MsgP2P, MsgTunnelHeartbeat, nil)
		if err != nil {
			gLog.w("%d write tunnel heartbeat error %s", t.id, err)
			t.close()
			return
		}
		gLog.dev("%d write tunnel heartbeat ok", t.id)
	}
	writeHb()
	for t.isRuning() {
		select {
		case buff := <-t.writeDataSmall:
			t.conn.WriteBuffer(buff)
			// gLog.d("write icmp %d", time.Now().Unix())
		default:
			select {
			case buff := <-t.writeDataSmall:
				t.conn.WriteBuffer(buff)
				// gLog.d("write icmp %d", time.Now().Unix())
			case buff := <-t.writeData:
				err := t.conn.WriteBuffer(buff)
				if err != nil {
					gLog.e("%d write tunnel error %s", t.id, err)
					t.close()
					return
				}
			case <-tc.C:
				writeHb()
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
	gLog.d("p2ptunnel wait for connecting")
	return t.start()
}

func (t *P2PTunnel) handleNodeData(head *openP2PHeader, body []byte, isRelay bool) {
	gLog.dev("%d tunnel read node data bodylen=%d, relay=%t", t.id, head.DataLen, isRelay)
	ch := GNetwork.nodeData
	// if body[9] == 1 { // TODO: deal relay
	// 	ch = GNetwork.nodeDataSmall
	// 	gLog.d("read icmp %d", time.Now().Unix())
	// }
	if isRelay {
		// fromPeerID := binary.LittleEndian.Uint64(body[:8]) // unused
		ch <- body[8:] // TODO: cache peerNodeID; encrypt/decrypt
	} else {
		ch <- body // TODO: cache peerNodeID; encrypt/decrypt
	}
}

func (t *P2PTunnel) handleNodeDataMP(head *openP2PHeader, body []byte) {
	gLog.dev("%s tid:%d tunnel read node data mp bodylen=%d", t.config.LogPeerNode(), t.id, head.DataLen) // Debug
	if head.DataLen < 16 {
		return
	}

	// TODO: reorder write tun
	fromNodeID := binary.LittleEndian.Uint64(body[:8])
	seq := binary.LittleEndian.Uint64(body[8:16])
	i, ok := GNetwork.apps.Load(fromNodeID)
	if !ok {
		gLog.e("handleNodeDataMP peer not found,from=%s nodeID=%d, seq=%d", t.config.LogPeerNode(), fromNodeID, seq)
		return
	}
	app := i.(*p2pApp)
	// ndmp := NodeDataMPHeader{fromNodeID: gConf.Network.nodeID, seq: seq}
	app.handleNodeDataMP(seq, body[16:], t)

}
func (t *P2PTunnel) handleNodeDataMPAck(head *openP2PHeader, body []byte) {

}

func (t *P2PTunnel) asyncWriteNodeData(id uint64, seq uint64, IPPacket []byte, relayHead []byte) {
	all := new(bytes.Buffer)
	if relayHead != nil {
		all.Write(encodeHeader(MsgP2P, MsgRelayData, uint32(openP2PHeaderSize+len(relayHead)+16+len(IPPacket))))
		all.Write(relayHead)
	}
	all.Write(encodeHeader(MsgP2P, MsgNodeDataMP, 16+uint32(len(IPPacket)))) // id+seq=16 bytes
	binary.Write(all, binary.LittleEndian, id)
	binary.Write(all, binary.LittleEndian, seq)
	all.Write(IPPacket)
	// if len(data) < 192 {
	if IPPacket[9] == 1 { // icmp
		select {
		case t.writeDataSmall <- all.Bytes():
			// gLog.w("%s:%d t.writeDataSmall write %d", t.config.PeerNode, t.id, len(t.writeDataSmall))
		default:
			gLog.w("%s:%d t.writeDataSmall is full, drop it", t.config.LogPeerNode(), t.id)
		}
	} else {
		t.writeData <- all.Bytes()
		// select {
		// case t.writeData <- writeBytes:
		// default:
		// 	gLog.w("%s:%d t.writeData is full, drop it", t.config.LogPeerNode(), t.id)
		// }
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

func (t *P2PTunnel) WriteMessageWithAppID(appID uint64, rtid uint64, mainType uint16, subType uint16, req interface{}) error {
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	head := new(bytes.Buffer)
	binary.Write(head, binary.LittleEndian, appID)
	msgWithAppID := append(head.Bytes(), data...)
	if rtid == 0 {
		return t.conn.WriteBytes(mainType, subType, msgWithAppID)
	}
	relayHead := new(bytes.Buffer)
	binary.Write(relayHead, binary.LittleEndian, rtid)
	msg, _ := newMessageWithBuff(mainType, subType, msgWithAppID)
	msgWithHead := append(relayHead.Bytes(), msg...)
	return t.conn.WriteBytes(mainType, MsgRelayData, msgWithHead)

}

func (t *P2PTunnel) WriteBytes(rtid uint64, mainType uint16, subType uint16, data []byte) error {
	if rtid == 0 {
		return t.conn.WriteBytes(mainType, subType, data)
	}
	all := new(bytes.Buffer)
	binary.Write(all, binary.LittleEndian, rtid)
	all.Write(encodeHeader(mainType, subType, uint32(len(data))))
	all.Write(data)
	return t.conn.WriteBytes(mainType, MsgRelayData, all.Bytes())

}

// func (t *P2PTunnel) RTT() int {
// 	if t.isWriting.Load() && t.rtt.Load() < int32(time.Now().Add(time.Duration(-t.writingTs.Load())).Unix()/int64(time.Millisecond)) {
// 		return int(time.Now().Add(time.Duration(-t.writingTs.Load())).Unix() / int64(time.Millisecond))
// 	}
// 	return int(t.rtt.Load())
// }
