package openp2p

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"runtime"
	"strings"
	"sync"
	"time"
)

type PacketHeader struct {
	version int
	// src     uint32
	// prot    uint8
	protocol byte
	dst      uint32
	port     uint16
}

func parseHeader(b []byte, h *PacketHeader) error {
	if len(b) < 20 {
		return fmt.Errorf("small packet")
	}
	h.version = int(b[0] >> 4)
	h.protocol = byte(b[9])
	if h.version == 4 {
		h.dst = binary.BigEndian.Uint32(b[16:20])
	} else if h.version != 6 {
		return fmt.Errorf("unknown version in ip header:%d", h.version)
	}
	if h.protocol == 6 || h.protocol == 17 { // TCP or UDP
		h.port = binary.BigEndian.Uint16(b[22:24])
	}
	return nil
}

type sdwanNode struct {
	name string
	id   uint64
}

type p2pSDWAN struct {
	nodeName      string
	tun           *optun
	sysRoute      sync.Map // ip:sdwanNode
	subnet        *net.IPNet
	gateway       net.IP
	virtualIP     *net.IPNet
	internalRoute *IPTree
}

func (s *p2pSDWAN) init(name string) error {
	if gConf.getSDWAN().Gateway == "" {
		gLog.Println(LvDEBUG, "not in sdwan clear all ")
	}
	if s.internalRoute == nil {
		s.internalRoute = NewIPTree("")
	}

	s.nodeName = name
	s.gateway, s.subnet, _ = net.ParseCIDR(gConf.getSDWAN().Gateway)
	for _, node := range gConf.getDelNodes() {
		gLog.Println(LvDEBUG, "deal deleted node: ", node.Name)
		delRoute(node.IP, s.gateway.String())
		s.internalRoute.Del(node.IP, node.IP)
		ipNum, _ := inetAtoN(node.IP)
		s.sysRoute.Delete(ipNum)
		gConf.delete(AppConfig{SrcPort: 0, PeerNode: node.Name})
		GNetwork.DeleteApp(AppConfig{SrcPort: 0, PeerNode: node.Name})
		arr := strings.Split(node.Resource, ",")
		for _, r := range arr {
			_, ipnet, err := net.ParseCIDR(r)
			if err != nil {
				// fmt.Println("Error parsing CIDR:", err)
				continue
			}
			if ipnet.Contains(net.ParseIP(gConf.Network.localIP)) { // local ip and resource in the same lan
				continue
			}
			minIP := ipnet.IP
			maxIP := make(net.IP, len(minIP))
			copy(maxIP, minIP)
			for i := range minIP {
				maxIP[i] = minIP[i] | ^ipnet.Mask[i]
			}
			s.internalRoute.Del(minIP.String(), maxIP.String())
			delRoute(ipnet.String(), s.gateway.String())
		}
	}
	for _, node := range gConf.getAddNodes() {
		gLog.Println(LvDEBUG, "deal add node: ", node.Name)
		ipNet := &net.IPNet{
			IP:   net.ParseIP(node.IP),
			Mask: s.subnet.Mask,
		}
		if node.Name == s.nodeName {
			s.virtualIP = ipNet
			gLog.Println(LvINFO, "start tun ", ipNet.String())
			err := s.StartTun()
			if err != nil {
				gLog.Println(LvERROR, "start tun error:", err)
				return err
			}
			gLog.Println(LvINFO, "start tun ok")
			allowTunForward()
			addRoute(s.subnet.String(), s.gateway.String(), s.tun.tunName)
			// addRoute("255.255.255.255/32", s.gateway.String(), s.tun.tunName) // for broadcast
			// addRoute("224.0.0.0/4", s.gateway.String(), s.tun.tunName)        // for multicast
			initSNATRule(s.subnet.String()) // for network resource
			continue
		}
		ip, err := inetAtoN(ipNet.String())
		if err != nil {
			return err
		}
		s.sysRoute.Store(ip, &sdwanNode{name: node.Name, id: NodeNameToID(node.Name)})
		s.internalRoute.AddIntIP(ip, ip, &sdwanNode{name: node.Name, id: NodeNameToID(node.Name)})
	}
	for _, node := range gConf.getAddNodes() {
		if node.Name == s.nodeName { // not deal resource itself
			continue
		}
		if len(node.Resource) > 0 {
			gLog.Printf(LvINFO, "deal add node: %s resource: %s", node.Name, node.Resource)
			arr := strings.Split(node.Resource, ",")
			for _, r := range arr {
				// add internal route
				_, ipnet, err := net.ParseCIDR(r)
				if err != nil {
					fmt.Println("Error parsing CIDR:", err)
					continue
				}
				if ipnet.Contains(net.ParseIP(gConf.Network.localIP)) { // local ip and resource in the same lan
					continue
				}
				minIP := ipnet.IP
				maxIP := make(net.IP, len(minIP))
				copy(maxIP, minIP)
				for i := range minIP {
					maxIP[i] = minIP[i] | ^ipnet.Mask[i]
				}
				s.internalRoute.Add(minIP.String(), maxIP.String(), &sdwanNode{name: node.Name, id: NodeNameToID(node.Name)})
				// add sys route
				addRoute(ipnet.String(), s.gateway.String(), s.tun.tunName)
			}
		}
	}
	gConf.retryAllMemApp()
	gLog.Printf(LvINFO, "sdwan init ok")
	return nil
}

