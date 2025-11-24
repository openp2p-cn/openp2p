package main

import (
	"fmt"
	op2p "openp2p/core"
	"time"
)

func main() {
	op2p.Run()
	echoServer()
	forever := make(chan bool)
	<-forever
}

func echoServer() {
	// peerID := fmt.Sprintf("%d", core.NodeNameToID(peerNode))
	for {
		nd := op2p.GNetwork.ReadNode(time.Second * 10)
		if nd == nil {
			fmt.Printf("waiting for node data\n")
			// time.Sleep(time.Second * 10)
			continue
		}
		// fmt.Printf("read %s len=%d data=%s\n", nd.Node, len(nd.Data), nd.Data[:16])
		nd[0] = 'R' // echo server mark as replied
		if err := op2p.GNetwork.WriteNode(0, nd); err != nil {
			fmt.Println("write error:", err)
			break
		}
	}
}
