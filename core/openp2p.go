package openp2p

import (
	"fmt"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

var GNetwork *P2PNetwork

func Run() {
	rand.Seed(time.Now().UnixNano())
	baseDir := filepath.Dir(os.Args[0])
	os.Chdir(baseDir) // for system service
	gLog = NewLogger(baseDir, ProductName, LvDEBUG, 1024*1024, LogFile|LogConsole)
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version", "-v", "--version":
			fmt.Println(OpenP2PVersion)
			return
		case "install":
			install()
			return
		case "uninstall":
			uninstall(true)
			return
		case "start":
			d := daemon{}
			err := d.Control("start", "", nil)
			if err != nil {
				log.Println("openp2p start error:", err)
				return
			}
			log.Println("openp2p start ok")
			return
		case "stop":
			d := daemon{}
			err := d.Control("stop", "", nil)
			if err != nil {
				log.Println("openp2p stop error:", err)
				return
			}
			log.Println("openp2p stop ok")
			return
		}
	} else {
		installByFilename()
	}
	parseParams("", "")
	gLog.i("openp2p start. version: %s", OpenP2PVersion)
	gLog.i("Contact: QQ group 16947733, Email openp2p.cn@gmail.com")

	if gConf.daemonMode {
		d := daemon{}
		d.run()
		return
	}

	gLog.i("node=%s, serverHost=%s, serverPort=%d", gConf.Network.Node, gConf.Network.ServerHost, gConf.Network.ServerPort)
	setFirewall()
	err := setRLimit()
	if err != nil {
		gLog.i("setRLimit error:%s", err)
	}
	P2PNetworkInstance()
	if ok := GNetwork.Connect(30000); !ok {
		gLog.e("P2PNetwork login error")
		return
	}
	// gLog.i("waiting for connection...")
	forever := make(chan bool)
	<-forever
}

// for Android app
// gomobile not support uint64 exported to java

func RunAsModule(baseDir string, token string, bw int, logLevel int) *P2PNetwork {
	rand.Seed(time.Now().UnixNano())
	os.Chdir(baseDir) // for system service
	gLog = NewLogger(baseDir, ProductName, LvINFO, 1024*1024, LogFile|LogConsole)

	parseParams("", "")

	n, err := strconv.ParseUint(token, 10, 64)
	if err == nil && n > 0 {
		gConf.setToken(n)
	}
	if n <= 0 && gConf.Network.Token == 0 { // not input token
		return nil
	}
	// gLog.setLevel(LogLevel(logLevel))
	gConf.setShareBandwidth(bw)
	gLog.i("openp2p start. version: %s", OpenP2PVersion)
	gLog.i("Contact: QQ group 16947733, Email openp2p.cn@gmail.com")
	gLog.i("node=%s, serverHost=%s, serverPort=%d", gConf.Network.Node, gConf.Network.ServerHost, gConf.Network.ServerPort)

	P2PNetworkInstance()
	if ok := GNetwork.Connect(30000); !ok {
		gLog.e("P2PNetwork login error")
		return nil
	}
	// gLog.i("waiting for connection...")
	return GNetwork
}

func RunCmd(cmd string) {
	rand.Seed(time.Now().UnixNano())
	baseDir := filepath.Dir(os.Args[0])
	os.Chdir(baseDir) // for system service
	gLog = NewLogger(baseDir, ProductName, LvINFO, 1024*1024, LogFile|LogConsole)

	parseParams("", cmd)
	setFirewall()
	err := setRLimit()
	if err != nil {
		gLog.i("setRLimit error:%s", err)
	}
	P2PNetworkInstance()
	if ok := GNetwork.Connect(30000); !ok {
		gLog.e("P2PNetwork login error")
		return
	}
	forever := make(chan bool)
	<-forever
}

func GetToken(baseDir string) string {
	os.Chdir(baseDir)
	gConf.load()
	return fmt.Sprintf("%d", gConf.Network.Token)
}

func Stop() {
	os.Exit(0)
}
