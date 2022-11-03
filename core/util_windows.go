package openp2p

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const (
	defaultInstallPath = "C:\\Program Files\\OpenP2P"
	defaultBinName     = "openp2p.exe"
)

func getOsName() (osName string) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion`, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return
	}
	defer k.Close()
	pn, _, err := k.GetStringValue("ProductName")
	if err == nil {
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
		gLog.Println(LvERROR, "add firewall error:", err)
		return
	}
	isXP := false
	osName := getOsName()
	if strings.Contains(osName, "XP") || strings.Contains(osName, "2003") {
		isXP = true
	}
	if isXP {
		exec.Command("cmd.exe", `/c`, fmt.Sprintf(`netsh firewall del allowedprogram "%s"`, fullPath)).Run()
		exec.Command("cmd.exe", `/c`, fmt.Sprintf(`netsh firewall add allowedprogram "%s" "%s" ENABLE`, ProducnName, fullPath)).Run()
	} else { // win7 or later
		exec.Command("cmd.exe", `/c`, fmt.Sprintf(`netsh advfirewall firewall del rule name="%s"`, ProducnName)).Run()
		exec.Command("cmd.exe", `/c`, fmt.Sprintf(`netsh advfirewall firewall add rule name="%s" dir=in action=allow program="%s" enable=yes`, ProducnName, fullPath)).Run()
	}
}
