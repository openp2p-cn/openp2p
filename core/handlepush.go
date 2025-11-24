package openp2p

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"runtime/pprof"
	"time"

	"github.com/openp2p-cn/totp"
)

func handlePush(subType uint16, msg []byte) error {
	pushHead := PushHeader{}
	err := binary.Read(bytes.NewReader(msg[openP2PHeaderSize:openP2PHeaderSize+PushHeaderSize]), binary.LittleEndian, &pushHead)
	if err != nil {
		return err
	}
	// gLog.d("handle push msg type:%d, push header:%+v", subType, pushHead)
	switch subType {
	case MsgPushConnectReq:
		err = handleConnectReq(msg)
	case MsgPushRsp:
		rsp := PushRsp{}
		if err = json.Unmarshal(msg[openP2PHeaderSize:], &rsp); err != nil {
			gLog.e("Unmarshal pushRsp:%s", err)
			return err
		}
		if rsp.Error == 0 {
			gLog.dev("push ok, detail:%s", rsp.Detail)
		} else {
			gLog.e("push error:%d, detail:%s", rsp.Error, rsp.Detail)
		}
	case MsgPushAddRelayTunnelReq:
		req := AddRelayTunnelReq{}
		if err = json.Unmarshal(msg[openP2PHeaderSize+PushHeaderSize:], &req); err != nil {
			gLog.e("Unmarshal %v:%s", reflect.TypeOf(req), err)
			return err
		}
		config := AppConfig{}
		config.PeerNode = req.RelayName
		config.peerToken = req.RelayToken
		config.relayMode = req.RelayMode
		config.PunchPriority = req.PunchPriority
		config.UnderlayProtocol = req.UnderlayProtocol
		go func(r AddRelayTunnelReq) {
			t, errDt := GNetwork.addDirectTunnel(config, 0)
			if errDt == nil && t != nil {
				// notify peer relay ready
				msg := TunnelMsg{ID: t.id}
				GNetwork.push(r.From, MsgPushAddRelayTunnelRsp, msg)
				appConfig := config
				appConfig.PeerNode = req.From
			} else {
				gLog.w("addDirectTunnel error:%s", errDt)
				GNetwork.push(r.From, MsgPushAddRelayTunnelRsp, "error") // compatible with old version client, trigger unmarshal error
			}
		}(req)
	case MsgPushServerSideSaveMemApp:
		req := ServerSideSaveMemApp{}
		if err = json.Unmarshal(msg[openP2PHeaderSize+PushHeaderSize:], &req); err != nil {
			gLog.e("Unmarshal %v:%s", reflect.TypeOf(req), err)
			return err
		}
		gLog.d("handle MsgPushServerSideSaveMemApp:%s", prettyJson(req))
		if req.RelayIndex > uint32(gConf.sdwan.TunnelNum-1) {
			return errors.New("wrong relay index")
		}
		var existTunnel *P2PTunnel
		i, ok := GNetwork.allTunnels.Load(req.TunnelID)
		if !ok {
			time.Sleep(time.Millisecond * 3000)
			i, ok = GNetwork.allTunnels.Load(req.TunnelID) // retry sometimes will receive MsgPushServerSideSaveMemApp but p2ptunnel not store yet.
			if !ok {
				gLog.e("handle MsgPushServerSideSaveMemApp error:%s", ErrMemAppTunnelNotFound)
				return ErrMemAppTunnelNotFound
			}
		}
		existTunnel = i.(*P2PTunnel)
		peerID := NodeNameToID(req.From)
		appIdx := peerID
		if req.SrcPort != 0 {
			appIdx = req.AppID
		}
		existApp, appok := GNetwork.apps.Load(appIdx)
		if appok {
			app := existApp.(*p2pApp)
			app.config.AppName = fmt.Sprintf("%d", peerID)
			app.id = req.AppID
			app.key = req.AppKey
			app.PreCalcKeyBytes()
			app.relayMode[req.RelayIndex] = req.RelayMode
			app.hbTime[req.RelayIndex] = time.Now()
			if req.RelayTunnelID == 0 {
				app.SetTunnel(existTunnel, 0)
			} else {
				app.SetTunnel(existTunnel, int(req.RelayIndex))              // TODO: merge two func
				app.SetRelayTunnelID(req.RelayTunnelID, int(req.RelayIndex)) // direct tunnel rtid=0, no need set rtid
			}
			gLog.d("found existing memapp, update it")
		} else {
			appConfig := existTunnel.config
			appConfig.SrcPort = int(req.SrcPort)
			appConfig.Protocol = ""
			appConfig.AppName = fmt.Sprintf("%d", peerID)
			appConfig.PeerNode = req.From
			app := p2pApp{
				id:      req.AppID,
				config:  appConfig,
				running: true,
				// asyncWriteChan: make(chan []byte, WriteDataChanSize),
				key: req.AppKey,
			}
			app.PreCalcKeyBytes()
			tunnelNum := 2
			if req.TunnelNum > uint32(tunnelNum) {
				tunnelNum = int(req.TunnelNum)
			}
			app.Init(tunnelNum)
			app.relayMode[req.RelayIndex] = req.RelayMode
			app.hbTime[req.RelayIndex] = time.Now()
			if req.RelayTunnelID == 0 {
				app.SetTunnel(existTunnel, 0)
			} else {
				app.SetTunnel(existTunnel, int(req.RelayIndex))
				app.SetRelayTunnelID(req.RelayTunnelID, int(req.RelayIndex))
			}
			if req.RelayTunnelID != 0 {
				app.relayNode[req.RelayIndex] = req.Node
			}
			app.Start(false)
			GNetwork.apps.Store(appIdx, &app)
			gLog.d("store memapp %d %d", appIdx, req.SrcPort)
		}

		return nil
	case MsgPushUpdate:
		gLog.i("MsgPushUpdate")
		err := update(gConf.Network.ServerHost, gConf.Network.ServerPort)
		if err == nil {
			os.Exit(9) // 9 tell daemon this exit because of update
		}
		return err
	case MsgPushRestart:
		gLog.i("MsgPushRestart")
		os.Exit(0)
		return err
	case MsgPushReportApps:
		err = handleReportApps()
	case MsgPushReportMemApps:
		err = handleReportMemApps()
	case MsgPushReportLog:
		err = handleLog(msg)
	case MsgPushReportGoroutine:
		err = handleReportGoroutine()
	case MsgPushReportHeap:
		err = handleReportHeap()
	case MsgPushCheckRemoteService:
		err = handleCheckRemoteService(msg)
	case MsgPushEditApp:
		err = handleEditApp(msg)
	case MsgPushEditNode:
		gLog.i("MsgPushEditNode")
		req := EditNode{}
		if err = json.Unmarshal(msg[openP2PHeaderSize:], &req); err != nil {
			gLog.e("Unmarshal %v:%s  %s", reflect.TypeOf(req), err, string(msg[openP2PHeaderSize:]))
			return err
		}
		gConf.setNode(req.NewName)
		gConf.setShareBandwidth(req.Bandwidth)
		gConf.Forcev6 = (req.Forcev6 != 0)
		gLog.i("set forcev6 to %v", gConf.Forcev6)
		gConf.save()
		os.Exit(0)
	case MsgPushSwitchApp:
		gLog.i("MsgPushSwitchApp")
		app := AppInfo{}
		if err = json.Unmarshal(msg[openP2PHeaderSize:], &app); err != nil {
			gLog.e("Unmarshal %v:%s  %s", reflect.TypeOf(app), err, string(msg[openP2PHeaderSize:]))
			return err
		}
		config := AppConfig{PeerNode: app.PeerNode, Enabled: app.Enabled, SrcPort: app.SrcPort, Protocol: app.Protocol}
		gLog.i("%s switch to %d", app.AppName, app.Enabled)
		gConf.switchApp(config, app.Enabled)
		if app.Enabled == 0 {
			// disable APP
			GNetwork.DeleteApp(config)
		}
	case MsgPushDstNodeOnline:
		req := PushDstNodeOnline{}
		if err = json.Unmarshal(msg[openP2PHeaderSize:], &req); err != nil {
			gLog.e("Unmarshal %v:%s  %s", reflect.TypeOf(req), err, string(msg[openP2PHeaderSize:]))
			return err
		}
		gLog.i("%s online, retryApp", req.Node)
		gConf.retryApp(req.Node)
	case MsgPushSpecTunnel:
		req := SpecTunnel{}
		if err = json.Unmarshal(msg[openP2PHeaderSize:], &req); err != nil {
			gLog.e("Unmarshal %v:%s  %s", reflect.TypeOf(req), err, string(msg[openP2PHeaderSize:]))
			return err
		}
		gLog.i("SpecTunnel %d", req.TunnelIndex)
		gConf.Network.specTunnel = int(req.TunnelIndex)
	case MsgPushSDWanRefresh:
		GNetwork.write(MsgSDWAN, MsgSDWANInfoReq, nil)
	default:
		i, ok := GNetwork.msgMap.Load(pushHead.From)
		if !ok {
			return ErrMsgChannelNotFound
		}
		ch := i.(chan msgCtx)
		ch <- msgCtx{data: msg, ts: time.Now()}
	}
	return err
}