func (s *p2pSDWAN) run() {
	s.sysRoute.Range(func(key, value interface{}) bool {
		node := value.(*sdwanNode)
		GNetwork.ConnectNode(node.name)
		return true
	})
}

func (s *p2pSDWAN) readNodeLoop() {
	gLog.Printf(LvDEBUG, "sdwan readNodeLoop start")
	defer gLog.Printf(LvDEBUG, "sdwan readNodeLoop end")
	writeBuff := make([][]byte, 1)
	for {
		nd := GNetwork.ReadNode(time.Second * 10) // TODO: read multi packet
		if nd == nil {
			gLog.Printf(LvDev, "waiting for node data")
			continue
		}
		head := PacketHeader{}
		parseHeader(nd.Data, &head)
		gLog.Printf(LvDev, "write tun dst ip=%s,len=%d", net.IP{byte(head.dst >> 24), byte(head.dst >> 16), byte(head.dst >> 8), byte(head.dst)}.String(), len(nd.Data))
		if PIHeaderSize == 0 {
			writeBuff[0] = nd.Data
		} else {
			writeBuff[0] = make([]byte, PIHeaderSize+len(nd.Data))
			copy(writeBuff[0][PIHeaderSize:], nd.Data)
		}

		len, err := s.tun.Write(writeBuff, PIHeaderSize)
		if err != nil {
			gLog.Printf(LvDEBUG, "write tun dst ip=%s,len=%d,error:%s", net.IP{byte(head.dst >> 24), byte(head.dst >> 16), byte(head.dst >> 8), byte(head.dst)}.String(), len, err)
		}
	}
}

func isBroadcastOrMulticast(ipUint32 uint32, subnet *net.IPNet) bool {
	// return ipUint32 == 0xffffffff || (byte(ipUint32) == 0xff) || (ipUint32>>28 == 0xe)
	return ipUint32 == 0xffffffff || (ipUint32>>28 == 0xe) // 225.255.255.255/32, 224.0.0.0/4
}

