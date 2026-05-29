package openp2p

import (
	"fmt"
	"log"
	"testing"
)

func TestAESCBC(t *testing.T) {
	for packetSize := 1; packetSize <= 8192; packetSize++ {
		log.Println("test packetSize=", packetSize)
		data := make([]byte, packetSize)
		for i := 0; i < packetSize; i++ {
			data[i] = byte('0' + i%10)
		}
		p2pEncryptBuf := make([]byte, len(data)+PaddingSize)
		inBuf := make([]byte, len(data)+PaddingSize)
		copy(inBuf, data)
		cryptKey := []byte("0123456789ABCDEF")
		sendBuf, err := encryptBytes(cryptKey, p2pEncryptBuf, inBuf, len(data))
		if err != nil {
			t.Errorf("encrypt packet failed:%s", err)
		}
		log.Printf("encrypt data len=%d\n", len(sendBuf))

		decryptBuf := make([]byte, len(sendBuf))
		outBuf, err := decryptBytes(cryptKey, decryptBuf, sendBuf, len(sendBuf))
		if err != nil {
			t.Errorf("decrypt packet failed:%s", err)
		}
		// log.Printf("len=%d,content=%s\n", len(outBuf), outBuf)
		log.Printf("decrypt data len=%d\n", len(outBuf))
		log.Println("validate")
		for i := 0; i < len(outBuf); i++ {
			if outBuf[i] != byte('0'+i%10) {
				t.Error("validate failed")
			}
		}
		log.Println("validate ok")
	}

}

func TestNetInfo(t *testing.T) {
	log.Println(netInfo())
}

func assertCompareVersion(t *testing.T, v1 string, v2 string, result int) {
	if compareVersion(v1, v2) != result {
		t.Errorf("compare version %s %s fail\n", v1, v2)
	}
}
func assertParseMajorVer(t *testing.T, v string, result int) {
	if parseMajorVer(v) != result {
		t.Errorf("ParseMajorVer %s fail\n", v)
	}
}
func TestCompareVersion(t *testing.T) {
	// test =
	assertCompareVersion(t, "0.98.0", "0.98.0", EQUAL)
	assertCompareVersion(t, "0.98", "0.98", EQUAL)
	assertCompareVersion(t, "1.4.0", "1.4.0", EQUAL)
	assertCompareVersion(t, "1.5.0", "1.5.0", EQUAL)
	// test >
	assertCompareVersion(t, "0.98.0.22345", "0.98.0.12345", GREATER)
	assertCompareVersion(t, "1.98.0.12345", "0.98", GREATER)
	assertCompareVersion(t, "10.98.0.12345", "9.98.0.12345", GREATER)
	assertCompareVersion(t, "1.4.0", "0.98.0.12345", GREATER)
	assertCompareVersion(t, "1.4", "0.98.0.12345", GREATER)
	assertCompareVersion(t, "1", "0.98.0.12345", GREATER)
	// test <
	assertCompareVersion(t, "0.98.0.12345", "0.98.0.12346", LESS)
	assertCompareVersion(t, "9.98.0.12345", "10.98.0.12345", LESS)
	assertCompareVersion(t, "1.4.2", "1.5.0", LESS)
	assertCompareVersion(t, "", "1.5.0", LESS)

}

func TestParseMajorVer(t *testing.T) {

	assertParseMajorVer(t, "0.98.0", 0)
	assertParseMajorVer(t, "0.98", 0)
	assertParseMajorVer(t, "1.4.0", 1)
	assertParseMajorVer(t, "1.5.0", 1)

	assertParseMajorVer(t, "0.98.0.22345", 0)
	assertParseMajorVer(t, "1.98.0.12345", 1)
	assertParseMajorVer(t, "10.98.0.12345", 10)
	assertParseMajorVer(t, "1.4.0", 1)
	assertParseMajorVer(t, "1.4", 1)
	assertParseMajorVer(t, "1", 1)
	assertParseMajorVer(t, "2", 2)
	assertParseMajorVer(t, "3", 3)
	assertParseMajorVer(t, "2.1.0", 2)
	assertParseMajorVer(t, "3.0.0", 3)

}

func TestIsIPv6(t *testing.T) {
	tests := []struct {
		ipStr string
		want  bool
	}{
		{"2001:0db8:85a3:0000:0000:8a2e:0370:7334", true}, // 有效的 IPv6 地址
		{"2001:db8::2:1", true},                           // 有效的 IPv6 地址
		{"192.168.1.1", false},                            // 无效的 IPv6 地址，是 IPv4
		{"2001:db8::G:1", false},                          // 无效的 IPv6 地址，包含非法字符
		// 可以添加更多测试用例
	}

	for _, tt := range tests {
		got := IsIPv6(tt.ipStr)
		if got != tt.want {
			t.Errorf("isValidIPv6(%s) = %v, want %v", tt.ipStr, got, tt.want)
		}
	}
}

func TestNodeID(t *testing.T) {
	node1 := "n1-stable"
	node2 := "tony-stable"
	nodeID1 := NodeNameToID(node1)
	nodeID2 := NodeNameToID(node2)
	if nodeID1 < nodeID2 {
		fmt.Printf("%s < %s\n", node1, node2)
	} else {
		fmt.Printf("%s >= %s\n", node1, node2)
	}
}

func TestCalcRetryTime(t *testing.T) {
	// 0-2 < 13s
	// 3-5:300
	// 6-10:600
	tests := []struct {
		retryNum float64
		want     float64
	}{
		{1.0, 10},
		{5.0, 13},
		{10.0, 180},
		{15.0, 9000},
		{18.0, 90000},
		// 可以添加更多测试用例
	}

	for _, tt := range tests {
		got := calcRetryTimeRelay(tt.retryNum)
		if got < tt.want*0.85 || got > tt.want*1.15 {
			t.Errorf("calcRetryTime(%f) = %f, want %f", tt.retryNum, got, tt.want)
		}
	}

	for i := 0; i < 20; i++ {
		log.Printf("%d retryTime=%fs", i, calcRetryTimeRelay(float64(i)))
	}
}

func TestCalcRetryTimeDirect(t *testing.T) {
	// 0-2 < 13s
	// 3-5:300
	// 6-10:600
	tests := []struct {
		retryNum float64
		want     float64
	}{
		{1.0, 10},
		{5.0, 13},
		{10.0, 180},
		{15.0, 9000},
		{18.0, 90000},
		// 可以添加更多测试用例
	}

	for _, tt := range tests {
		got := calcRetryTimeRelay(tt.retryNum)
		if got < tt.want*0.85 || got > tt.want*1.15 {
			t.Errorf("calcRetryTime(%f) = %f, want %f", tt.retryNum, got, tt.want)
		}
	}

	for i := 0; i < 20; i++ {
		log.Printf("%d retryTime=%fs", i, calcRetryTimeDirect(float64(i)))
	}
}
