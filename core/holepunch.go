package openp2p

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"time"
)

func handshakeC2C(t *P2PTunnel) (err error) {
	gLog.Printf(LvDEBUG, "handshakeC2C %s:%d:%d to %s:%d", gConf.Network.Node, t.coneLocalPort, t.coneNatPort, t.config.peerIP, t.config.peerConeNatPort)
	defer gLog.Printf(LvDEBUG, "handshakeC2C end")
	conn, err := net.ListenUDP("udp", t.localHoleAddr)
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = UDPWrite(conn, t.remoteHoleAddr, MsgP2P, MsgPunchHandshake, P2PHandshakeReq{ID: t.id})
	if err != nil {
		gLog.Println(LvDEBUG, "handshakeC2C write MsgPunchHandshake error:", err)
		return err
	}
	ra, head, buff, _, err := UDPRead(conn, HandshakeTimeout)
	if err != nil {
		gLog.Println(LvDEBUG, "handshakeC2C read MsgPunchHandshake error:", err)
		return err
	}
	t.remoteHoleAddr, _ = net.ResolveUDPAddr("udp", ra.String())
	var tunnelID uint64
	if len(buff) > openP2PHeaderSize {
		req := P2PHandshakeReq{}
		if err := json.Unmarshal(buff[openP2PHeaderSize:openP2PHeaderSize+int(head.DataLen)], &req); err == nil {
			tunnelID = req.ID
		}
	} else { // compatible with old version
		tunnelID = t.id
	}
	if head.MainType == MsgP2P && head.SubType == MsgPunchHandshake && tunnelID == t.id {
		gLog.Printf(LvDEBUG, "read tunnelid:%d handshake ", t.id)
		UDPWrite(conn, t.remoteHoleAddr, MsgP2P, MsgPunchHandshakeAck, P2PHandshakeReq{ID: t.id})
		_, head, _, _, err = UDPRead(conn, HandshakeTimeout)
		if err != nil {
			gLog.Println(LvDEBUG, "handshakeC2C write MsgPunchHandshakeAck error:", err)
			return err
		}
	}
	if head.MainType == MsgP2P && head.SubType == MsgPunchHandshakeAck && tunnelID == t.id {
		gLog.Printf(LvDEBUG, "read tunnelID:%d handshake ack ", t.id)
		_, err = UDPWrite(conn, t.remoteHoleAddr, MsgP2P, MsgPunchHandshakeAck, P2PHandshakeReq{ID: t.id})
		if err != nil {
			gLog.Println(LvDEBUG, "handshakeC2C write MsgPunchHandshakeAck error:", err)
			return err
		}
	}
	gLog.Printf(LvINFO, "handshakeC2C ok")
	return nil
}

