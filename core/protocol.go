package openp2p

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"hash/crc64"
	"math/big"
	"net"
	"time"
)

const OpenP2PVersion = "3.10.2"
const ProductName string = "openp2p"
const LeastSupportVersion = "3.0.0"
const SyncServerTimeVersion = "3.9.0"

const (
	IfconfigPort1 = 27180
	IfconfigPort2 = 27181
	WsPort        = 27183
	UDPPort1      = 27182
	UDPPort2      = 27183
)

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
	MsgQuery     = 7
)

// TODO: seperate node push and web push.
const (
	MsgPushRsp               = 0
	MsgPushConnectReq        = 1
	MsgPushConnectRsp        = 2
	MsgPushHandshakeStart    = 3
	MsgPushAddRelayTunnelReq = 4
	MsgPushAddRelayTunnelRsp = 5
	MsgPushUpdate            = 6
	MsgPushReportApps        = 7
	MsgPushUnderlayConnect   = 8
	MsgPushEditApp           = 9
	MsgPushSwitchApp         = 10
	MsgPushRestart           = 11
	MsgPushEditNode          = 12
	MsgPushAPPKey            = 13
	MsgPushReportLog         = 14
	MsgPushDstNodeOnline     = 15
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
	MsgReportApps
	MsgReportLog
)

const (
	ReadBuffLen           = 4096             // for UDP maybe not enough
	NetworkHeartbeatTime  = time.Second * 30 // TODO: server no response hb, save flow
	TunnelHeartbeatTime   = time.Second * 10 // some nat udp session expired time less than 15s. change to 10s
	TunnelIdleTimeout     = time.Minute
	SymmetricHandshakeNum = 800 // 0.992379
	// SymmetricHandshakeNum        = 1000 // 0.999510
	SymmetricHandshakeInterval = time.Millisecond
	HandshakeTimeout           = time.Second * 5
	PeerAddRelayTimeount       = time.Second * 30 // peer need times
	CheckActiveTimeout         = time.Second * 5
	PaddingSize                = 16
	AESKeySize                 = 16
	MaxRetry                   = 10
	Cone2ConePunchMaxRetry     = 1
	RetryInterval              = time.Second * 30
	PublicIPEchoTimeout        = time.Second * 1
	NatTestTimeout             = time.Second * 5
	UDPReadTimeout             = time.Second * 5
	ClientAPITimeout           = time.Second * 10
	UnderlayConnectTimeout     = time.Second * 10
	MaxDirectTry               = 3
	PunchTsDelay               = time.Second * 2
)

// NATNone has public ip
const (
	NATNone      = 0
	NATCone      = 1
	NATSymmetric = 2
	NATUnknown   = 314
)

// underlay protocol
const (
	UderlayAuto = "auto"
	UderlayQUIC = "quic"
	UderlayTCP  = "tcp"
)

// linkmode
const (
	LinkModeUDPPunch = "udppunch"
	LinkModeTCPPunch = "tcppunch"
	LinkModeIPv4     = "ipv4" // for web
	LinkModeIPv6     = "ipv6" // for web
	LinkModeTCP6     = "tcp6"
	LinkModeTCP4     = "tcp4"
	LinkModeUDP6     = "udp6"
	LinkModeUDP4     = "udp4"
)

const (
	MsgQueryPeerInfoReq = iota
	MsgQueryPeerInfoRsp
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
	From             string `json:"from,omitempty"`
	FromToken        uint64 `json:"fromToken,omitempty"` // deprecated
	Version          string `json:"version,omitempty"`
	Token            uint64 `json:"token,omitempty"`       // if public totp token
	ConeNatPort      int    `json:"coneNatPort,omitempty"` // if isPublic, is public port
	NatType          int    `json:"natType,omitempty"`
	HasIPv4          int    `json:"hasIPv4,omitempty"`
	IPv6             string `json:"IPv6,omitempty"`
	HasUPNPorNATPMP  int    `json:"hasUPNPorNATPMP,omitempty"`
	FromIP           string `json:"fromIP,omitempty"`
	ID               uint64 `json:"id,omitempty"`
	AppKey           uint64 `json:"appKey,omitempty"` // for underlay tcp
	LinkMode         string `json:"linkMode,omitempty"`
	IsUnderlayServer int    `json:"isServer,omitempty"` // Requset spec peer is server
}
type PushDstNodeOnline struct {
	Node string `json:"node,omitempty"`
}
type PushConnectRsp struct {
	Error           int    `json:"error,omitempty"`
	From            string `json:"from,omitempty"`
	To              string `json:"to,omitempty"`
	Detail          string `json:"detail,omitempty"`
	NatType         int    `json:"natType,omitempty"`
	HasIPv4         int    `json:"hasIPv4,omitempty"`
	IPv6            string `json:"IPv6,omitempty"` // if public relay node, ipv6 not set
	HasUPNPorNATPMP int    `json:"hasUPNPorNATPMP,omitempty"`
	ConeNatPort     int    `json:"coneNatPort,omitempty"` //it's not only cone, but also upnp or nat-pmp hole
	FromIP          string `json:"fromIP,omitempty"`
	ID              uint64 `json:"id,omitempty"`
	PunchTs         uint64 `json:"punchts,omitempty"` // server timestamp
	Version         string `json:"version,omitempty"`
}
type PushRsp struct {
	Error  int    `json:"error,omitempty"`
	Detail string `json:"detail,omitempty"`
}

