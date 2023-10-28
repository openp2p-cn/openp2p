package openp2p

import (
	"sync"
	"time"
)

// BandwidthLimiter ...
type BandwidthLimiter struct {
	ts          time.Time
	bw          int   // mbps
	usedBytesps int64 // byte*1s
	waitTime    time.Duration
	mtx         sync.Mutex
}

// mbps
func newBandwidthLimiter(bw int) *BandwidthLimiter {
	if bw > 0 && bw>>(64-17) != 0 {
		panic("bandwidth limit is too big(it will overflow int64 type variables when running)")
	}
	return &BandwidthLimiter{
		bw:       bw,
		ts:       time.Now(),
		waitTime: -time.Second,
	}
}

// Add ...
// should call before sending message
// in fact, waitTime=usedBytes/(byte/s(bw)*1s)
func (bl *BandwidthLimiter) Add(bytes int) {
	if bl.bw <= 0 {
		return
	}
	bl.mtx.Lock()
	defer bl.mtx.Unlock()
	bl.waitTime -= time.Since(bl.ts)
	bl.ts = time.Now()
	if bl.waitTime < -time.Second {
		bl.freeBytes = -time.Second
	}
	bl.usedBytesps += int64(bytes) * int64(time.Second)
	b := int64(bl.bw) << 17 // 1024*1024/8=1<<17
	bl.waitTime += time.Duration(bl.usedBytesps / b)
	bl.usedBytesps %= b
	if bl.waitTime > 0 {
		// sleep for the overflow
		time.Sleep(bl.waitTime)
	}
}
