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
	gLog.Printf(LvDEBUG, "handle push msg type:%d, push header:%+v", subType, pushHead)
	switch subType {
	case MsgPushConnectReq: // TODO: handle a msg move to a new function
		req := PushConnectReq{}
		err := json.Unmarshal(msg[openP2PHeaderSize+PushHeaderSize:], &req)
		if err != nil {
			gLog.Printf(LvERROR, "wrong MsgPushConnectReq:%s", err)
			return err
		}
		gLog.Printf(LvINFO, "%s is connecting...", req.From)
		gLog.Println(LvDEBUG, "push connect response to ", req.From)
		if compareVersion(req.Version, LeastSupportVersion) == LESS {
			gLog.Println(LvERROR, ErrVersionNotCompatible.Error(), ":", req.From)
			rsp := PushConnectRsp{
				Error:  10,
				Detail: ErrVersionNotCompatible.Error(),
				To:     req.From,
				From:   pn.config.Node,
			}
			pn.push(req.From, MsgPushConnectRsp, rsp)
			return ErrVersionNotCompatible
		}
		// verify totp token or token
		if VerifyTOTP(req.Token, pn.config.Token, time.Now().Unix()+(pn.serverTs-pn.localTs)) || // localTs may behind, auto adjust ts
			VerifyTOTP(req.Token, pn.config.Token, time.Now().Unix()) {
			gLog.Printf(LvINFO, "Access Granted\n")
			config := AppConfig{}
			config.peerNatType = req.NatType
			config.peerConeNatPort = req.ConeNatPort
			config.peerIP = req.FromIP
			config.PeerNode = req.From
			config.peerVersion = req.Version
			config.fromToken = req.Token
			config.IPv6 = req.IPv6
			config.hasIPv4 = req.HasIPv4
			config.hasUPNPorNATPMP = req.HasUPNPorNATPMP
			config.linkMode = req.LinkMode
			config.isUnderlayServer = req.IsUnderlayServer
			// share relay node will limit bandwidth
			if req.Token != pn.config.Token {
				gLog.Printf(LvINFO, "set share bandwidth %d mbps", pn.config.ShareBandwidth)
				config.shareBandwidth = pn.config.ShareBandwidth
			}
			// go pn.AddTunnel(config, req.ID)
			go pn.addDirectTunnel(config, req.ID)
			break
		}
		gLog.Println(LvERROR, "Access Denied:", req.From)
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
			gLog.Printf(LvERROR, "wrong pushRsp:%s", err)
			return err
		}
		if rsp.Error == 0 {
			gLog.Printf(LvDEBUG, "push ok, detail:%s", rsp.Detail)
		} else {
			gLog.Printf(LvERROR, "push error:%d, detail:%s", rsp.Error, rsp.Detail)
		}
	case MsgPushAddRelayTunnelReq:
		req := AddRelayTunnelReq{}
		err := json.Unmarshal(msg[openP2PHeaderSize+PushHeaderSize:], &req)
		if err != nil {
			gLog.Printf(LvERROR, "wrong RelayNodeRsp:%s", err)
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
			}

		}(req)
	case MsgPushAPPKey:
		req := APPKeySync{}
		err := json.Unmarshal(msg[openP2PHeaderSize+PushHeaderSize:], &req)
		if err != nil {
			gLog.Printf(LvERROR, "wrong APPKeySync:%s", err)
			return err
		}
		SaveKey(req.AppID, req.AppKey)
	case MsgPushUpdate:
		gLog.Println(LvINFO, "MsgPushUpdate")
		update(pn.config.ServerHost, pn.config.ServerPort) // download new version first, then exec ./openp2p update
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
		gLog.Println(LvINFO, "MsgPushRestart")
		os.Exit(0)
		return err
	case MsgPushReportApps:
		gLog.Println(LvINFO, "MsgPushReportApps")
		req := ReportApps{}
		gConf.mtx.Lock()
		defer gConf.mtx.Unlock()
		for _, config := range gConf.Apps {
			appActive := 0
			relayNode := ""
			relayMode := ""
			linkMode := LinkModeUDPPunch
			i, ok := pn.apps.Load(fmt.Sprintf("%s%d", config.Protocol, config.SrcPort))
			if ok {
				app := i.(*p2pApp)
				if app.isActive() {
					appActive = 1
				}
				relayNode = app.relayNode
				relayMode = app.relayMode
				linkMode = app.tunnel.linkModeWeb
			}
			appInfo := AppInfo{
				AppName:     config.AppName,
				Error:       config.errMsg,
				Protocol:    config.Protocol,
				SrcPort:     config.SrcPort,
				RelayNode:   relayNode,
				RelayMode:   relayMode,
				LinkMode:    linkMode,
				PeerNode:    config.PeerNode,
				DstHost:     config.DstHost,
				DstPort:     config.DstPort,
				PeerUser:    config.PeerUser,
				PeerIP:      config.peerIP,
				PeerNatType: config.peerNatType,
				RetryTime:   config.retryTime.Local().Format("2006-01-02T15:04:05-0700"),
				ConnectTime: config.connectTime.Local().Format("2006-01-02T15:04:05-0700"),
				IsActive:    appActive,
				Enabled:     config.Enabled,
			}
			req.Apps = append(req.Apps, appInfo)
		}
		pn.write(MsgReport, MsgReportApps, &req)
	case MsgPushReportLog:
		gLog.Println(LvINFO, "MsgPushReportLog")
		req := ReportLogReq{}
		err := json.Unmarshal(msg[openP2PHeaderSize:], &req)
		if err != nil {
			gLog.Printf(LvERROR, "wrong MsgPushReportLog:%s  %s", err, string(msg[openP2PHeaderSize:]))
			return err
		}
		if req.FileName == "" {
			req.FileName = "openp2p.log"
		}
		f, err := os.Open(filepath.Join("log", req.FileName))
		if err != nil {
			gLog.Println(LvERROR, "read log file error:", err)
			break
		}
		fi, err := f.Stat()
		if err != nil {
			break
		}
		if req.Offset == 0 && fi.Size() > 4096 {
			req.Offset = fi.Size() - 4096
		}
		if req.Len <= 0 {
			req.Len = 4096
		}
		f.Seek(req.Offset, 0)
		if req.Len > 1024*1024 { // too large
			break
		}
		buff := make([]byte, req.Len)
		readLength, err := f.Read(buff)
		f.Close()
		if err != nil {
			gLog.Println(LvERROR, "read log content error:", err)
			break
		}
		rsp := ReportLogRsp{}
		rsp.Content = string(buff[:readLength])
		rsp.FileName = req.FileName
		rsp.Total = fi.Size()
		rsp.Len = req.Len
		pn.write(MsgReport, MsgPushReportLog, &rsp)
	case MsgPushEditApp:
		gLog.Println(LvINFO, "MsgPushEditApp")
		newApp := AppInfo{}
		err := json.Unmarshal(msg[openP2PHeaderSize:], &newApp)
		if err != nil {
			gLog.Printf(LvERROR, "wrong MsgPushEditApp:%s  %s", err, string(msg[openP2PHeaderSize:]))
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
		gLog.Println(LvINFO, "MsgPushEditNode")
		req := EditNode{}
		err := json.Unmarshal(msg[openP2PHeaderSize:], &req)
		if err != nil {
			gLog.Printf(LvERROR, "wrong MsgPushEditNode:%s  %s", err, string(msg[openP2PHeaderSize:]))
			return err
		}
		gConf.setNode(req.NewName)
		gConf.setShareBandwidth(req.Bandwidth)
		gConf.save()
		// TODO: hot reload
		os.Exit(0)
	case MsgPushSwitchApp:
		gLog.Println(LvINFO, "MsgPushSwitchApp")
		app := AppInfo{}
		err := json.Unmarshal(msg[openP2PHeaderSize:], &app)
		if err != nil {
			gLog.Printf(LvERROR, "wrong MsgPushSwitchApp:%s  %s", err, string(msg[openP2PHeaderSize:]))
			return err
		}
		config := AppConfig{Enabled: app.Enabled, SrcPort: app.SrcPort, Protocol: app.Protocol}
		gLog.Println(LvINFO, app.AppName, " switch to ", app.Enabled)
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
