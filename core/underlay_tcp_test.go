package openp2p

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDialTCP(t *testing.T) {
	baseDir := filepath.Dir(os.Args[0])
	os.Chdir(baseDir) // for system service
	gLog = NewLogger(baseDir, ProductName, LvDEBUG, 1024*1024, LogFile|LogConsole)
	// ul, err := dialTCP("[240e:3b1:6f6:d14:1c0b:9605:554d:351c]", 3389, 0, LinkModeTCP6)
	// if err != nil || ul == nil {
	// 	t.Error("dialTCP error:", err)
	// }
	ul, err := dialTCP("192.168.3.9", 3389, 0, LinkModeTCP6)
	if err != nil || ul == nil {
		t.Error("dialTCP error:", err)
	}
}
