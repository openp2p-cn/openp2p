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
	// TODO: install sub command, deamon process
	// groups := flag.String("groups", "", "you could join in several groups. like: GroupName1:Password1;GroupName2:Password2; group name 8-31 characters")
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version", "-v", "--version":
			fmt.Println(OpenP2PVersion)
			return
		case "update":
			gLog = InitLogger(filepath.Dir(os.Args[0]), "openp2p", LevelDEBUG, 1024*1024, LogConsole)
			update()
			return
		}
	}

	user := flag.String("user", "", "user name. 8-31 characters")
	node := flag.String("node", "", "node name. 8-31 characters")
	password := flag.String("password", "", "user password. 8-31 characters")
	peerNode := flag.String("peernode", "", "peer node name that you want to connect")
	peerUser := flag.String("peeruser", "", "peer node user (default peeruser=user)")
	peerPassword := flag.String("peerpassword", "", "peer node password (default peerpassword=password)")
	dstIP := flag.String("dstip", "127.0.0.1", "destination ip ")
	serverHost := flag.String("serverhost", "openp2p.cn", "server host ")
	// serverHost := flag.String("serverhost", "127.0.0.1", "server host ") // for debug
	dstPort := flag.Int("dstport", 0, "destination port ")
	srcPort := flag.Int("srcport", 0, "source port ")
	protocol := flag.String("protocol", "tcp", "tcp or udp")
	noShare := flag.Bool("noshare", false, "disable using the huge numbers of shared nodes in OpenP2P network, your connectivity will be weak. also this node will not shared with others")
	shareBandwidth := flag.Int("sharebandwidth", 10, "N mbps share bandwidth limit, private node no limit")
	configFile := flag.Bool("f", false, "config file")
	daemonMode := flag.Bool("d", false, "daemonMode")
	byDaemon := flag.Bool("bydaemon", false, "start by daemon")
	logLevel := flag.Int("loglevel", 1, "0:debug 1:info 2:warn 3:error")
	flag.Parse()
	gLog = InitLogger(filepath.Dir(os.Args[0]), "openp2p", LogLevel(*logLevel), 1024*1024, LogConsole)
	gLog.Println(LevelINFO, "openp2p start. version: ", OpenP2PVersion)
	if *daemonMode {
		d := daemon{}
		d.run()
		return
	}
	if !*configFile {
		// validate cmd params
		if *node == "" {
			gLog.Println(LevelERROR, "node name not set", os.Args, len(os.Args), os.Args[0])
			return
		}
		if *user == "" {
			gLog.Println(LevelERROR, "user name not set")
			return
		}
		if *password == "" {
			gLog.Println(LevelERROR, "password not set")
			return
		}
		if *peerNode != "" {
			if *dstPort == 0 {
				gLog.Println(LevelERROR, "dstPort not set")
				return
			}
			if *srcPort == 0 {
				gLog.Println(LevelERROR, "srcPort not set")
				return
			}
		}
	}

	config := AppConfig{}
	config.PeerNode = *peerNode
	config.PeerUser = *peerUser
	config.PeerPassword = *peerPassword
	config.DstHost = *dstIP
	config.DstPort = *dstPort
	config.SrcPort = *srcPort
	config.Protocol = *protocol
	gLog.Println(LevelINFO, config)
	if *configFile {
		if err := gConf.load(); err != nil {
			gLog.Println(LevelERROR, "load config error. exit.")
			return
		}
	} else {
		gConf.add(config)
		gConf.Network = NetworkConfig{
			Node:           *node,
			User:           *user,
			Password:       *password,
			NoShare:        *noShare,
			ServerHost:     *serverHost,
			ServerPort:     27182,
			UDPPort1:       27182,
			UDPPort2:       27183,
			ipv6:           "240e:3b7:621:def0:fda4:dd7f:36a1:2803", // TODO: detect real ipv6
			shareBandwidth: *shareBandwidth,
		}
	}
	// gConf.save() // not change config file
	gConf.daemonMode = *byDaemon

	gLog.Println(LevelINFO, gConf)
	setFirewall()
	network := P2PNetworkInstance(&gConf.Network)
	if ok := network.Connect(30000); !ok {
		gLog.Println(LevelERROR, "P2PNetwork login error")
		return
	}
	for _, app := range gConf.Apps {
		// set default peer user password
		if app.PeerPassword == "" {
			app.PeerPassword = gConf.Network.Password
		}
		if app.PeerUser == "" {
			app.PeerUser = gConf.Network.User
		}
		err := network.AddApp(app)
		if err != nil {
			gLog.Println(LevelERROR, "addTunnel error")
		}
	}

	// test
	// go func() {

	// 	time.Sleep(time.Second * 30)
	// 	config := AppConfig{}
	// 	config.PeerNode = *peerNode
	// 	config.PeerUser = *peerUser
	// 	config.PeerPassword = *peerPassword
	// 	config.DstHost = *dstIP
	// 	config.DstPort = *dstPort
	// 	config.SrcPort = 32
	// 	config.Protocol = *protocol
	// 	network.AddApp(config)
	// 	// time.Sleep(time.Second * 30)
	// 	// network.DeleteTunnel(config)
	// 	// time.Sleep(time.Second * 30)
	// 	// network.DeleteTunnel(config)
	// }()

	// // TODO: http api
	// api := ClientAPI{}
	// go api.run()
	gLog.Println(LevelINFO, "waiting for connection...")
	forever := make(chan bool)
	<-forever
}
