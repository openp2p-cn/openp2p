package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net"
	"strconv"
	"strings"
	"time"

	reuse "github.com/openp2p-cn/go-reuseport"
)

var echoConn *net.UDPConn

func natTCP(serverHost string, serverPort int, localPort int) (publicIP string, publicPort int) {
	// dialer := &net.Dialer{
	// 	LocalAddr: &net.TCPAddr{
	// 		IP:   net.ParseIP("0.0.0.0"),
	// 		Port: localPort,
	// 	},
	// }
	conn, err := reuse.DialTimeout("tcp4", fmt.Sprintf("%s:%d", "0.0.0.0", localPort), fmt.Sprintf("%s:%d", serverHost, serverPort), time.Second*5)
	// conn, err := net.Dial("tcp4", fmt.Sprintf("%s:%d", serverHost, serverPort))
	if err != nil {
		fmt.Printf("Dial tcp4 %s:%d error:%s", serverHost, serverPort, err)
		return
	}
	defer conn.Close()
	_, wrerr := conn.Write([]byte("1"))
	if wrerr != nil {
		fmt.Printf("Write error: %s\n", wrerr)
		return
	}
	b := make([]byte, 1000)
	conn.SetReadDeadline(time.Now().Add(time.Second * 5))
	n, rderr := conn.Read(b)
	if rderr != nil {
		fmt.Printf("Read error: %s\n", rderr)
		return
	}
	arr := strings.Split(string(b[:n]), ":")
	if len(arr) < 2 {
		return
	}
	publicIP = arr[0]
	port, _ := strconv.ParseInt(arr[1], 10, 32)
	publicPort = int(port)
	return

}
func natTest(serverHost string, serverPort int, localPort int, echoPort int) (publicIP string, hasPublicIP int, hasUPNPorNATPMP int, publicPort int, err error) {
	conn, err := net.ListenPacket("udp", fmt.Sprintf(":%d", localPort))
	if err != nil {
		gLog.Println(LvERROR, "natTest listen udp error:", err)
		return "", 0, 0, 0, err
	}
	defer conn.Close()

	dst, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", serverHost, serverPort))
	if err != nil {
		return "", 0, 0, 0, err
	}

	// The connection can write data to the desired address.
	msg, err := newMessage(MsgNATDetect, 0, &NatDetectReq{SrcPort: localPort, EchoPort: echoPort})
	_, err = conn.WriteTo(msg, dst)
	if err != nil {
		return "", 0, 0, 0, err
	}
	deadline := time.Now().Add(NatTestTimeout)
	err = conn.SetReadDeadline(deadline)
	if err != nil {
		return "", 0, 0, 0, err
	}
	buffer := make([]byte, 1024)
	nRead, _, err := conn.ReadFrom(buffer)
	if err != nil {
		gLog.Println(LvERROR, "NAT detect error:", err)
		return "", 0, 0, 0, err
	}
	natRsp := NatDetectRsp{}
	err = json.Unmarshal(buffer[openP2PHeaderSize:nRead], &natRsp)
	hasPublicIP = 0
	hasUPNPorNATPMP = 0
	// testing for public ip
	if echoPort != 0 {
		for i := 0; i < 2; i++ {
			if i == 1 {
				// test upnp or nat-pmp
				nat, err := Discover()
				if err != nil || nat == nil {
					gLog.Println(LvDEBUG, "could not perform UPNP discover:", err)
					break
				}
				ext, err := nat.GetExternalAddress()
				if err != nil {
					gLog.Println(LvDEBUG, "could not perform UPNP external address:", err)
					break
				}
				log.Println("PublicIP:", ext)

				externalPort, err := nat.AddPortMapping("udp", echoPort, echoPort, "openp2p", 30)
				if err != nil {
					gLog.Println(LvDEBUG, "could not add udp UPNP port mapping", externalPort)
					break
				} else {
					nat.AddPortMapping("tcp", echoPort, echoPort, "openp2p", 604800)
				}
			}
			gLog.Printf(LvDEBUG, "public ip test start %s:%d", natRsp.IP, echoPort)
			conn, err := net.ListenUDP("udp", nil)
			if err != nil {
				break
			}
			defer conn.Close()
			dst, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", natRsp.IP, echoPort))
			if err != nil {
				break
			}
			conn.WriteTo([]byte("echo"), dst)
			buf := make([]byte, 1600)

			// wait for echo testing
			conn.SetReadDeadline(time.Now().Add(PublicIPEchoTimeout))
			_, _, err = conn.ReadFromUDP(buf)
			if err == nil {
				if i == 1 {
					gLog.Println(LvDEBUG, "UPNP or NAT-PMP:YES")
					hasUPNPorNATPMP = 1
				} else {
					gLog.Println(LvDEBUG, "public ip:YES")
					hasPublicIP = 1
				}
				break
			}
		}
	}
	return natRsp.IP, hasPublicIP, hasUPNPorNATPMP, natRsp.Port, nil
}

func getNATType(host string, udp1 int, udp2 int) (publicIP string, NATType int, hasIPvr int, hasUPNPorNATPMP int, err error) {
	// the random local port may be used by other.
	localPort := int(rand.Uint32()%15000 + 50000)
	echoPort := P2PNetworkInstance(nil).config.TCPPort
	go echo(echoPort)
	// _, natPort := natTCP(host, 27181, localPort)
	// gLog.Println(LvINFO, "nattcp:", natPort)
	// _, natPort = natTCP(host, 27180, localPort)
	// gLog.Println(LvINFO, "nattcp:", natPort)
	ip1, hasIPv4, hasUPNPorNATPMP, port1, err := natTest(host, udp1, localPort, echoPort)
	gLog.Printf(LvDEBUG, "local port:%d  nat port:%d", localPort, port1)
	if err != nil {
		return "", 0, hasIPv4, hasUPNPorNATPMP, err
	}
	if echoConn != nil {
		echoConn.Close()
		echoConn = nil
	}
	// if hasPublicIP == 1 || hasUPNPorNATPMP == 1 {
	// 	return ip1, NATNone, hasUPNPorNATPMP, nil
	// }
	_, _, _, port2, err := natTest(host, udp2, localPort, 0) // 2rd nat test not need testing publicip
	gLog.Printf(LvDEBUG, "local port:%d  nat port:%d", localPort, port2)
	if err != nil {
		return "", 0, hasIPv4, hasUPNPorNATPMP, err
	}
	natType := NATSymmetric
	if port1 == port2 {
		natType = NATCone
	}
	return ip1, natType, hasIPv4, hasUPNPorNATPMP, nil
}

func echo(echoPort int) {
	var err error
	echoConn, err = net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: echoPort})
	if err != nil {
		gLog.Println(LvERROR, "echo server listen error:", err)
		return
	}
	buf := make([]byte, 1600)
	// close outside for breaking the ReadFromUDP
	// wait 5s for echo testing
	echoConn.SetReadDeadline(time.Now().Add(time.Second * 30))
	n, addr, err := echoConn.ReadFromUDP(buf)
	if err != nil {
		return
	}
	echoConn.WriteToUDP(buf[0:n], addr)
}
