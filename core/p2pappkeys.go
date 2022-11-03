package openp2p

import (
	"sync"
)

var p2pAppKeys sync.Map

func GetKey(appID uint64) uint64 {
	i, ok := p2pAppKeys.Load(appID)
	if !ok {
		return 0
	}
	return i.(uint64)
}

func SaveKey(appID uint64, appKey uint64) {
	p2pAppKeys.Store(appID, appKey)
}
