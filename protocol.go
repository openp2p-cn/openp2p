package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"hash/crc64"
	"math/big"
	"net"
	"time"
)

const OpenP2PVersion = "0.95.5"
const ProducnName string = "openp2p"

type openP2PHeader struct {
	DataLen  uint32
	MainType uint16
	SubType  uint16
}

var openP2PHeaderSize = binary.Size(openP2PHeader{})

type PushHeader struct {
	From uint64
	To   uint64
}

var PushHeaderSize = binary.Size(PushHeader{})

type overlayHeader struct {
	id uint64
}

var overlayHeaderSize = binary.Size(overlayHeader{})

func decodeHeader(data []byte) (*openP2PHeader, error) {
	head := openP2PHeader{}
	rd := bytes.NewReader(data)
	err := binary.Read(rd, binary.LittleEndian, &head)
	if err != nil {
		return nil, err
	}
	return &head, nil
}

func encodeHeader(mainType uint16, subType uint16, len uint32) []byte {
	head := openP2PHeader{
		len,
		mainType,
		subType,
	}
	headBuf := new(bytes.Buffer)
	err := binary.Write(headBuf, binary.LittleEndian, head)
	if err != nil {
		return []byte("")
	}
	return headBuf.Bytes()
}

// Message type
const (
	MsgLogin     = 0
	MsgHeartbeat = 1
	MsgNATDetect = 2
	MsgPush      = 3
	MsgP2P       = 4
	MsgRelay     = 5
	MsgReport    = 6
)

const (
	MsgPushRsp               = 0
	MsgPushConnectReq        = 1
	MsgPushConnectRsp        = 2
	MsgPushHandshakeStart    = 3
	MsgPushAddRelayTunnelReq = 4
	MsgPushAddRelayTunnelRsp = 5
	MsgPushUpdate            = 6
	MsgPushReportApps        = 7
	MsgPushQuicConnect       = 8
)

// MsgP2P sub type message
const (
	MsgPunchHandshake = iota
	MsgPunchHandshakeAck
	MsgTunnelHandshake
	MsgTunnelHandshakeAck
	MsgTunnelHeartbeat
	MsgTunnelHeartbeatAck
	MsgOverlayConnectReq
	MsgOverlayConnectRsp
	MsgOverlayDisconnectReq
	MsgOverlayData
	MsgRelayData
	MsgRelayHeartbeat
	MsgRelayHeartbeatAck
)

// MsgRelay sub type message
const (
	MsgRelayNodeReq = iota
	MsgRelayNodeRsp
)

// MsgReport sub type message
const (
	MsgReportBasic = iota
	MsgReportQuery
	MsgReportConnect
)

const (
	ReadBuffLen           = 1024
	NetworkHeartbeatTime  = time.Second * 30 // TODO: server no response hb, save flow
	TunnelHeartbeatTime   = time.Second * 15
	TunnelIdleTimeout     = time.Minute
	SymmetricHandshakeNum = 800 // 0.992379
	// SymmetricHandshakeNum        = 1000 // 0.999510
	SymmetricHandshakeInterval   = time.Millisecond
	SymmetricHandshakeAckTimeout = time.Second * 11
	PeerAddRelayTimeount         = time.Second * 20
	CheckActiveTimeout           = time.Second * 5
	PaddingSize                  = 16
	AESKeySize                   = 16
	MaxRetry                     = 10
	RetryInterval                = time.Second * 30
	PublicIPEchoTimeout          = time.Second * 5
	NatTestTimeout               = time.Second * 10
)

// error message
var (
	// ErrorS2S string = "s2s is not supported"
	// ErrorHandshake string = "handshake error"
	ErrorS2S       = errors.New("s2s is not supported")
	ErrorHandshake = errors.New("handshake error")
)

// NATNone has public ip
const (
	NATNone      = 0
	NATCone      = 1
	NATSymmetric = 2
)

func newMessage(mainType uint16, subType uint16, packet interface{}) ([]byte, error) {
	data, err := json.Marshal(packet)
	if err != nil {
		return nil, err
	}
	// gLog.Println(LevelINFO,"write packet:", string(data))
	head := openP2PHeader{
		uint32(len(data)),
		mainType,
		subType,
	}
	headBuf := new(bytes.Buffer)
	err = binary.Write(headBuf, binary.LittleEndian, head)
	if err != nil {
		return nil, err
	}
	writeBytes := append(headBuf.Bytes(), data...)
	return writeBytes, nil
}

func nodeNameToID(name string) uint64 {
	return crc64.Checksum([]byte(name), crc64.MakeTable(crc64.ISO))
}

