package openp2p

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

var gConf Config

type AppConfig struct {
	// required
	AppName          string
	Protocol         string
	UnderlayProtocol string
	PunchPriority    int // bitwise DisableTCP|DisableUDP|TCPFirst  0:tcp and udp both enable, udp first
	Whitelist        string
	SrcPort          int
	PeerNode         string
	DstPort          int
	DstHost          string
	PeerUser         string
	RelayNode        string
	ForceRelay       int // default:0 disable;1 enable
	Enabled          int // default:1
	// runtime info
	relayMode        string // private|public
	peerVersion      string
	peerToken        uint64
	peerNatType      int
	peerLanIP        string
	hasIPv4          int
	peerIPv6         string
	hasUPNPorNATPMP  int
	peerIP           string
	peerConeNatPort  int
	retryNum         int
	retryTime        time.Time
	nextRetryTime    time.Time
	shareBandwidth   int
	errMsg           string
	connectTime      time.Time
	fromToken        uint64
	linkMode         string
	isUnderlayServer int
}

const (
	PunchPriorityUDPFirst = 0
	PunchPriorityTCPFirst = 1
	PunchPriorityTCPOnly  = 1 << 1
	PunchPriorityUDPOnly  = 1 << 2
)

func (c *AppConfig) ID() uint64 {
	if c.SrcPort == 0 { // memapp
		return NodeNameToID(c.PeerNode)
	}
	if c.Protocol == "tcp" {
		return uint64(c.SrcPort) * 10
	}
	return uint64(c.SrcPort)*10 + 1
}

func (c *AppConfig) LogPeerNode() string {
	if c.relayMode == "public" { // memapp
		return fmt.Sprintf("%d", NodeNameToID(c.PeerNode))
	}
	return c.PeerNode
}

type Config struct {
	Network NetworkConfig `json:"network"`
	Apps    []*AppConfig  `json:"apps"`

	LogLevel              int
	MaxLogSize            int
	TLSInsecureSkipVerify bool
	Forcev6               bool
	daemonMode            bool
	mtx                   sync.RWMutex
	fileMtx               sync.Mutex
	sdwanMtx              sync.Mutex
	sdwan                 SDWANInfo
	delNodes              []*SDWANNode
	addNodes              []*SDWANNode
}

func (c *Config) getSDWAN() SDWANInfo {
	c.sdwanMtx.Lock()
	defer c.sdwanMtx.Unlock()
	return c.sdwan
}

func (c *Config) getDelNodes() []*SDWANNode {
	c.sdwanMtx.Lock()
	defer c.sdwanMtx.Unlock()
	return c.delNodes
}

func (c *Config) getAddNodes() []*SDWANNode {
	c.sdwanMtx.Lock()
	defer c.sdwanMtx.Unlock()
	return c.addNodes
}

func (c *Config) resetSDWAN() {
	c.sdwanMtx.Lock()
	defer c.sdwanMtx.Unlock()
	c.delNodes = []*SDWANNode{}
	c.addNodes = []*SDWANNode{}
	c.sdwan = SDWANInfo{}
}
func (c *Config) setSDWAN(s SDWANInfo) {
	c.sdwanMtx.Lock()
	defer c.sdwanMtx.Unlock()
	// get old-new
	c.delNodes = []*SDWANNode{}
	for _, oldNode := range c.sdwan.Nodes {
		isDeleted := true
		for _, newNode := range s.Nodes {
			if oldNode.Name == newNode.Name && oldNode.IP == newNode.IP && oldNode.Resource == newNode.Resource && c.sdwan.Mode == s.Mode && c.sdwan.CentralNode == s.CentralNode {
				isDeleted = false
				break
			}
		}
		if isDeleted {
			c.delNodes = append(c.delNodes, oldNode)
		}
	}
	// get new-old
	c.addNodes = []*SDWANNode{}
	for _, newNode := range s.Nodes {
		isNew := true
		for _, oldNode := range c.sdwan.Nodes {
			if oldNode.Name == newNode.Name && oldNode.IP == newNode.IP && oldNode.Resource == newNode.Resource && c.sdwan.Mode == s.Mode && c.sdwan.CentralNode == s.CentralNode {
				isNew = false
				break
			}
		}
		if isNew {
			c.addNodes = append(c.addNodes, newNode)
		}
	}
	c.sdwan = s
	if c.sdwan.TunnelNum < 2 {
		c.sdwan.TunnelNum = 2 // DEBUG
	}
	if c.sdwan.TunnelNum > 3 {
		c.sdwan.TunnelNum = 3
	}
}

