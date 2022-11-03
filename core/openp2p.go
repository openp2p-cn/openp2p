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
	gLog = NewLogger(baseDir, ProducnName, LvDEBUG, 1024*1024, LogFileAndConsole)
	// TODO: install sub command, deamon process
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version", "-v", "--version":
			fmt.Println(OpenP2PVersion)
			return
		case "update":
			gLog = NewLogger(baseDir, ProducnName, LvDEBUG, 1024*1024, LogFileAndConsole)
			targetPath := filepath.Join(defaultInstallPath, defaultBinName)
			d := daemon{}
			err := d.Control("restart", targetPath, nil)
			if err != nil {
				gLog.Println(LvERROR, "restart service error:", err)
			} else {
				gLog.Println(LvINFO, "restart service ok.")
			}
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
	gLog.Println(LvINFO, "Contact: QQ: 477503927, Email: openp2p.cn@gmail.com")

	if gConf.daemonMode {
		d := daemon{}
		d.run()
		return
	}

	gLog.Println(LvINFO, &gConf)
	setFirewall()
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
func RunAsModule(baseDir string, token string, bw int) *P2PNetwork {
	rand.Seed(time.Now().UnixNano())
	os.Chdir(baseDir) // for system service
	gLog = NewLogger(baseDir, ProducnName, LvDEBUG, 1024*1024, LogFileAndConsole)
	// TODO: install sub command, deamon process
	parseParams("")

	n, _ := strconv.ParseUint(token, 10, 64)
	gConf.setToken(n)
	gConf.setShareBandwidth(0)
	gLog.Println(LvINFO, "openp2p start. version: ", OpenP2PVersion)
	gLog.Println(LvINFO, "Contact: QQ: 477503927, Email: openp2p.cn@gmail.com")
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
