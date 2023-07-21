package openp2p

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"sync"
	"time"

	"github.com/quic-go/quic-go"
)

// quic.DialContext do not support version 44,disable it
var quicVersion []quic.VersionNumber

type underlayQUIC struct {
	listener quic.Listener
	writeMtx *sync.Mutex
	quic.Stream
	quic.Connection
}

func (conn *underlayQUIC) Protocol() string {
	return "quic"
}

func (conn *underlayQUIC) ReadBuffer() (*openP2PHeader, []byte, error) {
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

func (conn *underlayQUIC) WriteBytes(mainType uint16, subType uint16, data []byte) error {
	writeBytes := append(encodeHeader(mainType, subType, uint32(len(data))), data...)
	conn.writeMtx.Lock()
	_, err := conn.Write(writeBytes)
	conn.writeMtx.Unlock()
	return err
}

func (conn *underlayQUIC) WriteBuffer(data []byte) error {
	conn.writeMtx.Lock()
	_, err := conn.Write(data)
	conn.writeMtx.Unlock()
	return err
}

func (conn *underlayQUIC) WriteMessage(mainType uint16, subType uint16, packet interface{}) error {
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

func (conn *underlayQUIC) Close() error {
	conn.Stream.CancelRead(1)
	conn.Connection.CloseWithError(0, "")
	return nil
}
func (conn *underlayQUIC) CloseListener() {
	if conn.listener != nil {
		conn.listener.Close()
	}
}

func (conn *underlayQUIC) Accept() error {
	ctx, cancel := context.WithTimeout(context.Background(), UnderlayConnectTimeout)
	defer cancel()
	sess, err := conn.listener.Accept(ctx)
	if err != nil {
		return err
	}
	stream, err := sess.AcceptStream(context.Background())
	if err != nil {
		return err
	}
	conn.Stream = stream
	conn.Connection = sess
	return nil
}

func listenQuic(addr string, idleTimeout time.Duration) (*underlayQUIC, error) {
	gLog.Println(LvDEBUG, "quic listen on ", addr)
	listener, err := quic.ListenAddr(addr, generateTLSConfig(),
		&quic.Config{Versions: quicVersion, MaxIdleTimeout: idleTimeout, DisablePathMTUDiscovery: true})
	if err != nil {
		return nil, fmt.Errorf("quic.ListenAddr error:%s", err)
	}
	return &underlayQUIC{listener: listener, writeMtx: &sync.Mutex{}}, nil
}

func dialQuic(conn *net.UDPConn, remoteAddr *net.UDPAddr, idleTimeout time.Duration) (*underlayQUIC, error) {
	tlsConf := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"openp2pv1"},
	}
	Connection, err := quic.DialContext(context.Background(), conn, remoteAddr, conn.LocalAddr().String(), tlsConf,
		&quic.Config{Versions: quicVersion, MaxIdleTimeout: idleTimeout, DisablePathMTUDiscovery: true})
	if err != nil {
		return nil, fmt.Errorf("quic.DialContext error:%s", err)
	}
	stream, err := Connection.OpenStreamSync(context.Background())
	if err != nil {
		return nil, fmt.Errorf("OpenStreamSync error:%s", err)
	}
	qConn := &underlayQUIC{nil, &sync.Mutex{}, stream, Connection}
	return qConn, nil
}

// Setup a bare-bones TLS config for the server
func generateTLSConfig() *tls.Config {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		panic(err)
	}
	template := x509.Certificate{SerialNumber: big.NewInt(1)}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		panic(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		panic(err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
		NextProtos:   []string{"openp2pv1"},
	}
}
