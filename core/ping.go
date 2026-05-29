package openp2p

import (
	"fmt"
	"net"
	"os"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

// 定义ICMP回显请求和应答的结构
type ICMPMessage struct {
	Type     uint8
	Code     uint8
	Checksum uint16
	Ident    uint16
	Seq      uint16
	Data     []byte
}

// Ping sends an ICMP Echo request to the specified host and returns the response time.
func Ping(host string) (time.Duration, error) {
	// Resolve the IP address of the host
	ipAddr, err := net.ResolveIPAddr("ip4", host)
	if err != nil {
		return 0, fmt.Errorf("failed to resolve host: %v", err)
	}

	// Create an ICMP listener
	conn, err := net.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		return 0, fmt.Errorf("failed to create ICMP connection: %v", err)
	}
	defer conn.Close()

	// Create an ICMP Echo request message
	message := icmp.Message{
		Type: ipv4.ICMPTypeEcho,
		Code: 0,
		Body: &icmp.Echo{
			ID:   os.Getpid() & 0xffff,
			Seq:  1,
			Data: []byte("HELLO-R-U-THERE"),
		},
	}

	// Marshal the message into binary form
	messageBytes, err := message.Marshal(nil)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal ICMP message: %v", err)
	}

	// Send the ICMP Echo request
	start := time.Now()
	if _, err := conn.WriteTo(messageBytes, ipAddr); err != nil {
		return 0, fmt.Errorf("failed to send ICMP request: %v", err)
	}

	// Set a deadline for the response
	err = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err != nil {
		return 0, fmt.Errorf("failed to set read deadline: %v", err)
	}

	// Read the ICMP response
	response := make([]byte, 1500)
	n, _, err := conn.ReadFrom(response)
	if err != nil {
		return 0, fmt.Errorf("failed to read ICMP response: %v", err)
	}

	// Parse the ICMP response message
	parsedMessage, err := icmp.ParseMessage(ipv4.ICMPTypeEchoReply.Protocol(), response[:n])
	if err != nil {
		return 0, fmt.Errorf("failed to parse ICMP response: %v", err)
	}

	// Check if the response is an Echo reply
	if parsedMessage.Type == ipv4.ICMPTypeEchoReply {
		duration := time.Since(start)
		return duration, nil
	} else {
		return 0, fmt.Errorf("unexpected ICMP message: %+v", parsedMessage)
	}
}
