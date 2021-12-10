package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/rand"
	"net"
	"sync"
	"time"
)

type p2pApp struct {
	config   AppConfig
	listener net.Listener
	tunnel   *P2PTunnel
	rtid     uint64
	hbTime   time.Time
	hbMtx    sync.Mutex
	running  bool
	id       uint64
	key      uint64
	wg       sync.WaitGroup
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
	var err error
	app.listener, err = net.Listen("tcp4", fmt.Sprintf("0.0.0.0:%d", app.config.SrcPort))
	if err != nil {
		gLog.Printf(LevelERROR, "listen error:%s", err)
		return err
	}
	for {
		conn, err := app.listener.Accept()
		if err != nil {
			gLog.Printf(LevelERROR, "%d accept error:%s", app.tunnel.id, err)
			break
		}
		otcp := overlayTCP{
			tunnel:   app.tunnel,
			conn:     conn,
			id:       rand.Uint64(),
			isClient: true,
			rtid:     app.rtid,
			appID:    app.id,
			appKey:   app.key,
		}
		// calc key bytes for encrypt
		if otcp.appKey != 0 {
			encryptKey := make([]byte, AESKeySize)
			binary.LittleEndian.PutUint64(encryptKey, otcp.appKey)
			binary.LittleEndian.PutUint64(encryptKey[8:], otcp.appKey)
			otcp.appKeyBytes = encryptKey
		}
		app.tunnel.overlayConns.Store(otcp.id, &otcp)
		gLog.Printf(LevelINFO, "Accept overlayID:%d", otcp.id)
		// tell peer connect
		req := OverlayConnectReq{ID: otcp.id,
			User:     app.config.PeerUser,
			Password: app.config.PeerPassword,
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

		go otcp.run()
	}
	return nil
}

func (app *p2pApp) listen() error {
	gLog.Printf(LevelINFO, "LISTEN ON PORT %d START", app.config.SrcPort)
	defer gLog.Printf(LevelINFO, "LISTEN ON PORT %d START", app.config.SrcPort)
	app.wg.Add(1)
	defer app.wg.Done()
	app.running = true
	if app.rtid != 0 {
		go app.relayHeartbeatLoop()
	}
	for app.running {
		if app.config.Protocol == "tcp" {
			app.listenTCP()
		}
		time.Sleep(time.Second * 5)
		// TODO: listen UDP
	}
	return nil
}

func (app *p2pApp) close() {
	app.running = false
	if app.listener != nil {
		app.listener.Close()
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
	gLog.Printf(LevelDEBUG, "relayHeartbeat to %d start", app.rtid)
	defer gLog.Printf(LevelDEBUG, "relayHeartbeat to %d end", app.rtid)
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