func (c *Config) switchApp(app AppConfig, enabled int) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	for i := 0; i < len(c.Apps); i++ {
		if c.Apps[i].Protocol == app.Protocol && c.Apps[i].SrcPort == app.SrcPort {
			c.Apps[i].Enabled = enabled
			c.Apps[i].retryNum = 0
			c.Apps[i].nextRetryTime = time.Now()
			break
		}
	}
	c.save()
}

// TODO: move to p2pnetwork
func (c *Config) retryApp(peerNode string) {
	GNetwork.apps.Range(func(id, i interface{}) bool {
		app := i.(*p2pApp)
		if app.config.PeerNode == peerNode {
			app.Retry(true)
		}
		if app.config.RelayNode == peerNode {
			app.Retry(false)
			gLog.d("retry app relay=%s", app.config.LogPeerNode())
		}
		return true
	})
}

func (c *Config) retryAllApp() {
	GNetwork.apps.Range(func(id, i interface{}) bool {
		app := i.(*p2pApp)
		app.Retry(true)
		return true
	})
}

func (c *Config) retryAllMemApp() {
	GNetwork.apps.Range(func(id, i interface{}) bool {
		app := i.(*p2pApp)
		if app.config.SrcPort != 0 {
			return true
		}
		app.Retry(true)
		return true
	})
}

func (c *Config) add(app AppConfig, override bool) {
	if app.AppName == "" {
		app.AppName = fmt.Sprintf("%d", app.ID())
	}
	c.mtx.Lock()
	defer c.mtx.Unlock()
	if override {
		for i := 0; i < len(c.Apps); i++ {
			if c.Apps[i].PeerNode == app.PeerNode && c.Apps[i].Protocol == app.Protocol && c.Apps[i].SrcPort == app.SrcPort {
				c.Apps[i] = &app // override it
				return
			}
		}
	}
	c.Apps = append(c.Apps, &app)
	if app.SrcPort != 0 {
		c.save()
	}
}

func (c *Config) delete(app AppConfig) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	for i := 0; i < len(c.Apps); i++ {
		if (app.SrcPort != 0 && c.Apps[i].Protocol == app.Protocol && c.Apps[i].SrcPort == app.SrcPort) || // normal app
			(app.SrcPort == 0 && c.Apps[i].SrcPort == 0 && c.Apps[i].PeerNode == app.PeerNode) { // memapp
			if i == len(c.Apps)-1 {
				c.Apps = c.Apps[:i]
			} else {
				c.Apps = append(c.Apps[:i], c.Apps[i+1:]...)
			}
			break
		}
	}
	if app.SrcPort != 0 {
		c.save()
	}
}

