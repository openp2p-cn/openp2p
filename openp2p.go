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
	gLog = NewLogger(binDir, "openp2p", LvDEBUG, 1024*1024, LogFileAndConsole)
	// TODO: install sub command, deamon process
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version", "-v", "--version":
			fmt.Println(OpenP2PVersion)
			return
		case "update":
			gLog = NewLogger(filepath.Dir(os.Args[0]), "openp2p", LvDEBUG, 1024*1024, LogFileAndConsole)
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
