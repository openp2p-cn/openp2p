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
	config      AppConfig
	listener    net.Listener
	listenerUDP *net.UDPConn
	tunnel      *P2PTunnel
	iptree      *IPTree
	rtid        uint64 // relay tunnelID
	relayNode   string
	relayMode   string
	hbTime      time.Time
	hbMtx       sync.Mutex
	running     bool
	id          uint64
	key         uint64
	wg          sync.WaitGroup
}

func (app *p2pApp) isActive() bool {
	if app.tunnel == nil {
		return false
	}
	if app.rtid == 0 { // direct mode app heartbeat equals to tunnel heartbeat
		return app.tunnel.isActive()
	}
	// relay mode calc app heartbeat
	app.hbMtx.Lock()
	defer app.hbMtx.Unlock()
	return time.Now().Before(app.hbTime.Add(TunnelIdleTimeout))
}

func (app *p2pApp) updateHeartbeat() {
	app.hbMtx.Lock()
	defer app.hbMtx.Unlock()
	app.hbTime = time.Now()
}

func (app *p2pApp) listenTCP() error {
	gLog.Printf(LvDEBUG, "tcp accept on port %d start", app.config.SrcPort)
	defer gLog.Printf(LvDEBUG, "tcp accept on port %d end", app.config.SrcPort)
	var err error
	app.listener, err = net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", app.config.SrcPort)) // support tcp4 and tcp6
	if err != nil {
		gLog.Printf(LvERROR, "listen error:%s", err)
		return err
	}
	for app.running {
		conn, err := app.listener.Accept()
		if err != nil {
			if app.running {
				gLog.Printf(LvERROR, "%d accept error:%s", app.id, err)
			}
			break
		}
		// check white list
		if app.config.Whitelist != "" {
			remoteIP := strings.Split(conn.RemoteAddr().String(), ":")[0]
			if !app.iptree.Contains(remoteIP) {
				conn.Close()
				gLog.Printf(LvERROR, "%s not in whitelist, access denied", remoteIP)
				continue
			}
		}
		oConn := overlayConn{
			tunnel:   app.tunnel,
			connTCP:  conn,
			id:       rand.Uint64(),
			isClient: true,
			rtid:     app.rtid,
			appID:    app.id,
			appKey:   app.key,
			running:  true,
		}
		// pre-calc key bytes for encrypt
		if oConn.appKey != 0 {
			encryptKey := make([]byte, AESKeySize)
			binary.LittleEndian.PutUint64(encryptKey, oConn.appKey)
			binary.LittleEndian.PutUint64(encryptKey[8:], oConn.appKey)
			oConn.appKeyBytes = encryptKey
		}
		app.tunnel.overlayConns.Store(oConn.id, &oConn)
		gLog.Printf(LvDEBUG, "Accept TCP overlayID:%d, %s", oConn.id, oConn.connTCP.RemoteAddr())
		// tell peer connect
		req := OverlayConnectReq{ID: oConn.id,
			Token:    app.tunnel.pn.config.Token,
			DstIP:    app.config.DstHost,
			DstPort:  app.config.DstPort,
			Protocol: app.config.Protocol,
			AppID:    app.id,
		}
		if app.rtid == 0 {
			app.tunnel.conn.WriteMessage(MsgP2P, MsgOverlayConnectReq, &req)
		} else {
			req.RelayTunnelID = app.tunnel.id
			relayHead := new(bytes.Buffer)
			binary.Write(relayHead, binary.LittleEndian, app.rtid)
			msg, _ := newMessage(MsgP2P, MsgOverlayConnectReq, &req)
			msgWithHead := append(relayHead.Bytes(), msg...)
			app.tunnel.conn.WriteBytes(MsgP2P, MsgRelayData, msgWithHead)
		}
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
			s, ok := app.tunnel.overlayConns.Load(id)
			if !ok {
				oConn := overlayConn{
					tunnel:     app.tunnel,
					connUDP:    app.listenerUDP,
					remoteAddr: remoteAddr,
					udpData:    make(chan []byte, 1000),
					id:         id,
					isClient:   true,
					rtid:       app.rtid,
					appID:      app.id,
					appKey:     app.key,
					running:    true,
				}
				// calc key bytes for encrypt
				if oConn.appKey != 0 {
					encryptKey := make([]byte, AESKeySize)
					binary.LittleEndian.PutUint64(encryptKey, oConn.appKey)
					binary.LittleEndian.PutUint64(encryptKey[8:], oConn.appKey)
					oConn.appKeyBytes = encryptKey
				}
				app.tunnel.overlayConns.Store(oConn.id, &oConn)
				gLog.Printf(LvDEBUG, "Accept UDP overlayID:%d", oConn.id)
				// tell peer connect
				req := OverlayConnectReq{ID: oConn.id,
					Token:    app.tunnel.pn.config.Token,
					DstIP:    app.config.DstHost,
					DstPort:  app.config.DstPort,
					Protocol: app.config.Protocol,
					AppID:    app.id,
				}
				if app.rtid == 0 {
					app.tunnel.conn.WriteMessage(MsgP2P, MsgOverlayConnectReq, &req)
				} else {
					req.RelayTunnelID = app.tunnel.id
					relayHead := new(bytes.Buffer)
					binary.Write(relayHead, binary.LittleEndian, app.rtid)
					msg, _ := newMessage(MsgP2P, MsgOverlayConnectReq, &req)
					msgWithHead := append(relayHead.Bytes(), msg...)
					app.tunnel.conn.WriteBytes(MsgP2P, MsgRelayData, msgWithHead)
				}
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
	gLog.Printf(LvINFO, "LISTEN ON PORT %s:%d START", app.config.Protocol, app.config.SrcPort)
	defer gLog.Printf(LvINFO, "LISTEN ON PORT %s:%d END", app.config.Protocol, app.config.SrcPort)
	app.wg.Add(1)
	defer app.wg.Done()
	app.running = true
	if app.rtid != 0 {
		go app.relayHeartbeatLoop()
	}
	for app.tunnel.isRuning() {
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
	if app.tunnel != nil {
		app.tunnel.closeOverlayConns(app.id)
	}
	app.wg.Wait()
}

// TODO: many relay app on the same P2PTunnel will send a lot of relay heartbeat
func (app *p2pApp) relayHeartbeatLoop() {
	app.wg.Add(1)
	defer app.wg.Done()
	gLog.Printf(LvDEBUG, "relayHeartbeat to %d start", app.rtid)
	defer gLog.Printf(LvDEBUG, "relayHeartbeat to %d end", app.rtid)
	relayHead := new(bytes.Buffer)
	binary.Write(relayHead, binary.LittleEndian, app.rtid)
	req := RelayHeartbeat{RelayTunnelID: app.tunnel.id,
		AppID: app.id}
	msg, _ := newMessage(MsgP2P, MsgRelayHeartbeat, &req)
	msgWithHead := append(relayHead.Bytes(), msg...)
	for app.tunnel.isRuning() && app.running {
		app.tunnel.conn.WriteBytes(MsgP2P, MsgRelayData, msgWithHead)
		time.Sleep(TunnelHeartbeatTime)
	}
}
