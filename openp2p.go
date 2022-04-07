package main

import (
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
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version", "-v", "--version":
			fmt.Println(OpenP2PVersion)
			return
		case "update":
			gLog = InitLogger(filepath.Dir(os.Args[0]), "openp2p", LevelDEBUG, 1024*1024, LogFileAndConsole)
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
	} else {
		installByFilename()
	}
	parseParams()
	gLog.Println(LevelINFO, "openp2p start. version: ", OpenP2PVersion)
	gLog.Println(LevelINFO, "Contact: QQ Group: 16947733, Email: openp2p.cn@gmail.com")

	if gConf.daemonMode {
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
