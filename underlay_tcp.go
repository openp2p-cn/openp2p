package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

type underlayTCP struct {
	listener net.Listener
	writeMtx *sync.Mutex
	net.Conn
}

func (conn *underlayTCP) Protocol() string {
	return "tcp"
}

func (conn *underlayTCP) ReadBuffer() (*openP2PHeader, []byte, error) {
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

func (conn *underlayTCP) WriteBytes(mainType uint16, subType uint16, data []byte) error {
	writeBytes := append(encodeHeader(mainType, subType, uint32(len(data))), data...)
	conn.writeMtx.Lock()
	_, err := conn.Write(writeBytes)
	conn.writeMtx.Unlock()
	return err
}

func (conn *underlayTCP) WriteBuffer(data []byte) error {
	conn.writeMtx.Lock()
	_, err := conn.Write(data)
	conn.writeMtx.Unlock()
	return err
}

func (conn *underlayTCP) WriteMessage(mainType uint16, subType uint16, packet interface{}) error {
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

func (conn *underlayTCP) Close() error {
	return conn.Conn.Close()
}

func listenTCP(port int, idleTimeout time.Duration) (*underlayTCP, error) {
	addr, _ := net.ResolveTCPAddr("tcp", fmt.Sprintf("0.0.0.0:%d", port))
	l, err := net.ListenTCP("tcp", addr)
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
	return &underlayTCP{writeMtx: &sync.Mutex{}, Conn: c}, nil
}

func dialTCP(host string, port int) (*underlayTCP, error) {
	c, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", host, port), SymmetricHandshakeAckTimeout)
	if err != nil {
		fmt.Printf("Dial %s:%d error:%s", host, port, err)
		return nil, err
	}
	return &underlayTCP{writeMtx: &sync.Mutex{}, Conn: c}, nil
}
