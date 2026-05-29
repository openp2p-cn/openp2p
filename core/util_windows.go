package openp2p

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/windows/registry"
)

var (
	defaultInstallPath = "C:\\Program Files\\OpenP2P"
)

const (
	defaultBinName = "openp2p.exe"
)

func getOsName() (osName string) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion`, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return
	}
	defer k.Close()
	pn, _, err := k.GetStringValue("ProductName")
	if err == nil {
		currentBuild, _, err := k.GetStringValue("CurrentBuild")
		if err != nil {
			return
		}
		buildNumber, err := strconv.Atoi(currentBuild)
		if err != nil {
			return
		}
		if buildNumber >= 22000 {
			pn = strings.Replace(pn, "Windows 10", "Windows 11", 1)
		}
		osName = pn
	}
	return
}

func setRLimit() error {
	return nil
}

func setFirewall() {
	fullPath, err := filepath.Abs(os.Args[0])
	if err != nil {
		gLog.e("add firewall error:%s", err)
		return
	}
	isXP := false
	osName := getOsName()
	if strings.Contains(osName, "XP") || strings.Contains(osName, "2003") {
		isXP = true
	}
	if isXP {
		exec.Command("cmd.exe", `/c`, fmt.Sprintf(`netsh firewall del allowedprogram "%s"`, fullPath)).Run()
		exec.Command("cmd.exe", `/c`, fmt.Sprintf(`netsh firewall add allowedprogram "%s" "%s" ENABLE`, ProductName, fullPath)).Run()
	} else { // win7 or later
		exec.Command("cmd.exe", `/c`, fmt.Sprintf(`netsh advfirewall firewall del rule name="%s"`, ProductName)).Run()
		exec.Command("cmd.exe", `/c`, fmt.Sprintf(`netsh advfirewall firewall add rule name="%s" dir=in action=allow program="%s" enable=yes`, ProductName, fullPath)).Run()
	}
}
