package openp2p

import (
	"strings"
	"syscall"
)

const (
	defaultInstallPath = "/usr/local/openp2p"
	defaultBinName     = "openp2p"
)

func getOsName() (osName string) {
	output := execOutput("sw_vers", "-productVersion")
	osName = "Mac OS X " + strings.TrimSpace(output)
	return
}

func setRLimit() error {
	var limit syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &limit); err != nil {
		return err
	}
	limit.Cur = 10240
	if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &limit); err != nil {
		return err
	}
	return nil
}

func setFirewall() {
}