func handshakeC2S(t *P2PTunnel) error {
	gLog.Printf(LvDEBUG, "tid:%d handshakeC2S start", t.id)
	defer gLog.Printf(LvDEBUG, "tid:%d handshakeC2S end", t.id)
	if !buildTunnelMtx.TryLock() {
		// time.Sleep(time.Second * 3)
		return ErrBuildTunnelBusy
	}
	defer buildTunnelMtx.Unlock()
	startTime := time.Now()
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	randPorts := r.Perm(65532)
	conn, err := net.ListenUDP("udp", t.localHoleAddr)
	if err != nil {
		return err
	}
	defer conn.Close()

	go func() error {
		gLog.Printf(LvDEBUG, "tid:%d send symmetric handshake to %s from %d:%d start", t.id, t.config.peerIP, t.coneLocalPort, t.coneNatPort)
		for i := 0; i < SymmetricHandshakeNum; i++ {
			// time.Sleep(SymmetricHandshakeInterval)
			dst, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", t.config.peerIP, randPorts[i]+2))
			if err != nil {
				return err
			}
			_, err = UDPWrite(conn, dst, MsgP2P, MsgPunchHandshake, P2PHandshakeReq{ID: t.id})
			if err != nil {
				gLog.Printf(LvDEBUG, "tid:%d handshakeC2S write MsgPunchHandshake error:%s", t.id, err)
				return err
			}
		}
		gLog.Printf(LvDEBUG, "tid:%d send symmetric handshake end", t.id)
		return nil
	}()
	err = conn.SetReadDeadline(time.Now().Add(HandshakeTimeout))
	if err != nil {
		gLog.Println(LvERROR, "tid:%d SymmetricHandshakeAckTimeout SetReadDeadline error", t.id)
		return err
	}
	// read response of the punching hole ok port
	buff := make([]byte, 1024)
	_, dst, err := conn.ReadFrom(buff)
	if err != nil {
		gLog.Println(LvERROR, "tid:%d handshakeC2S wait timeout", t.id)
		return err
	}
	head := &openP2PHeader{}
	err = binary.Read(bytes.NewReader(buff[:openP2PHeaderSize]), binary.LittleEndian, head)
	if err != nil {
		gLog.Printf(LvERROR, "tid:%d parse p2pheader error:%s", t.id, err)
		return err
	}
	t.remoteHoleAddr, _ = net.ResolveUDPAddr("udp", dst.String())
	var tunnelID uint64
	if len(buff) > openP2PHeaderSize {
		req := P2PHandshakeReq{}
		if err := json.Unmarshal(buff[openP2PHeaderSize:openP2PHeaderSize+int(head.DataLen)], &req); err == nil {
			tunnelID = req.ID
		}
	} else { // compatible with old version
		tunnelID = t.id
	}
	if head.MainType == MsgP2P && head.SubType == MsgPunchHandshake && tunnelID == t.id {
		gLog.Printf(LvDEBUG, "tid:%d handshakeC2S read handshake ", t.id)
		UDPWrite(conn, t.remoteHoleAddr, MsgP2P, MsgPunchHandshakeAck, P2PHandshakeReq{ID: t.id})
		for {
			_, head, buff, _, err = UDPRead(conn, HandshakeTimeout)
			if err != nil {
				gLog.Printf(LvDEBUG, "tid:%d handshakeC2S handshake error", t.id)
				return err
			}
			var tunnelID uint64
			if len(buff) > openP2PHeaderSize {
				req := P2PHandshakeReq{}
				if err := json.Unmarshal(buff[openP2PHeaderSize:openP2PHeaderSize+int(head.DataLen)], &req); err == nil {
					tunnelID = req.ID
				}
			} else { // compatible with old version
				tunnelID = t.id
			}
			// waiting ack
			if head.MainType == MsgP2P && head.SubType == MsgPunchHandshakeAck && tunnelID == t.id {
				break
			}
		}
	}
	if head.MainType == MsgP2P && head.SubType == MsgPunchHandshakeAck {
		gLog.Printf(LvDEBUG, "tid:%d handshakeC2S read handshake ack %s", t.id, t.remoteHoleAddr.String())
		_, err = UDPWrite(conn, t.remoteHoleAddr, MsgP2P, MsgPunchHandshakeAck, P2PHandshakeReq{ID: t.id})
		return err
	} else {
		gLog.Printf(LvDEBUG, "tid:%d handshakeS2C read msg but not MsgPunchHandshakeAck", t.id)
	}
	gLog.Printf(LvINFO, "tid:%d handshakeC2S ok. cost %d ms", t.id, time.Since(startTime)/time.Millisecond)
	return nil
}

