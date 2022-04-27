package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

type underlayTCP6 struct {
	listener net.Listener
	writeMtx *sync.Mutex
	net.Conn
}

func (conn *underlayTCP6) Protocol() string {
	return "tcp6"
}

func (conn *underlayTCP6) ReadBuffer() (*openP2PHeader, []byte, error) {
	headBuf := make([]byte, openP2PHeaderSize)
	_, err := io.ReadFull(conn, headBuf)
	if err != nil {
		return nil, nil, err
	}
	head, err := decodeHeader(headBuf)
	if err != nil {
		return nil, nil, err
	}
	dataBuf := make([]byte, head.DataLen)
	_, err = io.ReadFull(conn, dataBuf)
	return head, dataBuf, err
}

func (conn *underlayTCP6) WriteBytes(mainType uint16, subType uint16, data []byte) error {
	writeBytes := append(encodeHeader(mainType, subType, uint32(len(data))), data...)
	conn.writeMtx.Lock()
	_, err := conn.Write(writeBytes)
	conn.writeMtx.Unlock()
	return err
}

func (conn *underlayTCP6) WriteBuffer(data []byte) error {
	conn.writeMtx.Lock()
	_, err := conn.Write(data)
	conn.writeMtx.Unlock()
	return err
}

func (conn *underlayTCP6) WriteMessage(mainType uint16, subType uint16, packet interface{}) error {
	// TODO: call newMessage
	data, err := json.Marshal(packet)
	if err != nil {
		return err
	}
	writeBytes := append(encodeHeader(mainType, subType, uint32(len(data))), data...)
	conn.writeMtx.Lock()
	_, err = conn.Write(writeBytes)
	conn.writeMtx.Unlock()
	return err
}

func (conn *underlayTCP6) Close() error {
	return conn.Conn.Close()
}

func listenTCP6(port int, idleTimeout time.Duration) (*underlayTCP6, error) {
	addr, _ := net.ResolveTCPAddr("tcp6", fmt.Sprintf("0.0.0.0:%d", port))
	l, err := net.ListenTCP("tcp6", addr)
	if err != nil {
		return nil, err
	}
	defer l.Close()
	l.SetDeadline(time.Now().Add(SymmetricHandshakeAckTimeout))
	c, err := l.Accept()
	defer l.Close()
	if err != nil {
		return nil, err
	}
	return &underlayTCP6{writeMtx: &sync.Mutex{}, Conn: c}, nil
}

func dialTCP6(host string, port int) (*underlayTCP6, error) {
	c, err := net.DialTimeout("tcp6", fmt.Sprintf("%s:%d", host, port), SymmetricHandshakeAckTimeout)
	if err != nil {
		fmt.Printf("Dial %s:%d error:%s", host, port, err)
		return nil, err
	}
	return &underlayTCP6{writeMtx: &sync.Mutex{}, Conn: c}, nil
}
