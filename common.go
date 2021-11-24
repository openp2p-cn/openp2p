package main

import (
	"crypto/aes"
	"crypto/cipher"
	"fmt"
	"net"
)

func getmac(ip string) string {
	//get mac relative to the ip address which connected to the mq.
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	firstMac := ""
	for _, iface := range ifaces {
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			if firstMac == "" {
				firstMac = iface.HardwareAddr.String()
			}
			if ipNet, ok := addr.(*net.IPNet); ok && ipNet.IP.String() == ip {
				if iface.HardwareAddr.String() != "" {
					return iface.HardwareAddr.String()
				}
				return firstMac
			}
		}
	}
	return firstMac
}

var cbcIVBlock = []byte("UHNJUSBACIJFYSQN")

var paddingArray = [][]byte{
	{0},
	{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1},
	{2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2},
	{3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3},
	{4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4},
	{5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5},
	{6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6},
	{7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7},
	{8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8},
	{9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9, 9},
	{10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10, 10},
	{11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11, 11},
	{12, 12, 12, 12, 12, 12, 12, 12, 12, 12, 12, 12, 12, 12, 12, 12},
	{13, 13, 13, 13, 13, 13, 13, 13, 13, 13, 13, 13, 13, 13, 13, 13},
	{14, 14, 14, 14, 14, 14, 14, 14, 14, 14, 14, 14, 14, 14, 14, 14},
	{15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15},
	{16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16, 16},
}

func pkcs7Padding(plainData []byte, dataLen, blockSize int) int {
	padLen := blockSize - dataLen%blockSize
	pPadding := plainData[dataLen : dataLen+padLen]

	copy(pPadding, paddingArray[padLen][:padLen])
	return padLen
}

func pkcs7UnPadding(origData []byte, dataLen int) ([]byte, error) {
	unPadLen := int(origData[dataLen-1])
	if unPadLen <= 0 || unPadLen > 16 {
		return nil, fmt.Errorf("wrong pkcs7 padding head size:%d", unPadLen)
	}
	return origData[:(dataLen - unPadLen)], nil
}

func encryptBytes(key []byte, out, in []byte, plainLen int) ([]byte, error) {
	if len(key) == 0 {
		return in[:plainLen], nil
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	//iv := out[:aes.BlockSize]
	//if _, err := io.ReadFull(rand.Reader, iv); err != nil {
	//	return nil, err
	//}
	mode := cipher.NewCBCEncrypter(block, cbcIVBlock)
	total := pkcs7Padding(in, plainLen, aes.BlockSize) + plainLen
	mode.CryptBlocks(out[:total], in[:total])
	return out[:total], nil
}

func decryptBytes(key []byte, out, in []byte, dataLen int) ([]byte, error) {
	if len(key) == 0 {
		return in[:dataLen], nil
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	mode := cipher.NewCBCDecrypter(block, cbcIVBlock)
	mode.CryptBlocks(out[:dataLen], in[:dataLen])
	return pkcs7UnPadding(out, dataLen)
}
