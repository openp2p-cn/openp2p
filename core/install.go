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

// examples:
// listen:
// ./openp2p install -node hhd1207-222 -token YOUR-TOKEN -sharebandwidth 0
// listen and build p2papp:
// ./openp2p install -node hhd1207-222 -token YOUR-TOKEN -sharebandwidth 0 -peernode hhdhome-n1 -dstip 127.0.0.1 -dstport 50022 -protocol tcp -srcport 22
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
		gLog.Println(LvERROR, "cd error:", err)
		return
	}
	uninstall()
	// save config file
	parseParams("install")
	targetPath := filepath.Join(defaultInstallPath, defaultBinName)
	d := daemon{}
	binPath, _ := os.Executable() // can not use args[0], on Windows call openp2p is ok(=openp2p.exe)
	if targetPath != binPath {
		// copy files
		errFiles := copyFile(targetPath, binPath)
		if errFiles != nil {
			gLog.Println(LvERROR, errFiles)
		}
	}

	// install system service
	gLog.Println(LvINFO, "targetPath:", targetPath)
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

func copyFile(dst, src string) error {
	srcF, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("os.Open %s error: %w", src, err)
	}
	defer srcF.Close()
	dstF, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0775)
	if err != nil {
		return fmt.Errorf("os.OpenFile %s error: %w", dst, err)
	}
	defer dstF.Close()
	_, err = io.Copy(dstF, srcF)
	if err != nil {
		return fmt.Errorf("io.Copy error: %w", err)
	}
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
