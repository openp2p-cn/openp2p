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

const OpenP2PVersion = "3.10.9"
const ProductName string = "openp2p"
const LeastSupportVersion = "3.0.0"
const SyncServerTimeVersion = "3.9.0"
const SymmetricSimultaneouslySendVersion = "3.10.7"

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
	HandshakeTimeout           = time.Second * 10
	PeerAddRelayTimeount       = time.Second * 30 // peer need times
	CheckActiveTimeout         = time.Second * 5
	PaddingSize                = 16
	AESKeySize                 = 16
	MaxRetry                   = 10
	Cone2ConePunchMaxRetry     = 1
	PublicIPEchoTimeout        = time.Second * 1
	NatTestTimeout             = time.Second * 5
	UDPReadTimeout             = time.Second * 5
	ClientAPITimeout           = time.Second * 10
	UnderlayConnectTimeout     = time.Second * 10
	MaxDirectTry               = 3
	PunchTsDelay               = time.Second * 3
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
	Whitelist      string `json:"whitelist,omitempty"`
	SrcPort        int    `json:"srcPort,omitempty"`
	Protocol0      string `json:"protocol0,omitempty"`
	SrcPort0       int    `json:"srcPort0,omitempty"` // srcport+protocol is uneque, use as old app id
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

const ISRGRootX1 = `-----BEGIN CERTIFICATE-----
MIIEJjCCAw6gAwIBAgISAztStWq026ej0RCsk3ErbUdPMA0GCSqGSIb3DQEBCwUA
MDIxCzAJBgNVBAYTAlVTMRYwFAYDVQQKEw1MZXQncyBFbmNyeXB0MQswCQYDVQQD
EwJSMzAeFw0yMzA4MDQwODUyMjlaFw0yMzExMDIwODUyMjhaMBcxFTATBgNVBAMM
DCoub3BlbnAycC5jbjBZMBMGByqGSM49AgEGCCqGSM49AwEHA0IABPRdkgLV2FA+
3g/GjcA9UcfDfIFYgofSTNbOCQFIiQVMXrTgAToF1/tWaS2LOuysZcCX6OE7SCeG
lQ+0g+L2qvujggIaMIICFjAOBgNVHQ8BAf8EBAMCB4AwHQYDVR0lBBYwFAYIKwYB
BQUHAwEGCCsGAQUFBwMCMAwGA1UdEwEB/wQCMAAwHQYDVR0OBBYEFIdL5LNQC+X4
8r6u+3NlM238Vmk5MB8GA1UdIwQYMBaAFBQusxe3WFbLrlAJQOYfr52LFMLGMFUG
CCsGAQUFBwEBBEkwRzAhBggrBgEFBQcwAYYVaHR0cDovL3IzLm8ubGVuY3Iub3Jn
MCIGCCsGAQUFBzAChhZodHRwOi8vcjMuaS5sZW5jci5vcmcvMCMGA1UdEQQcMBqC
DCoub3BlbnAycC5jboIKb3BlbnAycC5jbjATBgNVHSAEDDAKMAgGBmeBDAECATCC
AQQGCisGAQQB1nkCBAIEgfUEgfIA8AB2AHoyjFTYty22IOo44FIe6YQWcDIThU07
0ivBOlejUutSAAABib/2fCgAAAQDAEcwRQIhAJzf9XNe0cu9CNYLLqtDCZZMqI6u
qsHrnnXcFQW23ioZAiAgwKp5DwZw9RmF19KOjD6lYJfTxc+anJUuWAlMwu1HYQB2
AK33vvp8/xDIi509nB4+GGq0Zyldz7EMJMqFhjTr3IKKAAABib/2fEEAAAQDAEcw
RQIgKeI7DopyzFXPdRQZKZrHVqfXQ8OipvlKXd5xRnKFjH4CIQDMM+TU+LOux8xK
1NlTiSs9DhQI/eU3ZXKxSQAqF50RnTANBgkqhkiG9w0BAQsFAAOCAQEATqZ+H2NT
cv4FzArD/Krlnur1OTitvpubRWM+ClB9Cr6pvPVB7Dp0/ALxu35ZmCtrzdJWTfmp
lHxU4nPXRPVjuPRNXooSyH//KTfHyf32919PQOi/qc/QEAuIzkGLJg0dIPKLxaNK
CiTWU+2iAYSHBgCWulfLX/RYNbBZQ9w0xIm3XhuMjCF/omG8ofuz1DmiRVR+17JA
nuDXQkxm7KhmbxSA4PsLwzvIWA8Wk44ZK7uncgRY3WIUXcVRELSFA5LuH67TOwag
al6iG56KW1N2Yy9YmeG27SYvHZYkjmuJ8NEy7Ku+Mi6gwO4hs0CYr2wtUacPfjKF
aYTGWSt6Pt8kmw==
-----END CERTIFICATE-----
-----BEGIN CERTIFICATE-----
MIIFFjCCAv6gAwIBAgIRAJErCErPDBinU/bWLiWnX1owDQYJKoZIhvcNAQELBQAw
TzELMAkGA1UEBhMCVVMxKTAnBgNVBAoTIEludGVybmV0IFNlY3VyaXR5IFJlc2Vh
cmNoIEdyb3VwMRUwEwYDVQQDEwxJU1JHIFJvb3QgWDEwHhcNMjAwOTA0MDAwMDAw
WhcNMjUwOTE1MTYwMDAwWjAyMQswCQYDVQQGEwJVUzEWMBQGA1UEChMNTGV0J3Mg
RW5jcnlwdDELMAkGA1UEAxMCUjMwggEiMA0GCSqGSIb3DQEBAQUAA4IBDwAwggEK
AoIBAQC7AhUozPaglNMPEuyNVZLD+ILxmaZ6QoinXSaqtSu5xUyxr45r+XXIo9cP
R5QUVTVXjJ6oojkZ9YI8QqlObvU7wy7bjcCwXPNZOOftz2nwWgsbvsCUJCWH+jdx
sxPnHKzhm+/b5DtFUkWWqcFTzjTIUu61ru2P3mBw4qVUq7ZtDpelQDRrK9O8Zutm
NHz6a4uPVymZ+DAXXbpyb/uBxa3Shlg9F8fnCbvxK/eG3MHacV3URuPMrSXBiLxg
Z3Vms/EY96Jc5lP/Ooi2R6X/ExjqmAl3P51T+c8B5fWmcBcUr2Ok/5mzk53cU6cG
/kiFHaFpriV1uxPMUgP17VGhi9sVAgMBAAGjggEIMIIBBDAOBgNVHQ8BAf8EBAMC
AYYwHQYDVR0lBBYwFAYIKwYBBQUHAwIGCCsGAQUFBwMBMBIGA1UdEwEB/wQIMAYB
Af8CAQAwHQYDVR0OBBYEFBQusxe3WFbLrlAJQOYfr52LFMLGMB8GA1UdIwQYMBaA
FHm0WeZ7tuXkAXOACIjIGlj26ZtuMDIGCCsGAQUFBwEBBCYwJDAiBggrBgEFBQcw
AoYWaHR0cDovL3gxLmkubGVuY3Iub3JnLzAnBgNVHR8EIDAeMBygGqAYhhZodHRw
Oi8veDEuYy5sZW5jci5vcmcvMCIGA1UdIAQbMBkwCAYGZ4EMAQIBMA0GCysGAQQB
gt8TAQEBMA0GCSqGSIb3DQEBCwUAA4ICAQCFyk5HPqP3hUSFvNVneLKYY611TR6W
PTNlclQtgaDqw+34IL9fzLdwALduO/ZelN7kIJ+m74uyA+eitRY8kc607TkC53wl
ikfmZW4/RvTZ8M6UK+5UzhK8jCdLuMGYL6KvzXGRSgi3yLgjewQtCPkIVz6D2QQz
CkcheAmCJ8MqyJu5zlzyZMjAvnnAT45tRAxekrsu94sQ4egdRCnbWSDtY7kh+BIm
lJNXoB1lBMEKIq4QDUOXoRgffuDghje1WrG9ML+Hbisq/yFOGwXD9RiX8F6sw6W4
avAuvDszue5L3sz85K+EC4Y/wFVDNvZo4TYXao6Z0f+lQKc0t8DQYzk1OXVu8rp2
yJMC6alLbBfODALZvYH7n7do1AZls4I9d1P4jnkDrQoxB3UqQ9hVl3LEKQ73xF1O
yK5GhDDX8oVfGKF5u+decIsH4YaTw7mP3GFxJSqv3+0lUFJoi5Lc5da149p90Ids
hCExroL1+7mryIkXPeFM5TgO9r0rvZaBFOvV2z0gp35Z0+L4WPlbuEjN/lxPFin+
HlUjr8gRsI3qfJOQFy/9rKIJR0Y/8Omwt/8oTWgy1mdeHmmjk7j1nYsvC9JSQ6Zv
MldlTTKB3zhThV1+XWYp6rjd5JW1zbVWEkLNxE7GJThEUG3szgBVGP7pSWTUTsqX
nLRbwHOoq7hHwg==
-----END CERTIFICATE-----
-----BEGIN CERTIFICATE-----
MIIFazCCA1OgAwIBAgIRAIIQz7DSQONZRGPgu2OCiwAwDQYJKoZIhvcNAQELBQAw
TzELMAkGA1UEBhMCVVMxKTAnBgNVBAoTIEludGVybmV0IFNlY3VyaXR5IFJlc2Vh
cmNoIEdyb3VwMRUwEwYDVQQDEwxJU1JHIFJvb3QgWDEwHhcNMTUwNjA0MTEwNDM4
WhcNMzUwNjA0MTEwNDM4WjBPMQswCQYDVQQGEwJVUzEpMCcGA1UEChMgSW50ZXJu
ZXQgU2VjdXJpdHkgUmVzZWFyY2ggR3JvdXAxFTATBgNVBAMTDElTUkcgUm9vdCBY
MTCCAiIwDQYJKoZIhvcNAQEBBQADggIPADCCAgoCggIBAK3oJHP0FDfzm54rVygc
h77ct984kIxuPOZXoHj3dcKi/vVqbvYATyjb3miGbESTtrFj/RQSa78f0uoxmyF+
0TM8ukj13Xnfs7j/EvEhmkvBioZxaUpmZmyPfjxwv60pIgbz5MDmgK7iS4+3mX6U
A5/TR5d8mUgjU+g4rk8Kb4Mu0UlXjIB0ttov0DiNewNwIRt18jA8+o+u3dpjq+sW
T8KOEUt+zwvo/7V3LvSye0rgTBIlDHCNAymg4VMk7BPZ7hm/ELNKjD+Jo2FR3qyH
B5T0Y3HsLuJvW5iB4YlcNHlsdu87kGJ55tukmi8mxdAQ4Q7e2RCOFvu396j3x+UC
B5iPNgiV5+I3lg02dZ77DnKxHZu8A/lJBdiB3QW0KtZB6awBdpUKD9jf1b0SHzUv
KBds0pjBqAlkd25HN7rOrFleaJ1/ctaJxQZBKT5ZPt0m9STJEadao0xAH0ahmbWn
OlFuhjuefXKnEgV4We0+UXgVCwOPjdAvBbI+e0ocS3MFEvzG6uBQE3xDk3SzynTn
jh8BCNAw1FtxNrQHusEwMFxIt4I7mKZ9YIqioymCzLq9gwQbooMDQaHWBfEbwrbw
qHyGO0aoSCqI3Haadr8faqU9GY/rOPNk3sgrDQoo//fb4hVC1CLQJ13hef4Y53CI
rU7m2Ys6xt0nUW7/vGT1M0NPAgMBAAGjQjBAMA4GA1UdDwEB/wQEAwIBBjAPBgNV
HRMBAf8EBTADAQH/MB0GA1UdDgQWBBR5tFnme7bl5AFzgAiIyBpY9umbbjANBgkq
hkiG9w0BAQsFAAOCAgEAVR9YqbyyqFDQDLHYGmkgJykIrGF1XIpu+ILlaS/V9lZL
ubhzEFnTIZd+50xx+7LSYK05qAvqFyFWhfFQDlnrzuBZ6brJFe+GnY+EgPbk6ZGQ
3BebYhtF8GaV0nxvwuo77x/Py9auJ/GpsMiu/X1+mvoiBOv/2X/qkSsisRcOj/KK
NFtY2PwByVS5uCbMiogziUwthDyC3+6WVwW6LLv3xLfHTjuCvjHIInNzktHCgKQ5
ORAzI4JMPJ+GslWYHb4phowim57iaztXOoJwTdwJx4nLCgdNbOhdjsnvzqvHu7Ur
TkXWStAmzOVyyghqpZXjFaH3pO3JLF+l+/+sKAIuvtd7u+Nxe5AW0wdeRlN8NwdC
jNPElpzVmbUq4JUagEiuTDkHzsxHpFKVK7q4+63SM1N95R1NbdWhscdCb+ZAJzVc
oyi3B43njTOQ5yOf+1CceWxG1bQVs5ZufpsMljq4Ui0/1lvh+wjChP4kqKOJ2qxq
4RgqsahDYVvTH9w7jXbyLeiNdd8XM2w9U/t7y0Ff/9yi0GE44Za4rF2LN9d11TPA
mRGunUHBcnWEvgJBQl9nJEiU0Zsnvgc/ubhPgXRR4Xq37Z0j4r7g1SgEEzwxA57d
emyPxgcYxn/eR44/KJ4EBs+lVDR3veyJm+kXQ99b21/+jh5Xos1AnX5iItreGCc=
-----END CERTIFICATE-----
`