func (c *Config) save() {
	c.fileMtx.Lock()
	defer c.fileMtx.Unlock()
	if c.Network.Token == 0 {
		gLog.e("c.Network.Token == 0 skip save")
		return
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil || len(data) < 16 {
		gLog.e("MarshalIndent config.json error:%v,  len=%d", err, len(data))
		return
	}
	err = os.WriteFile("config.json0", data, 0644)
	if err != nil {
		gLog.e("save config.json error:%v", err)
	}

	// verify if the file is written correctly
	data, err = os.ReadFile("config.json0")
	if err != nil {
		return
	}
	var tmpConfig Config
	err = json.Unmarshal(data, &tmpConfig)
	if err != nil {
		gLog.e("parse config.json error:", err)
		return
	}
	err = os.Rename("config.json0", "config.json")
	if err != nil {
		gLog.e("rename config file error:%v", err)
	}

}

// -d run, then worker serverport always WsPort.
// func init() {
func init() {
	gConf.LogLevel = int(LvINFO)
	gConf.MaxLogSize = 1024 * 1024
	gConf.Network.ShareBandwidth = 10
	gConf.Network.ServerHost = "api.openp2p.cn"
	gConf.Network.ServerPort = WsPort

}

func (c *Config) load() error {
	c.fileMtx.Lock()
	defer c.fileMtx.Unlock()
	data, err := os.ReadFile("config.json")
	if err != nil {
		return err
	}
	c.mtx.Lock()
	defer c.mtx.Unlock()
	err = json.Unmarshal(data, &c)
	if err != nil {
		gLog.e("parse config.json error:", err)
		return err
	}
	var filteredApps []*AppConfig // filter memapp
	for _, app := range c.Apps {
		if app.SrcPort != 0 {
			filteredApps = append(filteredApps, app)
		}
	}
	c.Apps = filteredApps
	c.Network.natType = NATUnknown
	return err
}

// deal with multi-thread r/w
func (c *Config) setToken(token uint64) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	if token != 0 {
		c.Network.Token = token
	}
}
func (c *Config) setUser(user string) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.Network.User = user
}
func (c *Config) setNode(node string) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.Network.Node = node
	c.Network.nodeID = NodeNameToID(c.Network.Node)
}
func (c *Config) nodeID() uint64 {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	if c.Network.nodeID == 0 {
		c.Network.nodeID = NodeNameToID(c.Network.Node)
	}
	return c.Network.nodeID
}
func (c *Config) setShareBandwidth(bw int) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	defer c.save()
	c.Network.ShareBandwidth = bw
}
func (c *Config) setIPv6(v6 string) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.Network.publicIPv6 = v6
}
func (c *Config) IPv6() string {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	return c.Network.publicIPv6
}

type NetworkConfig struct {
	// local info
	Token           uint64
	Node            string
	nodeID          uint64
	User            string
	localIP         string
	mac             string
	os              string
	publicIP        string
	previousIP      string // for publicIP change detect
	natType         int
	hasIPv4         int
	publicIPv6      string // must lowwer-case not save json
	hasUPNPorNATPMP int
	ShareBandwidth  int
	// server info
	ServerHost     string
	ServerIP       string
	ServerPort     int
	natDetectPort1 int
	natDetectPort2 int
	PublicIPPort   int // both tcp and udp
	specTunnel     int
}

