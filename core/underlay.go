package openp2p

import (
	"io"
	"time"
)

type underlay interface {
	Read([]byte) (int, error)
	Write([]byte) (int, error)
	ReadBuffer() (*openP2PHeader, []byte, error)
	WriteBytes(uint16, uint16, []byte) error
	WriteBuffer([]byte) error
	WriteMessage(uint16, uint16, interface{}) error
	Close() error
	WLock()
	WUnlock()
	SetReadDeadline(t time.Time) error
	SetWriteDeadline(t time.Time) error
	Protocol() string
}

func DefaultReadBuffer(ul underlay) (*openP2PHeader, []byte, error) {
	headBuf := make([]byte, openP2PHeaderSize)
	_, err := io.ReadFull(ul, headBuf)
	if err != nil {
		return nil, nil, err
	}
	head, err := decodeHeader(headBuf)
	if err != nil || head.MainType > 16 {
		return nil, nil, err
	}
	dataBuf := make([]byte, head.DataLen)
	_, err = io.ReadFull(ul, dataBuf)
	return head, dataBuf, err
}

func DefaultWriteBytes(ul underlay, mainType, subType uint16, data []byte) error {
	writeBytes := append(encodeHeader(mainType, subType, uint32(len(data))), data...)
	ul.SetWriteDeadline(time.Now().Add(TunnelHeartbeatTime / 2))
	ul.WLock()
	_, err := ul.Write(writeBytes)
	ul.WUnlock()
	return err
}

func DefaultWriteBuffer(ul underlay, data []byte) error {
	ul.SetWriteDeadline(time.Now().Add(TunnelHeartbeatTime / 2))
	ul.WLock()
	_, err := ul.Write(data)
	ul.WUnlock()
	return err
}

func DefaultWriteMessage(ul underlay, mainType uint16, subType uint16, packet interface{}) error {
	writeBytes, err := newMessage(mainType, subType, packet)
	if err != nil {
		return err
	}
	ul.SetWriteDeadline(time.Now().Add(TunnelHeartbeatTime / 2))
	ul.WLock()
	_, err = ul.Write(writeBytes)
	ul.WUnlock()
	return err
}