type LoginRsp struct {
	Error  int    `json:"error,omitempty"`
	Detail string `json:"detail,omitempty"`
	User   string `json:"user,omitempty"`
	Node   string `json:"node,omitempty"`
	Token  uint64 `json:"token,omitempty"`
	Ts     int64  `json:"ts,omitempty"`
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
	Token         uint64 `json:"token,omitempty"` // not totp token
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

type RelayNodeReq struct {
	PeerNode string `json:"peerNode,omitempty"`
}

type RelayNodeRsp struct {
	Mode       string `json:"mode,omitempty"` // private,public
	RelayName  string `json:"relayName,omitempty"`
	RelayToken uint64 `json:"relayToken,omitempty"`
}

type AddRelayTunnelReq struct {
	From       string `json:"from,omitempty"`
	RelayName  string `json:"relayName,omitempty"`
	RelayToken uint64 `json:"relayToken,omitempty"`
	AppID      uint64 `json:"appID,omitempty"`  // deprecated
	AppKey     uint64 `json:"appKey,omitempty"` // deprecated
}

type APPKeySync struct {
	AppID  uint64 `json:"appID,omitempty"`
	AppKey uint64 `json:"appKey,omitempty"`
}

type RelayHeartbeat struct {
	RelayTunnelID uint64 `json:"relayTunnelID,omitempty"`
	AppID         uint64 `json:"appID,omitempty"`
}

type ReportBasic struct {
	OS              string  `json:"os,omitempty"`
	Mac             string  `json:"mac,omitempty"`
	LanIP           string  `json:"lanIP,omitempty"`
	HasIPv4         int     `json:"hasIPv4,omitempty"`
	IPv6            string  `json:"IPv6,omitempty"`
	HasUPNPorNATPMP int     `json:"hasUPNPorNATPMP,omitempty"`
	Version         string  `json:"version,omitempty"`
	NetInfo         NetInfo `json:"netInfo,omitempty"`
}

type ReportConnect struct {
	Error          string `json:"error,omitempty"`
	Protocol       string `json:"protocol,omitempty"`
	SrcPort        int    `json:"srcPort,omitempty"`
	NatType        int    `json:"natType,omitempty"`
	PeerNode       string `json:"peerNode,omitempty"`
	DstPort        int    `json:"dstPort,omitempty"`
	DstHost        string `json:"dstHost,omitempty"`
	PeerUser       string `json:"peerUser,omitempty"`
	PeerNatType    int    `json:"peerNatType,omitempty"`
	PeerIP         string `json:"peerIP,omitempty"`
	ShareBandwidth int    `json:"shareBandWidth,omitempty"`
	RelayNode      string `json:"relayNode,omitempty"`
	Version        string `json:"version,omitempty"`
}

type AppInfo struct {
	AppName        string `json:"appName,omitempty"`
	Error          string `json:"error,omitempty"`
	Protocol       string `json:"protocol,omitempty"`
	SrcPort        int    `json:"srcPort,omitempty"`
	Protocol0      string `json:"protocol0,omitempty"`
	SrcPort0       int    `json:"srcPort0,omitempty"`
	NatType        int    `json:"natType,omitempty"`
	PeerNode       string `json:"peerNode,omitempty"`
	DstPort        int    `json:"dstPort,omitempty"`
	DstHost        string `json:"dstHost,omitempty"`
	PeerUser       string `json:"peerUser,omitempty"`
	PeerNatType    int    `json:"peerNatType,omitempty"`
	PeerIP         string `json:"peerIP,omitempty"`
	ShareBandwidth int    `json:"shareBandWidth,omitempty"`
	RelayNode      string `json:"relayNode,omitempty"`
	RelayMode      string `json:"relayMode,omitempty"`
	LinkMode       string `json:"linkMode,omitempty"`
	Version        string `json:"version,omitempty"`
	RetryTime      string `json:"retryTime,omitempty"`
	ConnectTime    string `json:"connectTime,omitempty"`
	IsActive       int    `json:"isActive,omitempty"`
	Enabled        int    `json:"enabled,omitempty"`
}

type ReportApps struct {
	Apps []AppInfo
}

type ReportLogReq struct {
	FileName string `json:"fileName,omitempty"`
	Offset   int64  `json:"offset,omitempty"`
	Len      int64  `json:"len,omitempty"`
}
type ReportLogRsp struct {
	FileName string `json:"fileName,omitempty"`
	Content  string `json:"content,omitempty"`
	Len      int64  `json:"len,omitempty"`
	Total    int64  `json:"total,omitempty"`
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

type ProfileInfo struct {
	User     string `json:"user,omitempty"`
	Password string `json:"password,omitempty"`
	Email    string `json:"email,omitempty"`
	Phone    string `json:"phone,omitempty"`
	Token    string `json:"token,omitempty"`
	Addtime  string `json:"addtime,omitempty"`
}

type EditNode struct {
	NewName   string `json:"newName,omitempty"`
	Bandwidth int    `json:"bandwidth,omitempty"`
}

type QueryPeerInfoReq struct {
	Token    uint64 `json:"token,omitempty"` // if public totp token
	PeerNode string `json:"peerNode,omitempty"`
}
type QueryPeerInfoRsp struct {
	Online          int    `json:"online,omitempty"`
	Version         string `json:"version,omitempty"`
	NatType         int    `json:"natType,omitempty"`
	IPv4            string `json:"IPv4,omitempty"`
	HasIPv4         int    `json:"hasIPv4,omitempty"` // has public ipv4
	IPv6            string `json:"IPv6,omitempty"`    // if public relay node, ipv6 not set
	HasUPNPorNATPMP int    `json:"hasUPNPorNATPMP,omitempty"`
}

const rootCA = `-----BEGIN CERTIFICATE-----
MIIDhTCCAm0CFHm0cd8dnGCbUW/OcS56jf0gvRk7MA0GCSqGSIb3DQEBCwUAMH4x
CzAJBgNVBAYTAkNOMQswCQYDVQQIDAJHRDETMBEGA1UECgwKb3BlbnAycC5jbjET
MBEGA1UECwwKb3BlbnAycC5jbjETMBEGA1UEAwwKb3BlbnAycC5jbjEjMCEGCSqG
SIb3DQEJARYUb3BlbnAycC5jbkBnbWFpbC5jb20wIBcNMjMwODAxMDkwMjMwWhgP
MjEyMzA3MDgwOTAyMzBaMH4xCzAJBgNVBAYTAkNOMQswCQYDVQQIDAJHRDETMBEG
A1UECgwKb3BlbnAycC5jbjETMBEGA1UECwwKb3BlbnAycC5jbjETMBEGA1UEAwwK
b3BlbnAycC5jbjEjMCEGCSqGSIb3DQEJARYUb3BlbnAycC5jbkBnbWFpbC5jb20w
ggEiMA0GCSqGSIb3DQEBAQUAA4IBDwAwggEKAoIBAQDWg8wPy5hBLUaY4WOXayKu
+magEz1LAY0krzXYSZaSCvGMwA0cervwAqgKfiiZEhho5UNA5iVOJ6bO1RL9H7Vp
4HuW9BttDU/NQHguD8pyqx06Kaosz5LRw8USz1BCWWFdmi8Mv4I0omtd7m6lbWnY
nrjQKLYPahPW481jUfJPqR6wUTnBuBMr2ZAGqmFR4Lhqs9B1P9GeBfDWNwVApJUC
VEhbElukRJxdUvWeJ5+HMENKQcHCTTgmQbmDLMobHXs3Xf7fT9qC76wOe9LFHI6L
dAww9gryQhxWauQl1NO8aGJTFu+3wgnKBdTMJmF/1iuZYXJOCR1solwqU1hCgBsj
AgMBAAEwDQYJKoZIhvcNAQELBQADggEBADp153YNVN8p6/3PLnXxHBDeDViAfeQd
VJmy8eH1LTq/xtUY71HGSpL7iIBNoQdDTHfsg3c6ZANBCxbO/7AhFAzPt1aK8eHy
XuEiW0Z6R8np1Khh3alCOfD15tKcjok//Wxisbz+YItlbDus/eWRbLGB3HGrzn4l
GB18jw+G7o4U3rGX8agHqVGQEd06gk1ZaprASpTGwSsv4A5ehosjT1d7re8Z5eD4
RVtXS+DplMClQ5QSlv3StwcWOsjyiAimNfLEU5xoEfq17yOJUTU1OTL4YOt16QUc
C1tnzFr3k/ioqFR7cnyzNrbjlfPOmO9l2WReEbMP3bvaSHm6EcpJKS8=
-----END CERTIFICATE-----`