func parseParams(subCommand string, cmd string) {
	fset := flag.NewFlagSet(subCommand, flag.ExitOnError)
	installPath := fset.String("installpath", "", "custom install path")
	serverHost := fset.String("serverhost", "api.openp2p.cn", "server host ")
	insecure := fset.Bool("insecure", false, "not verify TLS certificate")
	serverPort := fset.Int("serverport", WsPort, "server port ")
	// serverHost := flag.String("serverhost", "127.0.0.1", "server host ") // for debug
	token := fset.Uint64("token", 0, "token")
	node := fset.String("node", "", "node name. 8-31 characters. if not set, it will be hostname")
	peerNode := fset.String("peernode", "", "peer node name that you want to connect")
	dstIP := fset.String("dstip", "127.0.0.1", "destination ip ")
	whiteList := fset.String("whitelist", "", "whitelist for p2pApp ")
	dstPort := fset.Int("dstport", 0, "destination port ")
	srcPort := fset.Int("srcport", 0, "source port ")
	publicIPPort := fset.Int("publicipport", 0, "public ip port for upnp or publicip")
	protocol := fset.String("protocol", "tcp", "tcp or udp")
	underlayProtocol := fset.String("underlay_protocol", "quic", "quic or kcp")
	punchPriority := fset.Int("punch_priority", 0, "bitwise DisableTCP|DisableUDP|UDPFirst  0:tcp and udp both enable, tcp first")
	appName := fset.String("appname", "", "app name")
	relayNode := fset.String("relaynode", "", "relaynode")
	shareBandwidth := fset.Int("sharebandwidth", 10, "N mbps share bandwidth limit, private network no limit")
	daemonMode := fset.Bool("d", false, "daemonMode")
	notVerbose := fset.Bool("nv", false, "not log console")
	newconfig := fset.Bool("newconfig", false, "not load existing config.json")
	logLevel := fset.Int("loglevel", 1, "0:debug 1:info 2:warn 3:error")
	maxLogSize := fset.Int("maxlogsize", 1024*1024, "default 1MB")
	if cmd == "" {
		if subCommand == "" { // no subcommand
			fset.Parse(os.Args[1:])
		} else {
			fset.Parse(os.Args[2:])
		}
	} else {
		args := strings.Split(cmd, " ")
		fset.Parse(args)
	}

	config := AppConfig{Enabled: 1}
	config.PeerNode = *peerNode
	config.DstHost = *dstIP
	config.Whitelist = *whiteList
	config.DstPort = *dstPort
	config.SrcPort = *srcPort
	config.Protocol = *protocol
	config.UnderlayProtocol = *underlayProtocol
	config.PunchPriority = *punchPriority
	config.AppName = *appName
	config.RelayNode = *relayNode
	if *installPath != "" {
		defaultInstallPath = *installPath
	}
	if subCommand == "install" {
		if err := os.MkdirAll(defaultInstallPath, 0775); err != nil {
			gLog.e("parseParams MkdirAll %s error:%s", defaultInstallPath, err)
			return
		}
		if err := os.Chdir(defaultInstallPath); err != nil {
			gLog.e("parseParams Chdir error:%s", err)
			return
		}
	}
	if !*newconfig {
		gConf.load() // load old config. otherwise will clear all apps
	}
	if config.SrcPort != 0 { // filter memapp
		gConf.add(config, true)
	}
	// gConf.mtx.Lock() // when calling this func it's single-thread no lock
	gConf.daemonMode = *daemonMode
	// spec paramters in commandline will always be used
	fset.Visit(func(f *flag.Flag) {
		if f.Name == "sharebandwidth" {
			gConf.Network.ShareBandwidth = *shareBandwidth
		}
		if f.Name == "node" {
			gConf.setNode(*node)
		}
		if f.Name == "serverhost" {
			gConf.Network.ServerHost = *serverHost
		}
		if f.Name == "loglevel" {
			gConf.LogLevel = *logLevel
		}
		if f.Name == "maxlogsize" {
			gConf.MaxLogSize = *maxLogSize
		}
		if f.Name == "publicipport" {
			gConf.Network.PublicIPPort = *publicIPPort
		}
		if f.Name == "token" {
			gConf.setToken(*token)
		}
		if f.Name == "serverport" {
			gConf.Network.ServerPort = *serverPort
		}
		if f.Name == "insecure" {
			gConf.TLSInsecureSkipVerify = *insecure
		}
	})
	// set default value
	if gConf.Network.ServerHost == "" {
		gConf.Network.ServerHost = *serverHost
	}
	if gConf.Network.ServerPort == 0 {
		gConf.Network.ServerPort = *serverPort
	}
	if *node != "" {
		gConf.setNode(*node)
	} else {
		envNode := os.Getenv("OPENP2P_NODE")
		if envNode != "" {
			gConf.setNode(envNode)
		}
		if gConf.Network.Node == "" { // if node name not set. use os.Hostname
			gConf.setNode(defaultNodeName())
		}
	}
	if gConf.Network.PublicIPPort == 0 {
		if *publicIPPort == 0 {
			p := int(gConf.nodeID()%8192 + 1025)
			publicIPPort = &p
		}
		gConf.Network.PublicIPPort = *publicIPPort
	}
	if *token == 0 {
		envToken := os.Getenv("OPENP2P_TOKEN")
		if envToken != "" {
			if n, err := strconv.ParseUint(envToken, 10, 64); n != 0 && err == nil {
				gConf.setToken(n)
			}
		}
	}

	gConf.Network.natDetectPort1 = NATDetectPort1
	gConf.Network.natDetectPort2 = NATDetectPort2
	gLog.setLevel(LogLevel(gConf.LogLevel))
	gLog.setMaxSize(int64(gConf.MaxLogSize))
	if *notVerbose {
		gLog.setMode(LogFile)
	}
	gConf.save()
}
