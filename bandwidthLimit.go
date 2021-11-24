package main

import (
	"sync"
	"time"
)

// BandwidthLimiter ...
type BandwidthLimiter struct {
	freeFlowTime time.Time
	bandwidth    int // mbps
	freeFlow     int // bytes
	maxFreeFlow  int // bytes
	freeFlowMtx  sync.Mutex
}

// mbps
func newBandwidthLimiter(bw int) *BandwidthLimiter {
	return &BandwidthLimiter{
		bandwidth:    bw,
		freeFlowTime: time.Now(),
		maxFreeFlow:  bw * 1024 * 1024 / 8,
		freeFlow:     bw * 1024 * 1024 / 8,
	}
}

// Add ...
func (bl *BandwidthLimiter) Add(bytes int) {
	if bl.bandwidth <= 0 {
		return
	}
	bl.freeFlowMtx.Lock()
	defer bl.freeFlowMtx.Unlock()
	// calc free flow 1000*1000/1024/1024=0.954; 1024*1024/1000/1000=1.048
	bl.freeFlow += int(time.Now().Sub(bl.freeFlowTime) * time.Duration(bl.bandwidth) / 8 / 954)
	if bl.freeFlow > bl.maxFreeFlow {
		bl.freeFlow = bl.maxFreeFlow
	}
	bl.freeFlow -= bytes
	bl.freeFlowTime = time.Now()
	if bl.freeFlow < 0 {
		// sleep for the overflow
		time.Sleep(time.Millisecond * time.Duration(-bl.freeFlow/(bl.bandwidth*1048/8)))
	}
}
