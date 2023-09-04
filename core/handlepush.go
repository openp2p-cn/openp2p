package openp2p

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/openp2p-cn/totp"
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
		err = handleConnectReq(pn, subType, msg)
	case MsgPushRsp:
		rsp := PushRsp{}
		if err = json.Unmarshal(msg[openP2PHeaderSize:], &rsp); err != nil {
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
		if err = json.Unmarshal(msg[openP2PHeaderSize+PushHeaderSize:], &req); err != nil {
			gLog.Printf(LvERROR, "wrong %v:%s", reflect.TypeOf(req), err)
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
			} else {
				pn.push(r.From, MsgPushAddRelayTunnelRsp, "error") // compatible with old version client, trigger unmarshal error
			}
		}(req)
	case MsgPushAPPKey:
		req := APPKeySync{}
		if err = json.Unmarshal(msg[openP2PHeaderSize+PushHeaderSize:], &req); err != nil {
			gLog.Printf(LvERROR, "wrong %v:%s", reflect.TypeOf(req), err)
			return err
		}
		SaveKey(req.AppID, req.AppKey)
	case MsgPushUpdate:
		gLog.Println(LvINFO, "MsgPushUpdate")
		err := update(pn.config.ServerHost, pn.config.ServerPort)
		if err == nil {
			os.Exit(0)
		}
		return err
	case MsgPushRestart:
		gLog.Println(LvINFO, "MsgPushRestart")
		os.Exit(0)
		return err
	case MsgPushReportApps:
		err = handleReportApps(pn, subType, msg)
	case MsgPushReportLog:
		err = handleLog(pn, subType, msg)
	case MsgPushEditApp:
		err = handleEditApp(pn, subType, msg)
	case MsgPushEditNode:
		gLog.Println(LvINFO, "MsgPushEditNode")
		req := EditNode{}
		if err = json.Unmarshal(msg[openP2PHeaderSize:], &req); err != nil {
			gLog.Printf(LvERROR, "wrong %v:%s  %s", reflect.TypeOf(req), err, string(msg[openP2PHeaderSize:]))
			return err
		}
		gConf.setNode(req.NewName)
		gConf.setShareBandwidth(req.Bandwidth)
		// TODO: hot reload
		os.Exit(0)
	case MsgPushSwitchApp:
		gLog.Println(LvINFO, "MsgPushSwitchApp")
		app := AppInfo{}
		if err = json.Unmarshal(msg[openP2PHeaderSize:], &app); err != nil {
			gLog.Printf(LvERROR, "wrong %v:%s  %s", reflect.TypeOf(app), err, string(msg[openP2PHeaderSize:]))
			return err
		}
		config := AppConfig{Enabled: app.Enabled, SrcPort: app.SrcPort, Protocol: app.Protocol}
		gLog.Println(LvINFO, app.AppName, " switch to ", app.Enabled)
		gConf.switchApp(config, app.Enabled)
		if app.Enabled == 0 {
			// disable APP
			pn.DeleteApp(config)
		}
	case MsgPushDstNodeOnline:
		gLog.Println(LvINFO, "MsgPushDstNodeOnline")
		req := PushDstNodeOnline{}
		if err = json.Unmarshal(msg[openP2PHeaderSize:], &req); err != nil {
			gLog.Printf(LvERROR, "wrong %v:%s  %s", reflect.TypeOf(req), err, string(msg[openP2PHeaderSize:]))
			return err
		}
		gLog.Println(LvINFO, "retry peerNode ", req.Node)
		gConf.retryApp(req.Node)
	default:
		pn.msgMapMtx.Lock()
		ch := pn.msgMap[pushHead.From]
		pn.msgMapMtx.Unlock()
		ch <- pushMsg{data: msg, ts: time.Now()}
	}
	return err
}

