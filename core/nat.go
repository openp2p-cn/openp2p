package openp2p

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"strconv"
	"strings"
	"time"

	reuse "github.com/openp2p-cn/go-reuseport"
)

func natDetectTCP(serverHost string, serverPort int, lp int) (publicIP string, publicPort int, localPort int, err error) {
	gLog.dev("natDetectTCP start")
	defer gLog.dev("natDetectTCP end")
	conn, err := reuse.DialTimeout("tcp4", fmt.Sprintf("0.0.0.0:%d", lp), fmt.Sprintf("%s:%d", serverHost, serverPort), NatDetectTimeout)
	if err != nil {
		err = fmt.Errorf("dial tcp4 %s:%d error: %w", serverHost, serverPort, err)
		return
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.TCPAddr)
	localPort = localAddr.Port

	if _, err = conn.Write([]byte("1")); err != nil {
		err = fmt.Errorf("write error: %w", err)
		return
	}

	b := make([]byte, 1000)
	conn.SetReadDeadline(time.Now().Add(NatDetectTimeout))
	n, err := conn.Read(b)
	if err != nil {
		err = fmt.Errorf("read error: %w", err)
		return
	}

	response := strings.Split(string(b[:n]), ":")
	if len(response) < 2 {
		err = fmt.Errorf("invalid response format: %s", string(b[:n]))
		return
	}

	publicIP = response[0]
	port, err := strconv.Atoi(response[1])
	if err != nil {
		err = fmt.Errorf("invalid port format: %w", err)
		return
	}
	publicPort = port

	return
}

func natDetectUDP(serverHost string, serverPort int, localPort int) (publicIP string, publicPort int, err error) {
	gLog.dev("natDetectUDP start")
	defer gLog.dev("natDetectUDP end")
	conn, err := net.ListenPacket("udp", fmt.Sprintf(":%d", localPort))
	if err != nil {
		gLog.e("natDetectUDP listen udp error:%s", err)
		return "", 0, err
	}
	defer conn.Close()

	dst, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", serverHost, serverPort))
	if err != nil {
		return "", 0, err
	}

	// The connection can write data to the desired address.
	msg, err := newMessage(MsgNATDetect, MsgNAT, nil)
	_, err = conn.WriteTo(msg, dst)
	if err != nil {
		return "", 0, err
	}
	deadline := time.Now().Add(NatDetectTimeout)
	err = conn.SetReadDeadline(deadline)
	if err != nil {
		return "", 0, err
	}
	buffer := make([]byte, 1024)
	nRead, _, err := conn.ReadFrom(buffer)
	if err != nil {
		gLog.e("NAT detect error:%s", err)
		return "", 0, err
	}
	natRsp := NatDetectRsp{}
	json.Unmarshal(buffer[openP2PHeaderSize:nRead], &natRsp)

	return natRsp.IP, natRsp.Port, nil
}

func getNATType(host string, detectPort1 int, detectPort2 int) (publicIP string, NATType int, err error) {
	setUPNP(gConf.Network.PublicIPPort)
	// the random local port may be used by other.
	localPort := int(rand.Uint32()%15000 + 50000)

	ip1, port1, err := natDetectUDP(host, detectPort1, localPort)
	if err != nil {
		// udp block try tcp
		gLog.w("udp block, try tcp nat detect")
		if ip1, port1, _, err = natDetectTCP(host, detectPort1, localPort); err != nil {
			return "", 0, err
		}
	}
	_, port2, err := natDetectUDP(host, detectPort2, localPort) // 2rd nat test not need testing publicip
	if err != nil {
		gLog.w("udp block, try tcp nat detect")
		if _, port2, _, err = natDetectTCP(host, detectPort2, localPort); err != nil {
			return "", 0, err
		}
	}
	gLog.d("local port:%d  nat port:%d", localPort, port2)
	natType := NATSymmetric
	if port1 == port2 {
		natType = NATCone
	}
	return ip1, natType, nil
}

func publicIPTest(publicIP string, echoPort int) (hasPublicIP int, hasUPNPorNATPMP int) {
	if publicIP == "" || echoPort == 0 {
		return
	}
	var echoConn *net.UDPConn
	gLog.d("echo server start")
	var err error
	echoConn, err = net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: echoPort})
	if err != nil { // listen error
		gLog.e("echo server listen error:%s", err)
		return
	}
	defer echoConn.Close()
	// testing for public ip
	for i := 0; i < 2; i++ {
		if i == 1 {
			// test upnp or nat-pmp
			gLog.d("upnp test start")
			// 7 days for udp connection
			// 7 days for tcp connection
			setUPNP(echoPort)
		}
		gLog.d("public ip test start %s:%d", publicIP, echoPort)
		conn, err := net.ListenUDP("udp", nil)
		if err != nil {
			break
		}
		defer conn.Close()
		dst, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", gConf.Network.ServerIP, gConf.Network.ServerPort))
		if err != nil {
			break
		}

		// The connection can write data to the desired address.
		msg, _ := newMessage(MsgNATDetect, MsgPublicIP, NatDetectReq{EchoPort: echoPort})
		_, err = conn.WriteTo(msg, dst)
		if err != nil {
			continue
		}
		buf := make([]byte, 1600)

		// wait for echo testing
		echoConn.SetReadDeadline(time.Now().Add(PublicIPEchoTimeout))
		nRead, _, err := echoConn.ReadFromUDP(buf)
		if err != nil {
			gLog.d("publicIPTest echoConn read timeout:%s", err)
			continue
		}
		natRsp := NatDetectRsp{}
		err = json.Unmarshal(buf[openP2PHeaderSize:nRead], &natRsp)
		if err != nil {
			gLog.d("publicIPTest Unmarshal error:%s", err)
			continue
		}
		if natRsp.Port == echoPort {
			if i == 1 {
				gLog.d("UPNP or NAT-PMP:YES")
				hasUPNPorNATPMP = 1
			} else {
				gLog.d("public ip:YES")
				hasPublicIP = 1
			}
			break
		}
	}
	return
}

func setUPNP(echoPort int) {
	nat, err := Discover()
	if err != nil || nat == nil {
		gLog.d("could not perform UPNP discover:%s", err)
		return
	}
	ext, err := nat.GetExternalAddress()
	if err != nil {
		gLog.d("could not perform UPNP external address:%s", err)
		return
	}
	gLog.i("PublicIP:%v", ext)

	externalPort, err := nat.AddPortMapping("udp", echoPort, echoPort, "openp2p", 604800)
	if err != nil {
		gLog.d("could not add udp UPNP port mapping %d", externalPort)
		return
	} else {
		nat.AddPortMapping("tcp", echoPort, echoPort, "openp2p", 604800)
	}
}
