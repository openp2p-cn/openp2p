// Time-based One-time Password
package openp2p

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
)

const TOTPStep = 30 // 30s
func GenTOTP(token uint64, ts int64) uint64 {
	step := ts / TOTPStep
	tbuff := make([]byte, 8)
	binary.LittleEndian.PutUint64(tbuff, token)
	mac := hmac.New(sha256.New, tbuff)
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, uint64(step))
	mac.Write(b)
	num := binary.LittleEndian.Uint64(mac.Sum(nil)[:8])
	// fmt.Printf("%x\n", mac.Sum(nil))
	return num
}

func VerifyTOTP(code uint64, token uint64, ts int64) bool {
	if code == 0 {
		return false
	}
	if code == token {
		return true
	}
	if code == GenTOTP(token, ts) || code == GenTOTP(token, ts-TOTPStep) || code == GenTOTP(token, ts+TOTPStep) {
		return true
	}
	return false
}
