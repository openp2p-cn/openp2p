package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
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
		// verify token or name&password
		if VerifyTOTP(req.Token, pn.config.User, pn.config.Password, time.Now().Unix()+(pn.serverTs-pn.localTs)) || // localTs may behind, auto adjust ts
			VerifyTOTP(req.Token, pn.config.User, pn.config.Password, time.Now().Unix()) ||
			(req.User == pn.config.User && req.Password == pn.config.Password) {
			gLog.Printf(LevelINFO, "Access Granted\n")
			config := AppConfig{}
			config.peerNatType = req.NatType
			config.peerConeNatPort = req.ConeNatPort
			config.peerIP = req.FromIP
			config.PeerNode = req.From
			// share relay node will limit bandwidth
			if req.User != pn.config.User || req.Password != pn.config.Password {
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
		// set user password, maybe the relay node is your private node
		config.PeerUser = pn.config.User
		config.PeerPassword = pn.config.Password
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
		targetPath := filepath.Join(defaultInstallPath, defaultBinName)
		args := []string{"update"}
		env := os.Environ()
		// Windows does not support exec syscall.
		if runtime.GOOS == "windows" {
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
		}
		return syscall.Exec(targetPath, args, env)
	case MsgPushReportApps:
		gLog.Println(LevelINFO, "MsgPushReportApps")
		req := ReportApps{}
		// TODO: add the retrying apps
		gConf.mtx.Lock()
		defer gConf.mtx.Unlock()
		for _, config := range gConf.Apps {
			appInfo := AppInfo{
				AppName:  config.AppName,
				Protocol: config.Protocol,
				SrcPort:  config.SrcPort,
				// RelayNode:   relayNode,
				PeerNode:    config.PeerNode,
				DstHost:     config.DstHost,
				DstPort:     config.DstPort,
				PeerUser:    config.PeerUser,
				PeerIP:      config.peerIP,
				PeerNatType: config.peerNatType,
				RetryTime:   config.retryTime.String(),
				IsActive:    1,
			}
			req.Apps = append(req.Apps, appInfo)
		}
		// pn.apps.Range(func(_, i interface{}) bool {
		// 	app := i.(*p2pApp)
		// 	appInfo := AppInfo{
		// 		AppName:     app.config.AppName,
		// 		Protocol:    app.config.Protocol,
		// 		SrcPort:     app.config.SrcPort,
		// 		RelayNode:   app.relayNode,
		// 		PeerNode:    app.config.PeerNode,
		// 		DstHost:     app.config.DstHost,
		// 		DstPort:     app.config.DstPort,
		// 		PeerUser:    app.config.PeerUser,
		// 		PeerIP:      app.config.peerIP,
		// 		PeerNatType: app.config.peerNatType,
		// 		RetryTime:   app.config.retryTime.String(),
		// 		IsActive:    1,
		// 	}
		// 	req.Apps = append(req.Apps, appInfo)
		// 	return true
		// })
		pn.write(MsgReport, MsgReportApps, &req)
	case MsgPushEditApp:
		gLog.Println(LevelINFO, "MsgPushEditApp")
		newApp := AppInfo{}
		err := json.Unmarshal(msg[openP2PHeaderSize:], &newApp)
		if err != nil {
			gLog.Printf(LevelERROR, "wrong MsgPushEditApp:%s  %s", err, string(msg[openP2PHeaderSize:]))
			return err
		}
		var oldConf AppConfig
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
		gConf.add(newConf)
		gConf.save()
		pn.DeleteApp(oldConf) // save quickly for the next request reportApplist
		// autoReconnect will auto AddApp
		// pn.AddApp(config)
		// TODO: report result
	default:
		pn.msgMapMtx.Lock()
		ch := pn.msgMap[pushHead.From]
		pn.msgMapMtx.Unlock()
		ch <- msg
	}
	return nil
}
