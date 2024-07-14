package openp2p

import (
	"testing"
	"time"
)

func TestBandwidth(t *testing.T) {
	speed := 10 * 1024 * 1024 / 8 // 10mbps
	speedl := newSpeedLimiter(speed, 1)
	oneBuffSize := 4096
	writeNum := 5000
	expectTime := oneBuffSize * writeNum / speed
	startTs := time.Now()
	for i := 0; i < writeNum; i++ {
		speedl.Add(oneBuffSize, true)
	}
	t.Logf("cost %ds, expect %ds", time.Since(startTs)/time.Second, expectTime)
	if time.Since(startTs) > time.Duration(expectTime+1)*time.Second || time.Since(startTs) < time.Duration(expectTime-1)*time.Second {
		t.Error("error")
	}
}

func TestSymmetric(t *testing.T) {
	speed := 20000 / 180
	speedl := newSpeedLimiter(speed, 180)
	oneBuffSize := 300
	writeNum := 70
	expectTime := (oneBuffSize*writeNum - 20000) / speed
	t.Logf("expect %ds", expectTime)
	startTs := time.Now()
	for i := 0; i < writeNum; i++ {
		speedl.Add(oneBuffSize, true)
	}
	t.Logf("cost %ds, expect %ds", time.Since(startTs)/time.Second, expectTime)
	if time.Since(startTs) > time.Duration(expectTime+1)*time.Second || time.Since(startTs) < time.Duration(expectTime-1)*time.Second {
		t.Error("error")
	}
}

func TestSymmetric2(t *testing.T) {
	speed := 30000 / 180
	speedl := newSpeedLimiter(speed, 180)
	oneBuffSize := 800
	writeNum := 40
	expectTime := (oneBuffSize*writeNum - 30000) / speed
	startTs := time.Now()
	for i := 0; i < writeNum; {
		if speedl.Add(oneBuffSize, true) {
			i++
		} else {
			time.Sleep(time.Millisecond)
		}
	}
	t.Logf("cost %ds, expect %ds", time.Since(startTs)/time.Second, expectTime)
	if time.Since(startTs) > time.Duration(expectTime+1)*time.Second || time.Since(startTs) < time.Duration(expectTime-1)*time.Second {
		t.Error("error")
	}
}
