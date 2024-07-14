package openp2p

import (
	"fmt"
	"testing"
)

func TestSelectPriority(t *testing.T) {
	writeData := make(chan []byte, WriteDataChanSize)
	writeDataSmall := make(chan []byte, WriteDataChanSize/30)
	for i := 0; i < 100; i++ {
		writeData <- []byte("data")
		writeDataSmall <- []byte("small data")
	}
	for i := 0; i < 100; i++ {
		select {
		case buff := <-writeDataSmall:
			fmt.Printf("got small data:%s\n", string(buff))
		case buff := <-writeData:
			fmt.Printf("got data:%s\n", string(buff))
		}
	}

}
