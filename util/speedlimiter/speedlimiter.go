package speedlimiter

import (
	"fmt"
	"sync"
	"time"
)

// SpeedLimiter ...
type Instance struct {
	lastUpdate time.Time
	speed      int // per second
	precision  int // seconds
	freeCap    int
	maxFreeCap int
	mtx        sync.Mutex
}

func New(speed int, precision int) *Instance {
	return &Instance{
		speed:      speed,
		precision:  precision,
		lastUpdate: time.Now(),
		maxFreeCap: speed * precision,
		freeCap:    speed * precision,
	}
}

// Add ...
func (sl *Instance) Add(increment int, wait bool) bool {
	if sl.speed <= 0 {
		return true
	}
	sl.mtx.Lock()
	defer sl.mtx.Unlock()
	sl.freeCap += int(time.Since(sl.lastUpdate) * time.Duration(sl.speed) / time.Second)
	if sl.freeCap > sl.maxFreeCap {
		sl.freeCap = sl.maxFreeCap
	}
	if !wait && sl.freeCap < increment {
		return false
	}
	sl.freeCap -= increment
	sl.lastUpdate = time.Now()
	if sl.freeCap < 0 {
		// sleep for the overflow
		fmt.Println("sleep ", time.Millisecond*time.Duration(-sl.freeCap*100)/time.Duration(sl.speed))
		time.Sleep(time.Millisecond * time.Duration(-sl.freeCap*1000) / time.Duration(sl.speed)) // sleep ms
	}
	return true
}
