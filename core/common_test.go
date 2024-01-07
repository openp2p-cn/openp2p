package openp2p

import (
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
	assertCompareVersion(t, "1.5.105b2f", "1.5.105b2f", EQUAL)
	assertCompareVersion(t, "1.b5.0", "1.b5.0", EQUAL)
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
	assertCompareVersion(t, "1.5.105b2f", "1.5.105b2f.1", LESS)
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
