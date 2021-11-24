package main

import (
	"bytes"
	"encoding/binary"
	"net"
	"time"
)

// implement io.Writer
type overlayTCP struct {
	tunnel      *P2PTunnel
	conn        net.Conn
	id          uint64
	rtid        uint64
	running     bool
	isClient    bool
	appID       uint64
	appKey      uint64
	appKeyBytes []byte
}

func (otcp *overlayTCP) run() {
	gLog.Printf(LevelINFO, "%d overlayTCP run start", otcp.id)
	defer gLog.Printf(LevelINFO, "%d overlayTCP run end", otcp.id)
	otcp.running = true
	buffer := make([]byte, ReadBuffLen+PaddingSize)
	readBuf := buffer[:ReadBuffLen]
	encryptData := make([]byte, ReadBuffLen+PaddingSize) // 16 bytes for padding
	tunnelHead := new(bytes.Buffer)
	relayHead := new(bytes.Buffer)
	binary.Write(relayHead, binary.LittleEndian, otcp.rtid)
	binary.Write(tunnelHead, binary.LittleEndian, otcp.id)
	for otcp.running && otcp.tunnel.isRuning() {
		otcp.conn.SetReadDeadline(time.Now().Add(time.Second * 5))
		dataLen, err := otcp.conn.Read(readBuf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			// overlay tcp connection normal close, debug log
			gLog.Printf(LevelDEBUG, "overlayTCP %d read error:%s,close it", otcp.id, err)
			break
		} else {
			payload := readBuf[:dataLen]
			if otcp.appKey != 0 {
				payload, _ = encryptBytes(otcp.appKeyBytes, encryptData, buffer[:dataLen], dataLen)
			}
			writeBytes := append(tunnelHead.Bytes(), payload...)
			if otcp.rtid == 0 {
				otcp.tunnel.conn.WriteBytes(MsgP2P, MsgOverlayData, writeBytes)
			} else {
				// write raley data
				all := append(relayHead.Bytes(), encodeHeader(MsgP2P, MsgOverlayData, uint32(len(writeBytes)))...)
				all = append(all, writeBytes...)
				otcp.tunnel.conn.WriteBytes(MsgP2P, MsgRelayData, all)
				gLog.Printf(LevelDEBUG, "write relay data to %d:%d bodylen=%d", otcp.rtid, otcp.id, len(writeBytes))
			}
		}
	}
	otcp.conn.Close()
	otcp.tunnel.overlayConns.Delete(otcp.id)
	// notify peer disconnect
	if otcp.isClient {
		req := OverlayDisconnectReq{ID: otcp.id}
		if otcp.rtid == 0 {
			otcp.tunnel.conn.WriteMessage(MsgP2P, MsgOverlayDisconnectReq, &req)
		} else {
			// write relay data
			msg, _ := newMessage(MsgP2P, MsgOverlayDisconnectReq, &req)
			msgWithHead := append(relayHead.Bytes(), msg...)
			otcp.tunnel.conn.WriteBytes(MsgP2P, MsgRelayData, msgWithHead)
		}
	}
}

// calling by p2pTunnel
func (otcp *overlayTCP) Write(buff []byte) (n int, err error) {
	// add mutex when multi-thread calling
	n, err = otcp.conn.Write(buff)
	if err != nil {
		otcp.tunnel.overlayConns.Delete(otcp.id)
	}
	return
}
