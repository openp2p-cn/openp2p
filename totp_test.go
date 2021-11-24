// Time-based One-time Password
package main

import (
	"testing"
	"time"
)

func TestTOTP(t *testing.T) {
	for i := 0; i < 20; i++ {
		ts := time.Now().Unix()
		code := GenTOTP("testuser1", "testpassword1", ts)
		t.Log(code)
		if !VerifyTOTP(code, "testuser1", "testpassword1", ts) {
			t.Error("TOTP error")
		}
		if !VerifyTOTP(code, "testuser1", "testpassword1", ts-10) {
			t.Error("TOTP error")
		}
		if !VerifyTOTP(code, "testuser1", "testpassword1", ts+10) {
			t.Error("TOTP error")
		}
		if VerifyTOTP(code, "testuser1", "testpassword1", ts+60) {
			t.Error("TOTP error")
		}
		if VerifyTOTP(code, "testuser2", "testpassword1", ts+1) {
			t.Error("TOTP error")
		}
		if VerifyTOTP(code, "testuser1", "testpassword2", ts+1) {
			t.Error("TOTP error")
		}
		time.Sleep(time.Second)
		t.Log("round", i, " ", ts, " test ok")
	}

}