type PushConnectReq struct {
	From        string `json:"from,omitempty"`
	User        string `json:"user,omitempty"`
	Password    string `json:"password,omitempty"`
	Token       uint64 `json:"token,omitempty"`
	ConeNatPort int    `json:"coneNatPort,omitempty"`
	NatType     int    `json:"natType,omitempty"`
	FromIP      string `json:"fromIP,omitempty"`
	ID          uint64 `json:"id,omitempty"`
}
type PushConnectRsp struct {
	Error       int    `json:"error,omitempty"`
	From        string `json:"from,omitempty"`
	To          string `json:"to,omitempty"`
	Detail      string `json:"detail,omitempty"`
	NatType     int    `json:"natType,omitempty"`
	ConeNatPort int    `json:"coneNatPort,omitempty"`
	FromIP      string `json:"fromIP,omitempty"`
	ID          uint64 `json:"id,omitempty"`
}
type PushRsp struct {
	Error  int    `json:"error,omitempty"`
	Detail string `json:"detail,omitempty"`
}

type LoginRsp struct {
	Error  int    `json:"error,omitempty"`
	Detail string `json:"detail,omitempty"`
	Ts     uint64 `json:"ts,omitempty"`
}

type NatDetectReq struct {
	SrcPort  int `json:"srcPort,omitempty"`
	EchoPort int `json:"echoPort,omitempty"`
}

type NatDetectRsp struct {
	IP         string `json:"IP,omitempty"`
	Port       int    `json:"port,omitempty"`
	IsPublicIP int    `json:"isPublicIP,omitempty"`
}

type P2PHandshakeReq struct {
	ID uint64 `json:"id,omitempty"`
}

type OverlayConnectReq struct {
	ID            uint64 `json:"id,omitempty"`
	User          string `json:"user,omitempty"`
	Password      string `json:"password,omitempty"`
	DstIP         string `json:"dstIP,omitempty"`
	DstPort       int    `json:"dstPort,omitempty"`
	Protocol      string `json:"protocol,omitempty"`
	RelayTunnelID uint64 `json:"relayTunnelID,omitempty"` // if not 0 relay
	AppID         uint64 `json:"appID,omitempty"`
}
type OverlayDisconnectReq struct {
	ID uint64 `json:"id,omitempty"`
}
type TunnelMsg struct {
	ID uint64 `json:"id,omitempty"`
}

type RelayNodeRsp struct {
	RelayName  string `json:"relayName,omitempty"`
	RelayToken uint64 `json:"relayToken,omitempty"`
}

type AddRelayTunnelReq struct {
	From       string `json:"from,omitempty"`
	RelayName  string `json:"relayName,omitempty"`
	RelayToken uint64 `json:"relayToken,omitempty"`
	AppID      uint64 `json:"appID,omitempty"`
	AppKey     uint64 `json:"appKey,omitempty"`
}

type RelayHeartbeat struct {
	RelayTunnelID uint64 `json:"relayTunnelID,omitempty"`
	AppID         uint64 `json:"appID,omitempty"`
}

type ReportBasic struct {
	OS      string  `json:"os,omitempty"`
	Mac     string  `json:"mac,omitempty"`
	LanIP   string  `json:"lanIP,omitempty"`
	IPv6    string  `json:"IPv6,omitempty"`
	Version string  `json:"version,omitempty"`
	NetInfo NetInfo `json:"netInfo,omitempty"`
}

type ReportConnect struct {
	Error          string `json:"error,omitempty"`
	Protocol       string `json:"protocol,omitempty"`
	SrcPort        int    `json:"srcPort,omitempty"`
	NatType        int    `json:"natType,omitempty"`
	PeerNode       string `json:"peerNode,omitempty"`
	DstPort        int    `json:"dstPort,omitempty"`
	DstHost        string `json:"dsdtHost,omitempty"`
	PeerUser       string `json:"peerUser,omitempty"`
	PeerNatType    int    `json:"peerNatType,omitempty"`
	PeerIP         string `json:"peerIP,omitempty"`
	ShareBandwidth int    `json:"shareBandWidth,omitempty"`
	RelayNode      string `json:"relayNode,omitempty"`
	Version        string `json:"version,omitempty"`
}

type UpdateInfo struct {
	Error       int    `json:"error,omitempty"`
	ErrorDetail string `json:"errorDetail,omitempty"`
	Url         string `json:"url,omitempty"`
}

type NetInfo struct {
	IP         net.IP   `json:"ip"`
	IPDecimal  *big.Int `json:"ip_decimal"`
	Country    string   `json:"country,omitempty"`
	CountryISO string   `json:"country_iso,omitempty"`
	CountryEU  *bool    `json:"country_eu,omitempty"`
	RegionName string   `json:"region_name,omitempty"`
	RegionCode string   `json:"region_code,omitempty"`
	MetroCode  uint     `json:"metro_code,omitempty"`
	PostalCode string   `json:"zip_code,omitempty"`
	City       string   `json:"city,omitempty"`
	Latitude   float64  `json:"latitude,omitempty"`
	Longitude  float64  `json:"longitude,omitempty"`
	Timezone   string   `json:"time_zone,omitempty"`
	ASN        string   `json:"asn,omitempty"`
	ASNOrg     string   `json:"asn_org,omitempty"`
	Hostname   string   `json:"hostname,omitempty"`
}
