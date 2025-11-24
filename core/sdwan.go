package openp2p

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"reflect"
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
	tun           *optun
	tunErr        string
	sysRoute      sync.Map // ip:sdwanNode
	subnet        *net.IPNet
	gateway       net.IP
	virtualIP     *net.IPNet
	internalRoute *IPTree
}

func (s *p2pSDWAN) reset() {
	gLog.i("reset sdwan when network disconnected")
	// clear sysroute
	delRoutesByGateway(s.gateway.String())
	// clear internel route
	s.internalRoute = NewIPTree("")
	// clear p2papp
	for _, node := range gConf.getSDWAN().Nodes {
		gConf.delete(AppConfig{SrcPort: 0, PeerNode: node.Name})
		GNetwork.DeleteApp(AppConfig{SrcPort: 0, PeerNode: node.Name})
	}

	gConf.resetSDWAN()
}
func (s *p2pSDWAN) init() error {
	gConf.Network.previousIP = gConf.Network.publicIP
	if gConf.getSDWAN().Gateway == "" {
		gLog.d("sdwan init: not in sdwan clear all ")
	}
	if s.internalRoute == nil {
		s.internalRoute = NewIPTree("")
	}

	if gw, sn, err := net.ParseCIDR(gConf.getSDWAN().Gateway); err == nil { // preserve old gateway
		s.gateway = gw
		s.subnet = sn
	}

	for _, node := range gConf.getDelNodes() {
		gLog.d("sdwan init: deal deleted node: %s", node.Name)
		gLog.d("sdwan init: delRoute: %s, %s ", node.IP, s.gateway.String())
		// delRoute(node.IP, s.gateway.String()) // TODO: seems no need delelte each node
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
			gLog.d("sdwan init: resource delRoute: %s, %s ", ipnet.String(), s.gateway.String())
		}
	}
	for _, node := range gConf.getAddNodes() {
		gLog.d("sdwan init: deal add node: %s", node.Name)
		ipNet := &net.IPNet{
			IP:   net.ParseIP(node.IP),
			Mask: s.subnet.Mask,
		}
		if node.Name == gConf.Network.Node {
			s.virtualIP = ipNet
			gLog.i("sdwan init: start tun %s", ipNet.String())
			err := s.StartTun()
			if err != nil {
				gLog.e("sdwan init: start tun error:%s", err)
				return err
			}
			gLog.i("sdwan init: start tun ok")
			allowTunForward()
			gLog.d("sdwan init: addRoute %s %s %s", s.subnet.String(), s.gateway.String(), s.tun.tunName)
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
		if node.Name == gConf.Network.Node { // not deal resource itself
			continue
		}
		if len(node.Resource) > 0 {
			gLog.i("sdwan init: deal add node: %s resource: %s", node.Name, node.Resource)
			arr := strings.Split(node.Resource, ",")
			for _, r := range arr {
				// add internal route
				_, ipnet, err := net.ParseCIDR(r)
				if err != nil {
					fmt.Println("sdwan init: Error parsing CIDR:", err)
					continue
				}
				if ipnet.Contains(net.ParseIP(gConf.Network.localIP)) { // local ip and resource in the same lan
					gLog.d("sdwan init: local ip %s in this resource %s, ignore", gConf.Network.localIP, ipnet.IP.String())
					continue
				}
				// local net could access this single ip
				if ipnet.Mask[0] == 255 && ipnet.Mask[1] == 255 && ipnet.Mask[2] == 255 && ipnet.Mask[3] == 255 {
					gLog.d("sdwan init: ping %s start", ipnet.IP.String())
					if _, err := Ping(ipnet.IP.String()); err == nil {
						gLog.d("sdwan init: ping %s ok, ignore this resource", ipnet.IP.String())
						continue
					}
					gLog.d("sdwan init: ping %s failed", ipnet.IP.String())
				}
				minIP := ipnet.IP
				maxIP := make(net.IP, len(minIP))
				copy(maxIP, minIP)
				for i := range minIP {
					maxIP[i] = minIP[i] | ^ipnet.Mask[i]
				}
				s.internalRoute.Add(minIP.String(), maxIP.String(), &sdwanNode{name: node.Name, id: NodeNameToID(node.Name)})
				// add sys route
				gLog.d("sdwan init: addRoute %s %s %s", ipnet.String(), s.gateway.String(), s.tun.tunName)
				addRoute(ipnet.String(), s.gateway.String(), s.tun.tunName)
			}
		}
	}
	gConf.retryAllMemApp()
	gLog.i("sdwan init ok")
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
	gLog.d("sdwan readNodeLoop start")
	defer gLog.d("sdwan readNodeLoop end")
	writeBuff := make([][]byte, 1)
	for {
		nd := GNetwork.ReadNode(time.Second * 10) // TODO: read multi packet
		if nd == nil {
			gLog.dev("waiting for node data")
			continue
		}
		head := PacketHeader{}
		parseHeader(nd, &head)
		gLog.dev("write tun dst ip=%s,len=%d", net.IP{byte(head.dst >> 24), byte(head.dst >> 16), byte(head.dst >> 8), byte(head.dst)}.String(), len(nd))
		if PIHeaderSize == 0 {
			writeBuff[0] = nd
		} else {
			writeBuff[0] = make([]byte, PIHeaderSize+len(nd))
			copy(writeBuff[0][PIHeaderSize:], nd)
		}

		len, err := s.tun.Write(writeBuff, PIHeaderSize)
		if err != nil {
			gLog.d("write tun dst ip=%s,len=%d,error:%s", net.IP{byte(head.dst >> 24), byte(head.dst >> 16), byte(head.dst >> 8), byte(head.dst)}.String(), len, err)
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
			gLog.dev("multicast ip=%s", net.IP{byte(head.dst >> 24), byte(head.dst >> 16), byte(head.dst >> 8), byte(head.dst)}.String())
			GNetwork.WriteBroadcast(p)
			return
		}
		gLog.dev("internalRoute not found ip:%s", net.IP{byte(head.dst >> 24), byte(head.dst >> 16), byte(head.dst >> 8), byte(head.dst)}.String())
		return
	} else {
		node = v.(*sdwanNode)
	}

	err := GNetwork.WriteNode(node.id, p)
	if err != nil {
		gLog.dev("write packet to %s fail: %s", node.name, err)
	}
}

