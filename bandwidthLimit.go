package main

import (
	"sync"
	"time"
)

// BandwidthLimiter ...
type BandwidthLimiter struct {
	ts           time.Time
	bw           int // mbps
	freeBytes    int // bytes
	maxFreeBytes int // bytes
	mtx          sync.Mutex
}

// mbps
func newBandwidthLimiter(bw int) *BandwidthLimiter {
	return &BandwidthLimiter{
		bw:           bw,
		ts:           time.Now(),
		maxFreeBytes: bw * 1024 * 1024 / 8,
		freeBytes:    bw * 1024 * 1024 / 8,
	}
}

// Add ...
func (bl *BandwidthLimiter) Add(bytes int) {
	if bl.bw <= 0 {
		return
	}
	bl.mtx.Lock()
	defer bl.mtx.Unlock()
	// calc free flow 1000*1000/1024/1024=0.954; 1024*1024/1000/1000=1.048
	bl.freeBytes += int(time.Since(bl.ts) * time.Duration(bl.bw) / 8 / 954)
	if bl.freeBytes > bl.maxFreeBytes {
		bl.freeBytes = bl.maxFreeBytes
	}
	bl.freeBytes -= bytes
	bl.ts = time.Now()
	if bl.freeBytes < 0 {
		// sleep for the overflow
		time.Sleep(time.Millisecond * time.Duration(-bl.freeBytes/(bl.bw*1048/8)))
	}
}
