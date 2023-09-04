package openp2p

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/rand"
	"net"
	"time"
)

func handshakeC2C(t *P2PTunnel) (err error) {
	gLog.Printf(LvDEBUG, "handshakeC2C %s:%d:%d to %s:%d", t.pn.config.Node, t.coneLocalPort, t.coneNatPort, t.config.peerIP, t.config.peerConeNatPort)
	defer gLog.Printf(LvDEBUG, "handshakeC2C end")
	conn, err := net.ListenUDP("udp", t.la)
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = UDPWrite(conn, t.ra, MsgP2P, MsgPunchHandshake, P2PHandshakeReq{ID: t.id})
	if err != nil {
		gLog.Println(LvDEBUG, "handshakeC2C write MsgPunchHandshake error:", err)
		return err
	}
	ra, head, _, _, err := UDPRead(conn, HandshakeTimeout)
	if err != nil {
		time.Sleep(time.Millisecond * 200)
		gLog.Println(LvDEBUG, err, ", return this error when ip was not reachable, retry read")
		ra, head, _, _, err = UDPRead(conn, HandshakeTimeout)
		if err != nil {
			gLog.Println(LvDEBUG, "handshakeC2C read MsgPunchHandshake error:", err)
			return err
		}
	}
	t.ra, _ = net.ResolveUDPAddr("udp", ra.String())
	if head.MainType == MsgP2P && head.SubType == MsgPunchHandshake {
		gLog.Printf(LvDEBUG, "read %d handshake ", t.id)
		UDPWrite(conn, t.ra, MsgP2P, MsgPunchHandshakeAck, P2PHandshakeReq{ID: t.id})
		_, head, _, _, err = UDPRead(conn, HandshakeTimeout)
		if err != nil {
			gLog.Println(LvDEBUG, "handshakeC2C write MsgPunchHandshakeAck error", err)
			return err
		}
	}
	if head.MainType == MsgP2P && head.SubType == MsgPunchHandshakeAck {
		gLog.Printf(LvDEBUG, "read %d handshake ack ", t.id)
		_, err = UDPWrite(conn, t.ra, MsgP2P, MsgPunchHandshakeAck, P2PHandshakeReq{ID: t.id})
		if err != nil {
			gLog.Println(LvDEBUG, "handshakeC2C write MsgPunchHandshakeAck error", err)
			return err
		}
	}
	gLog.Printf(LvINFO, "handshakeC2C ok")
	return nil
}

func handshakeC2S(t *P2PTunnel) error {
	gLog.Printf(LvDEBUG, "handshakeC2S start")
	defer gLog.Printf(LvDEBUG, "handshakeC2S end")
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	randPorts := r.Perm(65532)
	conn, err := net.ListenUDP("udp", t.la)
	if err != nil {
		return err
	}
	defer conn.Close()

	go func() error {
		gLog.Printf(LvDEBUG, "send symmetric handshake to %s from %d:%d start", t.config.peerIP, t.coneLocalPort, t.coneNatPort)
		for i := 0; i < SymmetricHandshakeNum; i++ {
			// TODO: auto calc cost time
			// time.Sleep(SymmetricHandshakeInterval)
			dst, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", t.config.peerIP, randPorts[i]+2))
			if err != nil {
				return err
			}
			_, err = UDPWrite(conn, dst, MsgP2P, MsgPunchHandshake, P2PHandshakeReq{ID: t.id})
			if err != nil {
				gLog.Println(LvDEBUG, "handshakeC2S write MsgPunchHandshake error:", err)
				return err
			}
		}
		gLog.Println(LvDEBUG, "send symmetric handshake end")
		return nil
	}()
	err = conn.SetReadDeadline(time.Now().Add(HandshakeTimeout))
	if err != nil {
		gLog.Println(LvERROR, "SymmetricHandshakeAckTimeout SetReadDeadline error")
		return err
	}
	// read response of the punching hole ok port
	result := make([]byte, 1024)
	_, dst, err := conn.ReadFrom(result)
	if err != nil {
		gLog.Println(LvERROR, "handshakeC2S wait timeout")
		return err
	}
	head := &openP2PHeader{}
	err = binary.Read(bytes.NewReader(result[:openP2PHeaderSize]), binary.LittleEndian, head)
	if err != nil {
		gLog.Println(LvERROR, "parse p2pheader error:", err)
		return err
	}
	t.ra, _ = net.ResolveUDPAddr("udp", dst.String())
	if head.MainType == MsgP2P && head.SubType == MsgPunchHandshake {
		gLog.Printf(LvDEBUG, "handshakeC2S read %d handshake ", t.id)
		UDPWrite(conn, t.ra, MsgP2P, MsgPunchHandshakeAck, P2PHandshakeReq{ID: t.id})
		for {
			_, head, _, _, err = UDPRead(conn, HandshakeTimeout)
			if err != nil {
				gLog.Println(LvDEBUG, "handshakeC2S handshake error")
				return err
			}
			// waiting ack
			if head.MainType == MsgP2P && head.SubType == MsgPunchHandshakeAck {
				break
			}
		}
	}
	if head.MainType == MsgP2P && head.SubType == MsgPunchHandshakeAck {
		gLog.Printf(LvDEBUG, "handshakeC2S read %d handshake ack %s", t.id, t.ra.String())
		_, err = UDPWrite(conn, t.ra, MsgP2P, MsgPunchHandshakeAck, P2PHandshakeReq{ID: t.id})
		return err
	} else {
		gLog.Println(LvDEBUG, "handshakeS2C read msg but not MsgPunchHandshakeAck")
	}
	gLog.Printf(LvINFO, "handshakeC2S ok")
	return nil
}