func handshakeS2C(t *P2PTunnel) error {
	gLog.Printf(LvDEBUG, "tid:%d handshakeS2C start", t.id)
	defer gLog.Printf(LvDEBUG, "tid:%d handshakeS2C end", t.id)
	if !buildTunnelMtx.TryLock() {
		// time.Sleep(time.Second * 3)
		return ErrBuildTunnelBusy
	}
	defer buildTunnelMtx.Unlock()
	startTime := time.Now()
	gotCh := make(chan *net.UDPAddr, 5)
	// sequencely udp send handshake, do not parallel send
	gLog.Printf(LvDEBUG, "tid:%d send symmetric handshake to %s:%d start", t.id, t.config.peerIP, t.config.peerConeNatPort)
	gotIt := false
	for i := 0; i < SymmetricHandshakeNum; i++ {
		// time.Sleep(SymmetricHandshakeInterval)
		go func(t *P2PTunnel) error {
			conn, err := net.ListenUDP("udp", nil) // TODO: system allocated port really random?
			if err != nil {
				gLog.Printf(LvDEBUG, "tid:%d listen error", t.id)
				return err
			}
			defer conn.Close()
			UDPWrite(conn, t.remoteHoleAddr, MsgP2P, MsgPunchHandshake, P2PHandshakeReq{ID: t.id})
			_, head, buff, _, err := UDPRead(conn, HandshakeTimeout)
			if err != nil {
				// gLog.Println(LevelDEBUG, "one of the handshake error:", err)
				return err
			}
			if gotIt {
				return nil
			}
			var tunnelID uint64
			if len(buff) >= openP2PHeaderSize+8 {
				req := P2PHandshakeReq{}
				if err := json.Unmarshal(buff[openP2PHeaderSize:openP2PHeaderSize+int(head.DataLen)], &req); err == nil {
					tunnelID = req.ID
				}
			} else { // compatible with old version
				tunnelID = t.id
			}

			if head.MainType == MsgP2P && head.SubType == MsgPunchHandshake && tunnelID == t.id {
				gLog.Printf(LvDEBUG, "tid:%d handshakeS2C read handshake ", t.id)
				UDPWrite(conn, t.remoteHoleAddr, MsgP2P, MsgPunchHandshakeAck, P2PHandshakeReq{ID: t.id})
				// may read several MsgPunchHandshake
				for {
					_, head, buff, _, err = UDPRead(conn, HandshakeTimeout)
					if err != nil {
						gLog.Println(LvDEBUG, "tid:%d handshakeS2C handshake error", t.id)
						return err
					}
					if len(buff) > openP2PHeaderSize {
						req := P2PHandshakeReq{}
						if err := json.Unmarshal(buff[openP2PHeaderSize:openP2PHeaderSize+int(head.DataLen)], &req); err == nil {
							tunnelID = req.ID
						}
					} else { // compatible with old version
						tunnelID = t.id
					}
					if head.MainType == MsgP2P && head.SubType == MsgPunchHandshakeAck && tunnelID == t.id {
						break
					} else {
						gLog.Println(LvDEBUG, "tid:%d handshakeS2C read msg but not MsgPunchHandshakeAck", t.id)
					}
				}
			}
			if head.MainType == MsgP2P && head.SubType == MsgPunchHandshakeAck {
				gLog.Printf(LvDEBUG, "tid:%d handshakeS2C read handshake ack %s", t.id, conn.LocalAddr().String())
				UDPWrite(conn, t.remoteHoleAddr, MsgP2P, MsgPunchHandshakeAck, P2PHandshakeReq{ID: t.id})
				gotIt = true
				la, _ := net.ResolveUDPAddr("udp", conn.LocalAddr().String())
				gotCh <- la
				return nil
			} else {
				gLog.Printf(LvDEBUG, "tid:%d handshakeS2C read msg but not MsgPunchHandshakeAck", t.id)
			}
			return nil
		}(t)
	}
	gLog.Printf(LvDEBUG, "tid:%d send symmetric handshake end", t.id)
	if compareVersion(t.config.peerVersion, SymmetricSimultaneouslySendVersion) < 0 { // compatible with old client
		gLog.Printf(LvDEBUG, "tid:%d handshakeS2C ready, notify peer connect", t.id)
		GNetwork.push(t.config.PeerNode, MsgPushHandshakeStart, TunnelMsg{ID: t.id})
	}

	select {
	case <-time.After(HandshakeTimeout):
		return fmt.Errorf("tid:%d wait handshake timeout", t.id)
	case la := <-gotCh:
		t.localHoleAddr = la
		gLog.Printf(LvINFO, "tid:%d handshakeS2C ok. cost %dms", t.id, time.Since(startTime)/time.Millisecond)
	}
	return nil
}
