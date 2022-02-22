package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

func handlePush(pn *P2PNetwork, subType uint16, msg []byte) error {
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
		// verify totp token or token
		if VerifyTOTP(req.Token, pn.config.Token, time.Now().Unix()+(pn.serverTs-pn.localTs)) || // localTs may behind, auto adjust ts
			VerifyTOTP(req.Token, pn.config.Token, time.Now().Unix()) ||
			(req.FromToken == pn.config.Token) {
			gLog.Printf(LevelINFO, "Access Granted\n")
			config := AppConfig{}
			config.peerNatType = req.NatType
			config.peerConeNatPort = req.ConeNatPort
			config.peerIP = req.FromIP
			config.PeerNode = req.From
			// share relay node will limit bandwidth
			if req.FromToken != pn.config.Token {
				gLog.Printf(LevelINFO, "set share bandwidth %d mbps", pn.config.ShareBandwidth)
				config.shareBandwidth = pn.config.ShareBandwidth
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
		gLog.Println(LevelINFO, "MsgPushUpdate")
		update() // download new version first, then exec ./openp2p update
		targetPath := filepath.Join(defaultInstallPath, defaultBinName)
		args := []string{"update"}
		env := os.Environ()
		cmd := exec.Command(targetPath, args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		cmd.Env = env
		err := cmd.Run()
		if err == nil {
			os.Exit(0)
		}
		return err
	case MsgPushRestart:
		gLog.Println(LevelINFO, "MsgPushRestart")
		os.Exit(0)
		return err
	case MsgPushReportApps:
		gLog.Println(LevelINFO, "MsgPushReportApps")
		req := ReportApps{}
		// TODO: add the retrying apps
		gConf.mtx.Lock()
		defer gConf.mtx.Unlock()
		for _, config := range gConf.Apps {
			appActive := 0
			relayNode := ""
			relayMode := ""
			i, ok := pn.apps.Load(fmt.Sprintf("%s%d", config.Protocol, config.SrcPort))
			if ok {
				app := i.(*p2pApp)
				if app.isActive() {
					appActive = 1
				}
				relayNode = app.relayNode
				relayMode = app.relayMode
			}
			appInfo := AppInfo{
				AppName:     config.AppName,
				Protocol:    config.Protocol,
				SrcPort:     config.SrcPort,
				RelayNode:   relayNode,
				RelayMode:   relayMode,
				PeerNode:    config.PeerNode,
				DstHost:     config.DstHost,
				DstPort:     config.DstPort,
				PeerUser:    config.PeerUser,
				PeerIP:      config.peerIP,
				PeerNatType: config.peerNatType,
				RetryTime:   config.retryTime.String(),
				IsActive:    appActive,
				Enabled:     config.Enabled,
			}
			req.Apps = append(req.Apps, appInfo)
		}
		pn.write(MsgReport, MsgReportApps, &req)
	case MsgPushEditApp:
		gLog.Println(LevelINFO, "MsgPushEditApp")
		newApp := AppInfo{}
		err := json.Unmarshal(msg[openP2PHeaderSize:], &newApp)
		if err != nil {
			gLog.Printf(LevelERROR, "wrong MsgPushEditApp:%s  %s", err, string(msg[openP2PHeaderSize:]))
			return err
		}
		oldConf := AppConfig{Enabled: 1}
		// protocol0+srcPort0 exist, delApp
		oldConf.AppName = newApp.AppName
		oldConf.Protocol = newApp.Protocol0
		oldConf.SrcPort = newApp.SrcPort0
		oldConf.PeerNode = newApp.PeerNode
		oldConf.DstHost = newApp.DstHost
		oldConf.DstPort = newApp.DstPort

		gConf.delete(oldConf)
		// AddApp
		newConf := oldConf
		newConf.Protocol = newApp.Protocol
		newConf.SrcPort = newApp.SrcPort
		gConf.add(newConf, false)
		gConf.save()          // save quickly for the next request reportApplist
		pn.DeleteApp(oldConf) // DeleteApp may cost some times, execute at the end
		// autoReconnect will auto AddApp
		// pn.AddApp(config)
		// TODO: report result
	case MsgPushEditNode:
		gLog.Println(LevelINFO, "MsgPushEditNode")
		req := EditNode{}
		err := json.Unmarshal(msg[openP2PHeaderSize:], &req)
		if err != nil {
			gLog.Printf(LevelERROR, "wrong MsgPushEditNode:%s  %s", err, string(msg[openP2PHeaderSize:]))
			return err
		}
		gConf.mtx.Lock()
		gConf.Network.Node = req.NewName
		gConf.Network.ShareBandwidth = req.Bandwidth
		gConf.mtx.Unlock()
		gConf.save()
		// TODO: hot reload
		os.Exit(0)
	case MsgPushSwitchApp:
		gLog.Println(LevelINFO, "MsgPushSwitchApp")
		app := AppInfo{}
		err := json.Unmarshal(msg[openP2PHeaderSize:], &app)
		if err != nil {
			gLog.Printf(LevelERROR, "wrong MsgPushSwitchApp:%s  %s", err, string(msg[openP2PHeaderSize:]))
			return err
		}
		config := AppConfig{Enabled: app.Enabled, SrcPort: app.SrcPort, Protocol: app.Protocol}
		gLog.Println(LevelINFO, app.AppName, " switch to ", app.Enabled)
		gConf.switchApp(config, app.Enabled)
		if app.Enabled == 0 {
			// disable APP
			pn.DeleteApp(config)
		}
	default:
		pn.msgMapMtx.Lock()
		ch := pn.msgMap[pushHead.From]
		pn.msgMapMtx.Unlock()
		ch <- msg
	}
	return nil
}
