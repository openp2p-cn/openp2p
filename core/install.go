package openp2p

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func install() {
	gLog.Println(LvINFO, "openp2p start. version: ", OpenP2PVersion)
	gLog.Println(LvINFO, "Contact: QQ group 16947733, Email openp2p.cn@gmail.com")
	gLog.Println(LvINFO, "install start")
	defer gLog.Println(LvINFO, "install end")
	// auto uninstall
	err := os.MkdirAll(defaultInstallPath, 0775)

	if err != nil {
		gLog.Printf(LvERROR, "MkdirAll %s error:%s", defaultInstallPath, err)
		return
	}
	err = os.Chdir(defaultInstallPath)
	if err != nil {
		gLog.Println(LvERROR, "Chdir error:", err)
		return
	}

	uninstall()
	// save config file
	parseParams("install", "")
	targetPath := filepath.Join(defaultInstallPath, defaultBinName)
	d := daemon{}
	// copy files

	binPath, _ := os.Executable()
	src, errFiles := os.Open(binPath) // can not use args[0], on Windows call openp2p is ok(=openp2p.exe)
	if errFiles != nil {
		gLog.Printf(LvERROR, "os.Open %s error:%s", os.Args[0], errFiles)
		return
	}

	dst, errFiles := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0775)
	if errFiles != nil {
		gLog.Printf(LvERROR, "os.OpenFile %s error:%s", targetPath, errFiles)
		return
	}

	_, errFiles = io.Copy(dst, src)
	if errFiles != nil {
		gLog.Printf(LvERROR, "io.Copy error:%s", errFiles)
		return
	}
	src.Close()
	dst.Close()

	// install system service
	err = d.Control("install", targetPath, []string{"-d"})
	if err == nil {
		gLog.Println(LvINFO, "install system service ok.")
	}
	time.Sleep(time.Second * 2)
	err = d.Control("start", targetPath, []string{"-d"})
	if err != nil {
		gLog.Println(LvERROR, "start openp2p service error:", err)
	} else {
		gLog.Println(LvINFO, "start openp2p service ok.")
	}
	gLog.Println(LvINFO, "Visit WebUI on https://console.openp2p.cn")
}

func installByFilename() {
	params := strings.Split(filepath.Base(os.Args[0]), "-")
	if len(params) < 4 {
		return
	}
	serverHost := params[1]
	token := params[2]
	gLog.Println(LvINFO, "install start")
	targetPath := os.Args[0]
	args := []string{"install"}
	args = append(args, "-serverhost")
	args = append(args, serverHost)
	args = append(args, "-token")
	args = append(args, token)
	env := os.Environ()
	cmd := exec.Command(targetPath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = env
	err := cmd.Run()
	if err != nil {
		gLog.Println(LvERROR, "install by filename, start process error:", err)
		return
	}
	gLog.Println(LvINFO, "install end")
	gLog.Println(LvINFO, "Visit WebUI on https://console.openp2p.cn")
	fmt.Println("Press the Any Key to exit")
	fmt.Scanln()
	os.Exit(0)
}
func uninstall() {
	gLog.Println(LvINFO, "uninstall start")
	defer gLog.Println(LvINFO, "uninstall end")
	d := daemon{}
	err := d.Control("stop", "", nil)
	if err != nil { // service maybe not install
		return
	}
	err = d.Control("uninstall", "", nil)
	if err != nil {
		gLog.Println(LvERROR, "uninstall system service error:", err)
	} else {
		gLog.Println(LvINFO, "uninstall system service ok.")
	}
	binPath := filepath.Join(defaultInstallPath, defaultBinName)
	os.Remove(binPath + "0")
	os.Remove(binPath)
	// os.RemoveAll(defaultInstallPath)  // reserve config.json
}