func handleEditApp(pn *P2PNetwork, subType uint16, msg []byte) (err error) {
	gLog.Println(LvINFO, "MsgPushEditApp")
	newApp := AppInfo{}
	if err = json.Unmarshal(msg[openP2PHeaderSize:], &newApp); err != nil {
		gLog.Printf(LvERROR, "wrong %v:%s  %s", reflect.TypeOf(newApp), err, string(msg[openP2PHeaderSize:]))
		return err
	}
	oldConf := AppConfig{Enabled: 1}
	// protocol0+srcPort0 exist, delApp
	oldConf.AppName = newApp.AppName
	oldConf.Protocol = newApp.Protocol0
	oldConf.Whitelist = newApp.Whitelist
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
	pn.DeleteApp(oldConf) // DeleteApp may cost some times, execute at the end
	return nil
	// autoReconnect will auto AddApp
	// pn.AddApp(config)
	// TODO: report result
}

func handleConnectReq(pn *P2PNetwork, subType uint16, msg []byte) (err error) {
	req := PushConnectReq{}
	if err = json.Unmarshal(msg[openP2PHeaderSize+PushHeaderSize:], &req); err != nil {
		gLog.Printf(LvERROR, "wrong %v:%s", reflect.TypeOf(req), err)
		return err
	}
	gLog.Printf(LvDEBUG, "%s is connecting...", req.From)
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
	t := totp.TOTP{Step: totp.RelayTOTPStep}
	if t.Verify(req.Token, pn.config.Token, time.Now().Unix()-pn.dt/int64(time.Second)) { // localTs may behind, auto adjust ts
		gLog.Printf(LvINFO, "Access Granted\n")
		config := AppConfig{}
		config.peerNatType = req.NatType
		config.peerConeNatPort = req.ConeNatPort
		config.peerIP = req.FromIP
		config.PeerNode = req.From
		config.peerVersion = req.Version
		config.fromToken = req.Token
		config.peerIPv6 = req.IPv6
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
		return nil
	}
	gLog.Println(LvERROR, "Access Denied:", req.From)
	rsp := PushConnectRsp{
		Error:  1,
		Detail: fmt.Sprintf("connect to %s error: Access Denied", pn.config.Node),
		To:     req.From,
		From:   pn.config.Node,
	}
	return pn.push(req.From, MsgPushConnectRsp, rsp)
}

func handleReportApps(pn *P2PNetwork, subType uint16, msg []byte) (err error) {
	gLog.Println(LvINFO, "MsgPushReportApps")
	req := ReportApps{}
	gConf.mtx.Lock()
	defer gConf.mtx.Unlock()
	for _, config := range gConf.Apps {
		appActive := 0
		relayNode := ""
		relayMode := ""
		linkMode := LinkModeUDPPunch
		i, ok := pn.apps.Load(config.ID())
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
			Whitelist:   config.Whitelist,
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
	return pn.write(MsgReport, MsgReportApps, &req)
}

func handleLog(pn *P2PNetwork, subType uint16, msg []byte) (err error) {
	gLog.Println(LvDEBUG, "MsgPushReportLog")
	const defaultLen = 1024 * 128
	const maxLen = 1024 * 1024
	req := ReportLogReq{}
	if err = json.Unmarshal(msg[openP2PHeaderSize:], &req); err != nil {
		gLog.Printf(LvERROR, "wrong %v:%s  %s", reflect.TypeOf(req), err, string(msg[openP2PHeaderSize:]))
		return err
	}
	if req.FileName == "" {
		req.FileName = "openp2p.log"
	}
	f, err := os.Open(filepath.Join("log", req.FileName))
	if err != nil {
		gLog.Println(LvERROR, "read log file error:", err)
		return err
	}
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	if req.Offset > fi.Size() {
		req.Offset = fi.Size() - defaultLen
	}
	// verify input parameters
	if req.Offset < 0 {
		req.Offset = 0
	}
	if req.Len <= 0 || req.Len > maxLen {
		req.Len = defaultLen
	}

	f.Seek(req.Offset, 0)
	buff := make([]byte, req.Len)
	readLength, err := f.Read(buff)
	f.Close()
	if err != nil {
		gLog.Println(LvERROR, "read log content error:", err)
		return err
	}
	rsp := ReportLogRsp{}
	rsp.Content = string(buff[:readLength])
	rsp.FileName = req.FileName
	rsp.Total = fi.Size()
	rsp.Len = req.Len
	return pn.write(MsgReport, MsgPushReportLog, &rsp)
}
