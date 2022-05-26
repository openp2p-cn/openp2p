package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/rand"
	"net"
	"sync"
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
	ra, head, _, _, err := UDPRead(conn, 5000)
	if err != nil {
		time.Sleep(time.Millisecond * 200)
		gLog.Println(LvDEBUG, err, ", return this error when ip was not reachable, retry read")
		ra, head, _, _, err = UDPRead(conn, 5000)
		if err != nil {
			gLog.Println(LvDEBUG, "handshakeC2C read MsgPunchHandshake error:", err)
			return err
		}
	}
	t.ra, _ = net.ResolveUDPAddr("udp", ra.String())
	// cone server side
	if head.MainType == MsgP2P && head.SubType == MsgPunchHandshake {
		gLog.Printf(LvDEBUG, "read %d handshake ", t.id)
		UDPWrite(conn, t.ra, MsgP2P, MsgPunchHandshakeAck, P2PHandshakeReq{ID: t.id})
		_, head, _, _, err = UDPRead(conn, 5000)
		if err != nil {
			gLog.Println(LvDEBUG, "handshakeC2C write MsgPunchHandshakeAck error", err)
			return err
		}
		if head.MainType == MsgP2P && head.SubType == MsgPunchHandshakeAck {
			gLog.Printf(LvDEBUG, "read %d handshake ack ", t.id)
			gLog.Printf(LvINFO, "handshakeC2C ok")
			return nil
		}
	}
	// cone client side will only read handshake ack
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
	// even if read timeout, continue handshake
	t.pn.read(t.config.PeerNode, MsgPush, MsgPushHandshakeStart, SymmetricHandshakeAckTimeout)
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
			time.Sleep(SymmetricHandshakeInterval)
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
	deadline := time.Now().Add(SymmetricHandshakeAckTimeout)
	err = conn.SetReadDeadline(deadline)
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
	if head.MainType == MsgP2P && head.SubType == MsgPunchHandshakeAck {
		gLog.Printf(LvDEBUG, "handshakeC2S read %d handshake ack %s", t.id, dst.String())
		_, err = UDPWrite(conn, dst, MsgP2P, MsgPunchHandshakeAck, P2PHandshakeReq{ID: t.id})
		return err
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
	gotMtx := sync.Mutex{}
	for i := 0; i < SymmetricHandshakeNum; i++ {
		// TODO: auto calc cost time
		time.Sleep(SymmetricHandshakeInterval)
		go func(t *P2PTunnel) error {
			conn, err := net.ListenUDP("udp", nil)
			if err != nil {
				gLog.Printf(LvDEBUG, "listen error")
				return err
			}
			defer conn.Close()
			UDPWrite(conn, t.ra, MsgP2P, MsgPunchHandshake, P2PHandshakeReq{ID: t.id})
			_, head, _, _, err := UDPRead(conn, 10000)
			if err != nil {
				// gLog.Println(LevelDEBUG, "one of the handshake error:", err)
				return err
			}
			gotMtx.Lock()
			defer gotMtx.Unlock()
			if gotIt {
				return nil
			}
			gotIt = true
			t.la, _ = net.ResolveUDPAddr("udp", conn.LocalAddr().String())
			if head.MainType == MsgP2P && head.SubType == MsgPunchHandshake {
				gLog.Printf(LvDEBUG, "handshakeS2C read %d handshake ", t.id)
				UDPWrite(conn, t.ra, MsgP2P, MsgPunchHandshakeAck, P2PHandshakeReq{ID: t.id})
				_, head, _, _, err = UDPRead(conn, 5000)
				if err != nil {
					gLog.Println(LvDEBUG, "handshakeS2C handshake error")
					return err
				}
				if head.MainType == MsgP2P && head.SubType == MsgPunchHandshakeAck {
					gLog.Printf(LvDEBUG, "handshakeS2C read %d handshake ack %s", t.id, conn.LocalAddr().String())
					gotCh <- t.la
					return nil
				}
			}
			return nil
		}(t)
	}
	gLog.Printf(LvDEBUG, "send symmetric handshake end")
	gLog.Println(LvDEBUG, "handshakeS2C ready, notify peer connect")
	t.pn.push(t.config.PeerNode, MsgPushHandshakeStart, TunnelMsg{ID: t.id})

	select {
	case <-time.After(SymmetricHandshakeAckTimeout):
		return fmt.Errorf("wait handshake failed")
	case la := <-gotCh:
		gLog.Println(LvDEBUG, "symmetric handshake ok", la)
		gLog.Printf(LvINFO, "handshakeS2C ok")
	}
	return nil
}
