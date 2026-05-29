package openp2p

import (
	"bufio"
	"bytes"
	"io/ioutil"
	"os"
	"runtime"
	"strings"
	"syscall"
)

var (
	defaultInstallPath = "/usr/local/openp2p"
)

const (
	defaultBinName = "openp2p"
)

func getOsName() (osName string) {
	if runtime.GOOS == "android" {
		return "Android"
	}
	var sysnamePath string
	sysnamePath = "/etc/redhat-release"
	_, err := os.Stat(sysnamePath)
	if err != nil && os.IsNotExist(err) {
		str := "PRETTY_NAME="
		f, err := os.Open("/etc/os-release")
		if err != nil && os.IsNotExist(err) {
			str = "DISTRIB_ID="
			f, err = os.Open("/etc/openwrt_release")
		}
		if err == nil {
			buf := bufio.NewReader(f)
			for {
				line, err := buf.ReadString('\n')
				if err == nil {
					line = strings.TrimSpace(line)
					pos := strings.Count(line, str)
					if pos > 0 {
						len1 := len([]rune(str)) + 1
						rs := []rune(line)
						osName = string(rs[len1 : (len(rs))-1])
						break
					}
				} else {
					break
				}
			}
		}
	} else {
		buff, err := ioutil.ReadFile(sysnamePath)
		if err == nil {
			osName = string(bytes.TrimSpace(buff))
		}
	}
	if osName == "" {
		osName = "Linux"
	}
	return
}

func setRLimit() error {
	var limit syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &limit); err != nil {
		return err
	}
	limit.Max = 65536
	limit.Cur = limit.Max
	if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &limit); err != nil {
		return err
	}
	return nil
}

func setFirewall() {
}
