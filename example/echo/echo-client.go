package main

import (
	"fmt"
	op "openp2p/core"
	"time"
)

func main() {
	op.Run()
	for i := 0; i < 10; i++ {
		go echoClient("5800-debug")
	}
	echoClient("5800-debug")
}

func echoClient(peerNode string) {
	sendDatalen := op.ReadBuffLen
	sendBuff := make([]byte, sendDatalen)
	for i := 0; i < len(sendBuff); i++ {
		sendBuff[i] = byte('A' + i/100)
	}
	// peerNode = "YOUR-PEER-NODE-NAME"
	if err := op.GNetwork.ConnectNode(peerNode); err != nil {
		fmt.Println("connect error:", err)
		return
	}
	for i := 0; ; i++ {
		sendBuff[1] = 'A' + byte(i%26)
		if err := op.GNetwork.WriteNode(op.NodeNameToID(peerNode), sendBuff[:sendDatalen]); err != nil {
			fmt.Println("write error:", err)
			break
		}
		nd := op.GNetwork.ReadNode(time.Second * 10)
		if nd == nil {
			fmt.Printf("waiting for node data\n")
			time.Sleep(time.Second * 10)
			continue
		}
		fmt.Printf("read %d len=%d data=%s\n", nd.NodeID, len(nd.Data), nd.Data[:16]) // only print 16 bytes
	}
}