func handleEditApp(msg []byte) (err error) {
	gLog.i("MsgPushEditApp")
	newApp := AppInfo{}
	if err = json.Unmarshal(msg[openP2PHeaderSize:], &newApp); err != nil {
		gLog.e("Unmarshal %v:%s  %s", reflect.TypeOf(newApp), err, string(msg[openP2PHeaderSize:]))
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
	if newApp.Protocol0 != "" && newApp.SrcPort0 != 0 { // not edit
		gConf.delete(oldConf)
	}

	if newApp.SrcPort != 0 { // delete app
		// AddApp
		newConf := oldConf
		newConf.Protocol = newApp.Protocol
		newConf.SrcPort = newApp.SrcPort
		newConf.RelayNode = newApp.SpecRelayNode
		newConf.PunchPriority = newApp.PunchPriority
		gConf.add(newConf, false)
	}

	if newApp.Protocol0 != "" && newApp.SrcPort0 != 0 { // not edit
		GNetwork.DeleteApp(oldConf) // DeleteApp may cost some times, execute at the end
	}
	return nil
}

func handleConnectReq(msg []byte) (err error) {
	req := PushConnectReq{}
	if err = json.Unmarshal(msg[openP2PHeaderSize+PushHeaderSize:], &req); err != nil {
		gLog.e("Unmarshal %v:%s", reflect.TypeOf(req), err)
		return err
	}
	gLog.d("%s is connecting... push connect response", req.From)
	if compareVersion(req.Version, LeastSupportVersion) < 0 {
		gLog.e("%s:%s", ErrVersionNotCompatible.Error(), req.From)
		rsp := PushConnectRsp{
			Error:  10,
			Detail: ErrVersionNotCompatible.Error(),
			To:     req.From,
			From:   gConf.Network.Node,
		}
		GNetwork.push(req.From, MsgPushConnectRsp, rsp)
		return ErrVersionNotCompatible
	}
	// verify totp token or token
	t := totp.TOTP{Step: totp.RelayTOTPStep}
	if t.Verify(req.Token, gConf.Network.Token, time.Now().Unix()-GNetwork.dt/int64(time.Second)) { // localTs may behind, auto adjust ts
		gLog.d("handleConnectReq Access Granted")
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
		config.UnderlayProtocol = req.UnderlayProtocol
		// share relay node will limit bandwidth
		if req.Token != gConf.Network.Token {
			gLog.i("set share bandwidth %d mbps", gConf.Network.ShareBandwidth)
			config.shareBandwidth = gConf.Network.ShareBandwidth
		}
		// go GNetwork.AddTunnel(config, req.ID)
		go func() {
			GNetwork.addDirectTunnel(config, req.ID)
		}()
		return nil
	}
	gLog.e("handleConnectReq Access Denied:%s", req.From)
	rsp := PushConnectRsp{
		Error:  1,
		Detail: fmt.Sprintf("connect to %s error: Access Denied", gConf.Network.Node),
		To:     req.From,
		From:   gConf.Network.Node,
	}
	return GNetwork.push(req.From, MsgPushConnectRsp, rsp)
}

func handleReportApps() (err error) {
	gLog.i("MsgPushReportApps")
	req := ReportApps{}
	gConf.mtx.RLock()
	defer gConf.mtx.RUnlock()

	for _, config := range gConf.Apps {
		appActive := 0
		relayNode := ""
		specRelayNode := ""
		relayMode := ""
		linkMode := LinkModeUDPPunch
		var connectTime string
		var retryTime string
		app := GNetwork.findApp(config)
		if app != nil {

			if app.IsActive() {
				appActive = 1
			}
			specRelayNode = app.config.RelayNode
			t, tidx := app.AvailableTunnel()
			if tidx != 0 { // TODO: should always report relay node for app edit
				relayNode = app.relayNode[tidx]
				relayMode = app.relayMode[tidx]
			}

			if t != nil {
				linkMode = t.linkModeWeb
			}
			retryTime = app.RetryTime().Local().Format("2006-01-02T15:04:05-0700")
			connectTime = app.ConnectTime().Local().Format("2006-01-02T15:04:05-0700")
		}
		appInfo := AppInfo{
			AppName:       config.AppName,
			Error:         config.errMsg,
			Protocol:      config.Protocol,
			PunchPriority: config.PunchPriority,
			Whitelist:     config.Whitelist,
			SrcPort:       config.SrcPort,
			RelayNode:     relayNode,
			SpecRelayNode: specRelayNode,
			RelayMode:     relayMode,
			LinkMode:      linkMode,
			PeerNode:      config.PeerNode,
			DstHost:       config.DstHost,
			DstPort:       config.DstPort,
			PeerUser:      config.PeerUser,
			PeerIP:        config.peerIP,
			PeerNatType:   config.peerNatType,
			RetryTime:     retryTime,
			ConnectTime:   connectTime,
			IsActive:      appActive,
			Enabled:       config.Enabled,
		}
		req.Apps = append(req.Apps, appInfo)
	}
	return GNetwork.write(MsgReport, MsgReportApps, &req)

}

func handleReportMemApps() (err error) {
	gLog.i("handleReportMemApps")
	req := ReportApps{}
	GNetwork.sdwan.sysRoute.Range(func(key, value interface{}) bool {
		node := value.(*sdwanNode)
		appActive := 0
		relayMode := ""
		var connectTime string
		var retryTime string

		i, ok := GNetwork.apps.Load(node.id)
		var app *p2pApp
		var t *P2PTunnel
		var tidx int
		if ok {
			app = i.(*p2pApp)
			t, tidx = app.AvailableTunnel()
			if app.IsActive() {
				appActive = 1
			}
			if tidx != 0 {
				relayMode = app.relayMode[tidx]
			}
			retryTime = app.RetryTime().Local().Format("2006-01-02T15:04:05-0700")
			connectTime = app.ConnectTime().Local().Format("2006-01-02T15:04:05-0700")
		}
		appInfo := AppInfo{
			RelayMode: relayMode,
			PeerNode:  node.name,
			IsActive:  appActive,
			Enabled:   1,
		}
		if app != nil {
			appInfo.AppName = app.config.AppName
			appInfo.Error = app.config.errMsg
			appInfo.Protocol = app.config.Protocol
			appInfo.Whitelist = app.config.Whitelist
			appInfo.SrcPort = app.config.SrcPort

			if tidx != 0 {
				appInfo.RelayNode = app.relayNode[tidx]
			}

			if t != nil {
				appInfo.LinkMode = t.linkModeWeb
			}
			appInfo.DstHost = app.config.DstHost
			appInfo.DstPort = app.config.DstPort
			appInfo.PeerUser = app.config.PeerUser
			appInfo.PeerIP = app.config.peerIP
			appInfo.PeerNatType = app.config.peerNatType
			appInfo.RetryTime = retryTime
			appInfo.ConnectTime = connectTime
		}
		req.Apps = append(req.Apps, appInfo)
		return true
	})
	req.TunError = GNetwork.sdwan.tunErr
	gLog.d("handleReportMemApps res:%s", prettyJson(req))
	gConf.retryAllMemApp()
	return GNetwork.write(MsgReport, MsgReportMemApps, &req)
}

func handleLog(msg []byte) (err error) {
	gLog.d("MsgPushReportLog")
	const defaultLen = 1024 * 128
	const maxLen = 1024 * 1024
	req := ReportLogReq{}
	if err = json.Unmarshal(msg[openP2PHeaderSize:], &req); err != nil {
		gLog.e("Unmarshal %v:%s  %s", reflect.TypeOf(req), err, string(msg[openP2PHeaderSize:]))
		return err
	}
	if req.FileName == "" {
		req.FileName = "openp2p.log"
	} else {
		req.FileName = sanitizeFileName(req.FileName)
	}
	if req.IsSetLogLevel == 1 {
		gLog.setLevel(LogLevel(req.LogLevel))
	}
	f, err := os.Open(filepath.Join("log", req.FileName))
	if err != nil {
		gLog.e("read log file error:%s", err)
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
		gLog.e("read log content error:%s", err)
		return err
	}
	rsp := ReportLogRsp{}
	rsp.Content = string(buff[:readLength])
	rsp.FileName = req.FileName
	rsp.Total = fi.Size()
	rsp.Len = req.Len
	return GNetwork.write(MsgReport, MsgPushReportLog, &rsp)
}

func handleReportGoroutine() (err error) {
	gLog.d("handleReportGoroutine")
	buf := make([]byte, 1024*128)
	stackLen := runtime.Stack(buf, true)
	return GNetwork.write(MsgReport, MsgReportResponse, string(buf[:stackLen]))
}

func handleReportHeap() error {
	var buf bytes.Buffer
	err := pprof.Lookup("heap").WriteTo(&buf, 1)
	if err != nil {
		return err
	}

	return GNetwork.write(MsgReport, MsgReportResponse, buf.String())
}

func handleCheckRemoteService(msg []byte) (err error) {
	gLog.d("handleCheckRemoteService")
	req := CheckRemoteService{}
	if err = json.Unmarshal(msg[openP2PHeaderSize:], &req); err != nil {
		gLog.e("Unmarshal %v:%s  %s", reflect.TypeOf(req), err, string(msg[openP2PHeaderSize:]))
		return err
	}
	rsp := PushRsp{Error: 0}
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", req.Host, req.Port), time.Second*3)
	if err != nil {
		rsp.Error = 1
		rsp.Detail = ErrRemoteServiceUnable.Error()
	} else {
		conn.Close()
	}
	return GNetwork.write(MsgReport, MsgReportResponse, rsp)
}
