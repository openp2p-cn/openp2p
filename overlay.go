package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"net"
	"time"
)

var ErrDeadlineExceeded error = &DeadlineExceededError{}

// DeadlineExceededError is returned for an expired deadline.
type DeadlineExceededError struct{}

// Implement the net.Error interface.
// The string is "i/o timeout" because that is what was returned
// by earlier Go versions. Changing it may break programs that
// match on error strings.
func (e *DeadlineExceededError) Error() string   { return "i/o timeout" }
func (e *DeadlineExceededError) Timeout() bool   { return true }
func (e *DeadlineExceededError) Temporary() bool { return true }

// implement io.Writer
type overlayConn struct {
	tunnel      *P2PTunnel
	connTCP     net.Conn
	id          uint64
	rtid        uint64
	running     bool
	isClient    bool
	appID       uint64
	appKey      uint64
	appKeyBytes []byte
	// for udp
	connUDP       *net.UDPConn
	remoteAddr    net.Addr
	udpRelayData  chan []byte
	lastReadUDPTs time.Time
}

func (oConn *overlayConn) run() {
	gLog.Printf(LevelDEBUG, "%d overlayConn run start", oConn.id)
	defer gLog.Printf(LevelDEBUG, "%d overlayConn run end", oConn.id)
	oConn.running = true
	oConn.lastReadUDPTs = time.Now()
	buffer := make([]byte, ReadBuffLen+PaddingSize)
	readBuf := buffer[:ReadBuffLen]
	encryptData := make([]byte, ReadBuffLen+PaddingSize) // 16 bytes for padding
	tunnelHead := new(bytes.Buffer)
	relayHead := new(bytes.Buffer)
	binary.Write(relayHead, binary.LittleEndian, oConn.rtid)
	binary.Write(tunnelHead, binary.LittleEndian, oConn.id)
	for oConn.running && oConn.tunnel.isRuning() {
		buff, dataLen, err := oConn.Read(readBuf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			// overlay tcp connection normal close, debug log
			gLog.Printf(LevelDEBUG, "overlayConn %d read error:%s,close it", oConn.id, err)
			break
		}
		payload := buff[:dataLen]
		if oConn.appKey != 0 {
			payload, _ = encryptBytes(oConn.appKeyBytes, encryptData, buffer[:dataLen], dataLen)
		}
		writeBytes := append(tunnelHead.Bytes(), payload...)
		if oConn.rtid == 0 {
			oConn.tunnel.conn.WriteBytes(MsgP2P, MsgOverlayData, writeBytes)
			gLog.Printf(LevelDEBUG, "write overlay data to %d:%d bodylen=%d", oConn.rtid, oConn.id, len(writeBytes))
		} else {
			// write raley data
			all := append(relayHead.Bytes(), encodeHeader(MsgP2P, MsgOverlayData, uint32(len(writeBytes)))...)
			all = append(all, writeBytes...)
			oConn.tunnel.conn.WriteBytes(MsgP2P, MsgRelayData, all)
			gLog.Printf(LevelDEBUG, "write relay data to %d:%d bodylen=%d", oConn.rtid, oConn.id, len(writeBytes))
		}
	}
	if oConn.connTCP != nil {
		oConn.connTCP.Close()
	}
	if oConn.connUDP != nil {
		oConn.connUDP.Close()
	}
	oConn.tunnel.overlayConns.Delete(oConn.id)
	// notify peer disconnect
	if oConn.isClient {
		req := OverlayDisconnectReq{ID: oConn.id}
		if oConn.rtid == 0 {
			oConn.tunnel.conn.WriteMessage(MsgP2P, MsgOverlayDisconnectReq, &req)
		} else {
			// write relay data
			msg, _ := newMessage(MsgP2P, MsgOverlayDisconnectReq, &req)
			msgWithHead := append(relayHead.Bytes(), msg...)
			oConn.tunnel.conn.WriteBytes(MsgP2P, MsgRelayData, msgWithHead)
		}
	}
}

func (oConn *overlayConn) Read(reuseBuff []byte) (buff []byte, n int, err error) {
	if oConn.connUDP != nil {
		if time.Now().After(oConn.lastReadUDPTs.Add(time.Minute * 5)) {
			err = errors.New("udp close")
			return
		}
		if oConn.remoteAddr != nil { // as server
			select {
			case buff = <-oConn.udpRelayData:
				n = len(buff)
				oConn.lastReadUDPTs = time.Now()
			case <-time.After(time.Second * 10):
				err = ErrDeadlineExceeded
			}
		} else { // as client
			oConn.connUDP.SetReadDeadline(time.Now().Add(5 * time.Second))
			n, _, err = oConn.connUDP.ReadFrom(reuseBuff)
			if err == nil {
				oConn.lastReadUDPTs = time.Now()
			}
			buff = reuseBuff
		}
		return
	}
	oConn.connTCP.SetReadDeadline(time.Now().Add(time.Second * 5))
	n, err = oConn.connTCP.Read(reuseBuff)
	buff = reuseBuff
	return
}

// calling by p2pTunnel
func (oConn *overlayConn) Write(buff []byte) (n int, err error) {
	// add mutex when multi-thread calling
	if oConn.connUDP != nil {
		if oConn.remoteAddr == nil {
			n, err = oConn.connUDP.Write(buff)
		} else {
			n, err = oConn.connUDP.WriteTo(buff, oConn.remoteAddr)
		}
		if err != nil {
			oConn.running = false
		}
		return
	}
	n, err = oConn.connTCP.Write(buff)
	if err != nil {
		oConn.running = false
	}
	return
}
