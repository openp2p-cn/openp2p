// Time-based One-time Password
package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
)

const TOTPStep = 30 // 30s
func GenTOTP(user string, password string, ts int64) uint64 {
	step := ts / TOTPStep
	mac := hmac.New(sha256.New, []byte(user+password))
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, uint64(step))
	mac.Write(b)
	num := binary.LittleEndian.Uint64(mac.Sum(nil)[:8])
	// fmt.Printf("%x\n", mac.Sum(nil))
	return num
}

func VerifyTOTP(code uint64, user string, password string, ts int64) bool {
	if code == 0 {
		return false
	}
	if code == GenTOTP(user, password, ts) || code == GenTOTP(user, password, ts-TOTPStep) || code == GenTOTP(user, password, ts+TOTPStep) {
		return true
	}
	return false
}