func handshakeS2C(t *P2PTunnel) error {
	gLog.Printf(LvDEBUG, "handshakeS2C start")
	defer gLog.Printf(LvDEBUG, "handshakeS2C end")
	gotCh := make(chan *net.UDPAddr, 5)
	// sequencely udp send handshake, do not parallel send
	gLog.Printf(LvDEBUG, "send symmetric handshake to %s:%d start", t.config.peerIP, t.config.peerConeNatPort)
	gotIt := false
	for i := 0; i < SymmetricHandshakeNum; i++ {
		// TODO: auto calc cost time
		// time.Sleep(SymmetricHandshakeInterval)
		go func(t *P2PTunnel) error {
			conn, err := net.ListenUDP("udp", nil) // TODO: system allocated port really random?
			if err != nil {
				gLog.Printf(LvDEBUG, "listen error")
				return err
			}
			defer conn.Close()
			UDPWrite(conn, t.ra, MsgP2P, MsgPunchHandshake, P2PHandshakeReq{ID: t.id})
			_, head, _, _, err := UDPRead(conn, HandshakeTimeout)
			if err != nil {
				// gLog.Println(LevelDEBUG, "one of the handshake error:", err)
				return err
			}
			if gotIt {
				return nil
			}

			if head.MainType == MsgP2P && head.SubType == MsgPunchHandshake {
				gLog.Printf(LvDEBUG, "handshakeS2C read %d handshake ", t.id)
				UDPWrite(conn, t.ra, MsgP2P, MsgPunchHandshakeAck, P2PHandshakeReq{ID: t.id})
				// may read sereral MsgPunchHandshake
				for {
					_, head, _, _, err = UDPRead(conn, HandshakeTimeout)
					if err != nil {
						gLog.Println(LvDEBUG, "handshakeS2C handshake error")
						return err
					}
					if head.MainType == MsgP2P && head.SubType == MsgPunchHandshakeAck {
						break
					} else {
						gLog.Println(LvDEBUG, "handshakeS2C read msg but not MsgPunchHandshakeAck")
					}
				}
			}
			if head.MainType == MsgP2P && head.SubType == MsgPunchHandshakeAck {
				gLog.Printf(LvDEBUG, "handshakeS2C read %d handshake ack %s", t.id, conn.LocalAddr().String())
				UDPWrite(conn, t.ra, MsgP2P, MsgPunchHandshakeAck, P2PHandshakeReq{ID: t.id})
				gotIt = true
				la, _ := net.ResolveUDPAddr("udp", conn.LocalAddr().String())
				gotCh <- la
				return nil
			} else {
				gLog.Println(LvDEBUG, "handshakeS2C read msg but not MsgPunchHandshakeAck")
			}
			return nil
		}(t)
	}
	gLog.Printf(LvDEBUG, "send symmetric handshake end")
	if compareVersion(t.config.peerVersion, SymmetricSimultaneouslySendVersion) == LESS { // compatible with old client
		gLog.Println(LvDEBUG, "handshakeS2C ready, notify peer connect")
		t.pn.push(t.config.PeerNode, MsgPushHandshakeStart, TunnelMsg{ID: t.id})
	}

	select {
	case <-time.After(HandshakeTimeout):
		return fmt.Errorf("wait handshake failed")
	case la := <-gotCh:
		t.la = la
		gLog.Println(LvDEBUG, "symmetric handshake ok", la)
		gLog.Printf(LvINFO, "handshakeS2C ok")
	}
	return nil
}