func (s *p2pSDWAN) readTunLoop() {
	gLog.d("sdwan readTunLoop start")
	defer gLog.d("sdwan readTunLoop end")
	readBuff := make([][]byte, ReadTunBuffNum)
	for i := 0; i < ReadTunBuffNum; i++ {
		readBuff[i] = make([]byte, ReadTunBuffSize+PIHeaderSize)
	}
	readBuffSize := make([]int, ReadTunBuffNum)
	ih := PacketHeader{}
	for {
		n, err := s.tun.Read(readBuff, readBuffSize, PIHeaderSize)
		if err != nil {
			gLog.e("read tun fail: %s", err)
			return
		}
		for i := 0; i < n; i++ {
			if readBuffSize[i] > ReadTunBuffSize {
				gLog.e("read tun overflow: len=%d", readBuffSize[i])
				continue
			}
			parseHeader(readBuff[i][PIHeaderSize:readBuffSize[i]+PIHeaderSize], &ih)
			gLog.dev("read tun dst ip=%s,len=%d", net.IP{byte(ih.dst >> 24), byte(ih.dst >> 16), byte(ih.dst >> 8), byte(ih.dst)}.String(), readBuffSize[0])
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
			gLog.e("open tun fail:%v", err)
			s.tunErr = err.Error()
			return err
		}
		s.tun = tun
		s.tunErr = ""
		go s.readTunLoop()
		go s.readNodeLoop() // multi-thread read will cause packets out of order, resulting in slower speeds
	}
	err := setTunAddr(s.tun.tunName, s.virtualIP.String(), sdwan.Gateway, s.tun.dev)
	if err != nil {
		gLog.e("setTunAddr error:%s,%s,%s,%s", err, s.tun.tunName, s.virtualIP.String(), sdwan.Gateway)
		return err
	}
	return nil
}

func handleSDWAN(subType uint16, msg []byte) error {
	gLog.d("handle sdwan msg type:%d", subType)
	var err error
	switch subType {
	case MsgSDWANInfoRsp:
		rsp := SDWANInfo{}
		if err = json.Unmarshal(msg[openP2PHeaderSize:], &rsp); err != nil {
			return ErrMsgFormat
		}
		gLog.i("sdwan init:%s", prettyJson(rsp))
		// GNetwork.sdwan.detail = &rsp
		if gConf.Network.previousIP != gConf.Network.publicIP || gConf.getSDWAN().CentralNode != rsp.CentralNode {
			GNetwork.sdwan.reset()
			preAndroidSDWANConfig = "" // let androind app reset vpnservice
		}
		gConf.setSDWAN(rsp)
		if runtime.GOOS == "android" {
			if !compareResources(preAndroidSDWANConfig, string(msg[openP2PHeaderSize:])) { // when config change, notify android app
				select {
				case AndroidSDWANConfig <- msg[openP2PHeaderSize:]:
				default:
				}
				preAndroidSDWANConfig = string(msg[openP2PHeaderSize:])
			}
		}
		err = GNetwork.sdwan.init()
		if err != nil {
			gLog.e("sdwan init fail: %s", err)
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

// for android vpnservice
func compareResources(json1, json2 string) bool {
	var net1, net2 SDWANInfo
	if err := json.Unmarshal([]byte(json1), &net1); err != nil {
		fmt.Println("Error parsing json1:", err)
		fmt.Println("Error parsing json1:", string(json1))
		return false
	}
	if err := json.Unmarshal([]byte(json2), &net2); err != nil {
		fmt.Println("Error parsing json2:", err)
		fmt.Println("Error parsing json1:", string(json2))
		return false
	}

	// 获取所有资源并比较
	resources1 := getResources(net1)
	resources2 := getResources(net2)
	return reflect.DeepEqual(resources1, resources2)
}

func getResources(network SDWANInfo) []string {
	var resources []string
	for _, node := range network.Nodes {
		if node.Resource != "" {
			resources = append(resources, node.Resource)
		}
	}
	return resources
}
