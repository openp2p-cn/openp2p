package main

import (
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"time"
)

func main() {
	rand.Seed(time.Now().UnixNano())
	binDir := filepath.Dir(os.Args[0])
	os.Chdir(binDir) // for system service
	gLog = InitLogger(binDir, "openp2p", LevelDEBUG, 1024*1024, LogFileAndConsole)

	// TODO: install sub command, deamon process
	// groups := flag.String("groups", "", "you could join in several groups. like: GroupName1:Password1;GroupName2:Password2; group name 8-31 characters")
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version", "-v", "--version":
			fmt.Println(OpenP2PVersion)
			return
		case "update":
			gLog = InitLogger(filepath.Dir(os.Args[0]), "openp2p", LevelDEBUG, 1024*1024, LogFileAndConsole)
			update()
			targetPath := filepath.Join(defaultInstallPath, defaultBinName)
			d := daemon{}
			err := d.Control("restart", targetPath, nil)
			if err != nil {
				gLog.Println(LevelERROR, "restart service error:", err)
			} else {
				gLog.Println(LevelINFO, "restart service ok.")
			}
			return
		case "install":
			install()
			return
		case "uninstall":
			uninstall()
			return
		}
	}
	serverHost := flag.String("serverhost", "api.openp2p.cn", "server host ")
	// serverHost := flag.String("serverhost", "127.0.0.1", "server host ") // for debug
	user := flag.String("user", "", "user name. 8-31 characters")
	node := flag.String("node", "", "node name. 8-31 characters")
	password := flag.String("password", "", "user password. 8-31 characters")
	peerNode := flag.String("peernode", "", "peer node name that you want to connect")
	peerUser := flag.String("peeruser", "", "peer node user (default peeruser=user)")
	peerPassword := flag.String("peerpassword", "", "peer node password (default peerpassword=password)")
	dstIP := flag.String("dstip", "127.0.0.1", "destination ip ")
	dstPort := flag.Int("dstport", 0, "destination port ")
	srcPort := flag.Int("srcport", 0, "source port ")
	protocol := flag.String("protocol", "tcp", "tcp or udp")
	appName := flag.String("appname", "", "app name")
	flag.Bool("noshare", false, "deprecated. uses -sharebandwidth -1") // Deprecated, rm later
	shareBandwidth := flag.Int("sharebandwidth", 10, "N mbps share bandwidth limit, private node no limit")
	flag.Bool("f", false, "deprecated. config file") // Deprecated, rm later
	daemonMode := flag.Bool("d", false, "daemonMode")
	flag.Bool("bydaemon", false, "start by daemon") // Deprecated, rm later
	logLevel := flag.Int("loglevel", 1, "0:debug 1:info 2:warn 3:error")
	flag.Parse()

	config := AppConfig{}
	config.PeerNode = *peerNode
	config.PeerUser = *peerUser
	config.PeerPassword = *peerPassword
	config.DstHost = *dstIP
	config.DstPort = *dstPort
	config.SrcPort = *srcPort
	config.Protocol = *protocol
	config.AppName = *appName
	// add command config first
	gConf.add(config)
	gConf.load()
	gConf.mtx.Lock()

	// spec paramters in commandline will always be used
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "sharebandwidth" {
			gConf.Network.ShareBandwidth = *shareBandwidth
		}
		if f.Name == "node" {
			gConf.Network.Node = *node
		}
		if f.Name == "user" {
			gConf.Network.User = *user
		}
		if f.Name == "password" {
			gConf.Network.Password = *password
		}
		if f.Name == "serverhost" {
			gConf.Network.ServerHost = *serverHost
		}
		if f.Name == "loglevel" {
			gConf.LogLevel = *logLevel
		}
	})

	if gConf.Network.ServerHost == "" {
		gConf.Network.ServerHost = *serverHost
	}
	if gConf.Network.Node == "" {
		gConf.Network.Node = *node
	}
	if gConf.Network.User == "" {
		gConf.Network.User = *user
	}
	if gConf.Network.Password == "" {
		gConf.Network.Password = *password
	}
	if gConf.LogLevel == IntValueNotSet {
		gConf.LogLevel = *logLevel
	}
	if gConf.Network.ShareBandwidth == IntValueNotSet {
		gConf.Network.ShareBandwidth = *shareBandwidth
	}

	gConf.Network.ServerPort = 27182
	gConf.Network.UDPPort1 = 27182
	gConf.Network.UDPPort2 = 27183
	gLog.Println(LevelINFO, "openp2p start. version: ", OpenP2PVersion)
	gLog.setLevel(LogLevel(gConf.LogLevel))
	gConf.mtx.Unlock()
	gConf.save()
	if *daemonMode {
		d := daemon{}
		d.run()
		return
	}
	gLog.Println(LevelINFO, &gConf)
	setFirewall()
	network := P2PNetworkInstance(&gConf.Network)
	if ok := network.Connect(30000); !ok {
		gLog.Println(LevelERROR, "P2PNetwork login error")
		return
	}
	gLog.Println(LevelINFO, "waiting for connection...")
	forever := make(chan bool)
	<-forever
}