func (s *p2pSDWAN) routeTunPacket(p []byte, head *PacketHeader) {
	var node *sdwanNode
	// v, ok := s.routes.Load(ih.dst)
	v, ok := s.internalRoute.Load(head.dst)
	if !ok || v == nil {
		if isBroadcastOrMulticast(head.dst, s.subnet) {
			gLog.Printf(LvDev, "multicast ip=%s", net.IP{byte(head.dst >> 24), byte(head.dst >> 16), byte(head.dst >> 8), byte(head.dst)}.String())
			GNetwork.WriteBroadcast(p)
		}
		return
	} else {
		node = v.(*sdwanNode)
	}

	err := GNetwork.WriteNode(node.id, p)
	if err != nil {
		gLog.Printf(LvDev, "write packet to %s fail: %s", node.name, err)
	}
}

func (s *p2pSDWAN) readTunLoop() {
	gLog.Printf(LvDEBUG, "sdwan readTunLoop start")
	defer gLog.Printf(LvDEBUG, "sdwan readTunLoop end")
	readBuff := make([][]byte, ReadTunBuffNum)
	for i := 0; i < ReadTunBuffNum; i++ {
		readBuff[i] = make([]byte, ReadTunBuffSize+PIHeaderSize)
	}
	readBuffSize := make([]int, ReadTunBuffNum)
	ih := PacketHeader{}
	for {
		n, err := s.tun.Read(readBuff, readBuffSize, PIHeaderSize)
		if err != nil {
			gLog.Printf(LvERROR, "read tun fail: ", err)
			return
		}
		for i := 0; i < n; i++ {
			if readBuffSize[i] > ReadTunBuffSize {
				gLog.Printf(LvERROR, "read tun overflow: len=", readBuffSize[i])
				continue
			}
			parseHeader(readBuff[i][PIHeaderSize:readBuffSize[i]+PIHeaderSize], &ih)
			gLog.Printf(LvDev, "read tun dst ip=%s,len=%d", net.IP{byte(ih.dst >> 24), byte(ih.dst >> 16), byte(ih.dst >> 8), byte(ih.dst)}.String(), readBuffSize[0])
			s.routeTunPacket(readBuff[i][PIHeaderSize:readBuffSize[i]+PIHeaderSize], &ih)
		}
	}
}

func (s *p2pSDWAN) StartTun() error {
	sdwan := gConf.getSDWAN()
	if s.tun == nil {
		tun := &optun{}
		err := tun.Start(s.virtualIP.String(), &sdwan)
		if err != nil {
			gLog.Println(LvERROR, "open tun fail:", err)
			return err
		}
		s.tun = tun
		go s.readTunLoop()
		go s.readNodeLoop() // multi-thread read will cause packets out of order, resulting in slower speeds
	}
	err := setTunAddr(s.tun.tunName, s.virtualIP.String(), sdwan.Gateway, s.tun.dev)
	if err != nil {
		gLog.Printf(LvERROR, "setTunAddr error:%s,%s,%s,%s", err, s.tun.tunName, s.virtualIP.String(), sdwan.Gateway)
		return err
	}
	return nil
}

func handleSDWAN(subType uint16, msg []byte) error {
	gLog.Printf(LvDEBUG, "handle sdwan msg type:%d", subType)
	var err error
	switch subType {
	case MsgSDWANInfoRsp:
		rsp := SDWANInfo{}
		if err = json.Unmarshal(msg[openP2PHeaderSize:], &rsp); err != nil {
			return ErrMsgFormat
		}
		gLog.Println(LvINFO, "sdwan init:", prettyJson(rsp))
		if runtime.GOOS == "android" {
			AndroidSDWANConfig <- msg[openP2PHeaderSize:]
		}
		// GNetwork.sdwan.detail = &rsp
		gConf.setSDWAN(rsp)
		err = GNetwork.sdwan.init(gConf.Network.Node)
		if err != nil {
			gLog.Println(LvERROR, "sdwan init fail: ", err)
			if GNetwork.sdwan.tun != nil {
				GNetwork.sdwan.tun.Stop()
				GNetwork.sdwan.tun = nil
				return err
			}
		}
		go GNetwork.sdwan.run()
	default:
	}
	return err
}
