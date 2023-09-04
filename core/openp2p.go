package openp2p

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

func Run() {
	rand.Seed(time.Now().UnixNano())
	baseDir := filepath.Dir(os.Args[0])
	os.Chdir(baseDir) // for system service
	gLog = NewLogger(baseDir, ProductName, LvDEBUG, 1024*1024, LogFile|LogConsole)
	// TODO: install sub command, deamon process
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version", "-v", "--version":
			fmt.Println(OpenP2PVersion)
			return
		case "install":
			install()
			return
		case "uninstall":
			uninstall()
			return
		}
	} else {
		installByFilename()
	}
	parseParams("")
	gLog.Println(LvINFO, "openp2p start. version: ", OpenP2PVersion)
	gLog.Println(LvINFO, "Contact: QQ group 16947733, Email openp2p.cn@gmail.com")

	if gConf.daemonMode {
		d := daemon{}
		d.run()
		return
	}

	gLog.Println(LvINFO, &gConf)
	setFirewall()
	err := setRLimit()
	if err != nil {
		gLog.Println(LvINFO, "setRLimit error:", err)
	}
	network := P2PNetworkInstance(&gConf.Network)
	if ok := network.Connect(30000); !ok {
		gLog.Println(LvERROR, "P2PNetwork login error")
		return
	}
	gLog.Println(LvINFO, "waiting for connection...")
	forever := make(chan bool)
	<-forever
}

var network *P2PNetwork

// for Android app
// gomobile not support uint64 exported to java
func RunAsModule(baseDir string, token string, bw int, logLevel int) *P2PNetwork {
	rand.Seed(time.Now().UnixNano())
	os.Chdir(baseDir) // for system service
	gLog = NewLogger(baseDir, ProductName, LvDEBUG, 1024*1024, LogFile|LogConsole)

	parseParams("")

	n, err := strconv.ParseUint(token, 10, 64)
	if err == nil {
		gConf.setToken(n)
	}
	gLog.setLevel(LogLevel(logLevel))
	gConf.setShareBandwidth(bw)
	gLog.Println(LvINFO, "openp2p start. version: ", OpenP2PVersion)
	gLog.Println(LvINFO, "Contact: QQ group 16947733, Email openp2p.cn@gmail.com")
	gLog.Println(LvINFO, &gConf)

	network = P2PNetworkInstance(&gConf.Network)
	if ok := network.Connect(30000); !ok {
		gLog.Println(LvERROR, "P2PNetwork login error")
		return nil
	}
	gLog.Println(LvINFO, "waiting for connection...")
	return network
}

func GetToken(baseDir string) string {
	os.Chdir(baseDir)
	gConf.load()
	return fmt.Sprintf("%d", gConf.Network.Token)
}
