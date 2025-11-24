package openp2p

import (
	"bytes"
	"encoding/binary"
	"errors"
	"net"
	"sync"
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

var overlayConns sync.Map // both TCP and UDP
func closeOverlayConns(appID uint64) {
	overlayConns.Range(func(_, i interface{}) bool {
		oConn := i.(*overlayConn)
		if oConn.app.id == appID {
			oConn.Close()
		}
		return true
	})
}

// implement io.Writer
type overlayConn struct {
	app      *p2pApp
	connTCP  net.Conn
	id       uint64
	running  bool
	isClient bool
	// for udp
	connUDP       *net.UDPConn
	remoteAddr    net.Addr
	udpData       chan []byte
	lastReadUDPTs time.Time
}

func (oConn *overlayConn) run() {
	gLog.d("oid:%d overlayConn run start", oConn.id)
	defer gLog.d("oid:%d overlayConn run end", oConn.id)
	oConn.lastReadUDPTs = time.Now()
	buffer := make([]byte, ReadBuffLen+PaddingSize) // 16 bytes for padding
	reuseBuff := buffer[:ReadBuffLen]
	encryptData := make([]byte, ReadBuffLen+PaddingSize) // 16 bytes for padding
	overlayHead := new(bytes.Buffer)

	binary.Write(overlayHead, binary.LittleEndian, oConn.id)
	for oConn.running && oConn.app.running {
		readBuff, dataLen, err := oConn.Read(reuseBuff)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			// overlay tcp connection normal close, debug log
			gLog.d("oid:%d overlayConn read error:%s,close it", oConn.id, err)
			break
		}
		payload := readBuff[:dataLen]
		if oConn.app.key != 0 {
			payload, _ = encryptBytes(oConn.app.appKeyBytes, encryptData, readBuff[:dataLen], dataLen)
		}
		writeBytes := append(overlayHead.Bytes(), payload...)
		oConn.app.WriteBytes(writeBytes)
	}
	if oConn.connTCP != nil {
		oConn.connTCP.Close()
	}
	if oConn.connUDP != nil {
		oConn.connUDP.Close()
	}
	overlayConns.Delete(oConn.id)
	// notify peer disconnect
	req := OverlayDisconnectReq{ID: oConn.id}
	oConn.app.WriteMessage(MsgP2P, MsgOverlayDisconnectReq, &req)
}

func (oConn *overlayConn) Read(reuseBuff []byte) (buff []byte, dataLen int, err error) {
	if !oConn.running {
		err = ErrOverlayConnDisconnect
		return
	}
	if oConn.connUDP != nil {
		if time.Now().After(oConn.lastReadUDPTs.Add(time.Minute * 5)) {
			err = errors.New("udp close")
			return
		}
		if oConn.remoteAddr != nil { // as server
			select {
			case buff = <-oConn.udpData:
				dataLen = len(buff) - PaddingSize
				oConn.lastReadUDPTs = time.Now()
			case <-time.After(time.Second * 10):
				err = ErrDeadlineExceeded
			}
		} else { // as client
			oConn.connUDP.SetReadDeadline(time.Now().Add(UDPReadTimeout))
			dataLen, _, err = oConn.connUDP.ReadFrom(reuseBuff)
			if err == nil {
				oConn.lastReadUDPTs = time.Now()
			}
			buff = reuseBuff
		}
		return
	}
	if oConn.connTCP != nil {
		oConn.connTCP.SetReadDeadline(time.Now().Add(UDPReadTimeout))
		dataLen, err = oConn.connTCP.Read(reuseBuff)
		buff = reuseBuff
	}

	return
}

// calling by p2pTunnel
func (oConn *overlayConn) Write(buff []byte) (n int, err error) {
	// add mutex when multi-thread calling
	if !oConn.running {
		return 0, ErrOverlayConnDisconnect
	}
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
	if oConn.connTCP != nil {
		n, err = oConn.connTCP.Write(buff)
	}

	if err != nil {
		oConn.running = false
	}
	return
}

func (oConn *overlayConn) Close() (err error) {
	oConn.running = false
	if oConn.connTCP != nil {
		oConn.connTCP.Close()
		// oConn.connTCP = nil
	}
	if oConn.connUDP != nil {
		oConn.connUDP.Close()
		// oConn.connUDP = nil
	}
	return nil
}
