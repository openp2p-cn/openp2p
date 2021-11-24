package main

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
