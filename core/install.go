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
	gLog.i("openp2p start. version: %s", OpenP2PVersion)
	gLog.i("Contact: QQ group 16947733, Email openp2p.cn@gmail.com")
	gLog.i("install start")
	defer gLog.i("install end")
	parseParams("install", "")
	// auto uninstall
	uninstall(false)
	gLog.i("install path: %s", defaultInstallPath)
	targetPath := filepath.Join(defaultInstallPath, defaultBinName)
	d := daemon{}
	// copy files

	binPath, _ := os.Executable()
	src, errFiles := os.Open(binPath) // can not use args[0], on Windows call openp2p is ok(=openp2p.exe)
	if errFiles != nil {
		gLog.e("os.Open %s error:%s", os.Args[0], errFiles)
		return
	}

	dst, errFiles := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0775)
	if errFiles != nil {
		time.Sleep(time.Second * 5) // maybe windows defender occupied the file, retry
		dst, errFiles = os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0775)
		if errFiles != nil {
			gLog.e("os.OpenFile %s error:%s", targetPath, errFiles)
			return
		}
	}

	_, errFiles = io.Copy(dst, src)
	if errFiles != nil {
		gLog.e("io.Copy error:%s", errFiles)
		return
	}
	src.Close()
	dst.Close()

	// install system service
	err := d.Control("install", targetPath, []string{"-d"})
	if err == nil {
		gLog.i("install system service ok.")
	}
	time.Sleep(time.Second * 2)
	err = d.Control("start", targetPath, []string{"-d"})
	if err != nil {
		gLog.e("start openp2p service error:%s", err)
	} else {
		gLog.i("start openp2p service ok.")
	}
	gConf.save()
	gLog.i("Visit WebUI on https://console.openp2p.cn")
}

func installByFilename() {
	params := strings.Split(filepath.Base(os.Args[0]), "-")
	if len(params) < 4 {
		return
	}
	serverHost := params[1]
	token := params[2]
	gLog.i("install start")
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
		gLog.e("install by filename, start process error:%s", err)
		return
	}
	gLog.i("install end")
	gLog.i("Visit WebUI on https://console.openp2p.cn")
	fmt.Println("Press the Any Key to exit")
	fmt.Scanln()
	os.Exit(0)
}

func uninstall(rmFiles bool) {
	gLog.i("uninstall start")
	defer gLog.i("uninstall end")
	d := daemon{}
	err := d.Control("stop", "", nil)
	if err != nil { // service maybe not install
		gLog.d("stop service error:%s", err)
	}
	err = d.Control("uninstall", "", nil)
	if err != nil {
		gLog.d("uninstall system service error:%s", err)
	} else {
		gLog.i("uninstall system service ok.")
	}
	time.Sleep(time.Second * 3)
	binPath := filepath.Join(defaultInstallPath, defaultBinName)
	os.Remove(binPath + "0")
	os.Remove(binPath)
	if rmFiles {
		if err := os.RemoveAll(defaultInstallPath); err != nil {
			gLog.e("RemoveAll %s error:%s", defaultInstallPath, err)
		}
	}

}
