package openp2p

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"sync"
	"time"

	"github.com/quic-go/quic-go"
)

// quic.Dial do not support version 44, disable it
var quicVersion []quic.Version

type underlayQUIC struct {
	listener *quic.Listener
	writeMtx *sync.Mutex
	*quic.Stream
	*quic.Conn
}

func (conn *underlayQUIC) Protocol() string {
	return "quic"
}

func (conn *underlayQUIC) ReadBuffer() (*openP2PHeader, []byte, error) {
	return DefaultReadBuffer(conn)
}

func (conn *underlayQUIC) WriteBytes(mainType uint16, subType uint16, data []byte) error {
	return DefaultWriteBytes(conn, mainType, subType, data)
}

func (conn *underlayQUIC) WriteBuffer(data []byte) error {
	return DefaultWriteBuffer(conn, data)
}

func (conn *underlayQUIC) WriteMessage(mainType uint16, subType uint16, packet interface{}) error {
	return DefaultWriteMessage(conn, mainType, subType, packet)
}

func (conn *underlayQUIC) Close() error {
	// CancelRead expects a StreamErrorCode; using 1 as before (application-defined)
	if conn.Stream != nil {
		conn.Stream.CancelRead(1)
		// close send-side of stream
		_ = conn.Stream.Close()
	}
	if conn.Conn != nil {
		// CloseWithError expects an ApplicationErrorCode and a description.
		// 0 is zero-value; keep behavior similar to old CloseWithError(0,"")
		_ = conn.Conn.CloseWithError(0, "")
	}
	conn.CloseListener()
	return nil
}
func (conn *underlayQUIC) WLock() {
	conn.writeMtx.Lock()
}
func (conn *underlayQUIC) WUnlock() {
	conn.writeMtx.Unlock()
}
func (conn *underlayQUIC) CloseListener() {
	if conn.listener != nil {
		_ = conn.listener.Close()
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
	conn.Conn = sess
	return nil
}

func listenQuic(addr string, idleTimeout time.Duration) (*underlayQUIC, error) {
	gLog.d("quic listen on %s", addr)
	listener, err := quic.ListenAddr(addr, generateTLSConfig(),
		&quic.Config{Versions: quicVersion, MaxIdleTimeout: idleTimeout, DisablePathMTUDiscovery: true})
	if err != nil {
		return nil, fmt.Errorf("quic.ListenAddr error:%s", err)
	}
	ul := &underlayQUIC{listener: listener, writeMtx: &sync.Mutex{}}
	err = ul.Accept()
	if err != nil {
		ul.CloseListener()
		return nil, fmt.Errorf("accept quic error:%s", err)
	}
	return ul, nil
}

func dialQuic(pconn *net.UDPConn, remoteAddr *net.UDPAddr, timeout time.Duration) (*underlayQUIC, error) {
	tlsConf := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"openp2pv1"},
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// New API: quic.Dial(ctx, packetConn, remoteAddr, tlsConf, quicConfig)
	connection, err := quic.Dial(ctx, pconn, remoteAddr, tlsConf,
		&quic.Config{Versions: quicVersion, MaxIdleTimeout: TunnelIdleTimeout, DisablePathMTUDiscovery: true})
	if err != nil {
		return nil, fmt.Errorf("quic.Dial error:%s", err)
	}
	stream, err := connection.OpenStreamSync(context.Background())
	if err != nil {
		return nil, fmt.Errorf("OpenStreamSync error:%s", err)
	}
	qConn := &underlayQUIC{nil, &sync.Mutex{}, stream, connection}
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
