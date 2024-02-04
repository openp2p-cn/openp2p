package aes_cbc

import (
	"crypto/aes"
	"crypto/cipher"
	"fmt"
)

var cbcIVBlock = []byte("UHNJUSBACIJFYSQN")

/*
func PKCS7PaddingArray(size byte)[]byte{
	a:=make([]byte,size)
	for i:=byte(0);i<size;i++{
		a[i]=size
	}
	return a
}
*/
/*
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
*/

func PKCS7Padding(plainData []byte, blockSize int) []byte {
	var padLen = blockSize - len(plainData)%blockSize
	// pPadding := plainData[dataLen : dataLen+padLen]
	if cap(plainData[len(plainData):]) < padLen {
		new := make([]byte, len(plainData)+padLen)
		copy(new, plainData)
		plainData = new[:len(plainData)]
	}
	pPadding := plainData[len(plainData):][:padLen]
	for i := 0; i < padLen; i++ {
		pPadding[i] = byte(padLen)
	}
	// copy(pPadding, paddingArray[padLen][:padLen])
	return plainData[:len(plainData)+padLen]
}

func PKCS7UnPadding(origData []byte) []byte {
	var unPadLen = origData[len(origData)-1]
	return origData[:len(origData)-int(unPadLen)]
}

// AES-CBC
func Encrypt(key []byte, out, in []byte, plainLen int) ([]byte, error) {
	if len(key) == 0 {
		return in[:plainLen], nil
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	// iv := out[:aes.BlockSize]
	// if _, err := io.ReadFull(rand.Reader, iv); err != nil {
	// 	return nil, err
	// }
	mode := cipher.NewCBCEncrypter(block, cbcIVBlock)
	in = PKCS7Padding(in[:plainLen], aes.BlockSize)
	mode.CryptBlocks(out[:len(in)], in)
	return out[:len(in)], nil
}

func Decrypt(key []byte, out, in []byte, dataLen int) ([]byte, error) {
	if len(key) == 0 {
		return in[:dataLen], nil
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	mode := cipher.NewCBCDecrypter(block, cbcIVBlock)
	mode.CryptBlocks(out[:dataLen], in[:dataLen])
	if out[dataLen-1] > aes.BlockSize {
		return nil, fmt.Errorf("wrong pkcs7 padding head size: %d", out[dataLen-1])
	}
	return PKCS7UnPadding(out[:dataLen]), nil
}
