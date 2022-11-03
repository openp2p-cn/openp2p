// Time-based One-time Password
package openp2p

import (
	"testing"
	"time"
)

func TestTOTP(t *testing.T) {
	for i := 0; i < 20; i++ {
		ts := time.Now().Unix()
		code := GenTOTP(13666999958022769123, ts)
		t.Log(code)
		if !VerifyTOTP(code, 13666999958022769123, ts) {
			t.Error("TOTP error")
		}
		if !VerifyTOTP(code, 13666999958022769123, ts-10) {
			t.Error("TOTP error")
		}
		if !VerifyTOTP(code, 13666999958022769123, ts+10) {
			t.Error("TOTP error")
		}
		if VerifyTOTP(code, 13666999958022769123, ts+60) {
			t.Error("TOTP error")
		}
		if VerifyTOTP(code, 13666999958022769124, ts+1) {
			t.Error("TOTP error")
		}
		if VerifyTOTP(code, 13666999958022769125, ts+1) {
			t.Error("TOTP error")
		}
		time.Sleep(time.Second)
		t.Log("round", i, " ", ts, " test ok")
	}

}
