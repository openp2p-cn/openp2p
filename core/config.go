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
	PunchPriorityTCPFirst   = 1
	PunchPriorityUDPDisable = 1 << 1
	PunchPriorityTCPDisable = 1 << 2
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

	LogLevel   int
	MaxLogSize int
	daemonMode bool
	mtx        sync.Mutex
	sdwanMtx   sync.Mutex
	sdwan      SDWANInfo
	delNodes   []*SDWANNode
	addNodes   []*SDWANNode
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

func (c *Config) retryApp(peerNode string) {
	GNetwork.apps.Range(func(id, i interface{}) bool {
		app := i.(*p2pApp)
		if app.config.PeerNode == peerNode {
			gLog.Println(LvDEBUG, "retry app ", app.config.LogPeerNode())
			app.config.retryNum = 0
			app.config.nextRetryTime = time.Now()
			app.retryRelayNum = 0
			app.nextRetryRelayTime = time.Now()
			app.hbMtx.Lock()
			app.hbTimeRelay = time.Now().Add(-TunnelHeartbeatTime * 3)
			app.hbMtx.Unlock()
		}
		if app.config.RelayNode == peerNode {
			gLog.Println(LvDEBUG, "retry app ", app.config.LogPeerNode())
			app.retryRelayNum = 0
			app.nextRetryRelayTime = time.Now()
			app.hbMtx.Lock()
			app.hbTimeRelay = time.Now().Add(-TunnelHeartbeatTime * 3)
			app.hbMtx.Unlock()
		}
		return true
	})
}

func (c *Config) retryAllApp() {
	GNetwork.apps.Range(func(id, i interface{}) bool {
		app := i.(*p2pApp)
		gLog.Println(LvDEBUG, "retry app ", app.config.LogPeerNode())
		app.config.retryNum = 0
		app.config.nextRetryTime = time.Now()
		app.retryRelayNum = 0
		app.nextRetryRelayTime = time.Now()
		app.hbMtx.Lock()
		defer app.hbMtx.Unlock()
		app.hbTimeRelay = time.Now().Add(-TunnelHeartbeatTime * 3)
		return true
	})
}

func (c *Config) retryAllMemApp() {
	GNetwork.apps.Range(func(id, i interface{}) bool {
		app := i.(*p2pApp)
		if app.config.SrcPort != 0 {
			return true
		}
		gLog.Println(LvDEBUG, "retry app ", app.config.LogPeerNode())
		app.config.retryNum = 0
		app.config.nextRetryTime = time.Now()
		app.retryRelayNum = 0
		app.nextRetryRelayTime = time.Now()
		app.hbMtx.Lock()
		defer app.hbMtx.Unlock()
		app.hbTimeRelay = time.Now().Add(-TunnelHeartbeatTime * 3)
		return true
	})
}

func (c *Config) add(app AppConfig, override bool) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	defer c.save()
	if override {
		for i := 0; i < len(c.Apps); i++ {
			if c.Apps[i].PeerNode == app.PeerNode && c.Apps[i].Protocol == app.Protocol && c.Apps[i].SrcPort == app.SrcPort {
				c.Apps[i] = &app // override it
				return
			}
		}
	}
	c.Apps = append(c.Apps, &app)
}

func (c *Config) delete(app AppConfig) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	defer c.save()
	for i := 0; i < len(c.Apps); i++ {
		if (app.SrcPort != 0 && c.Apps[i].Protocol == app.Protocol && c.Apps[i].SrcPort == app.SrcPort) || // normal app
			(app.SrcPort == 0 && c.Apps[i].SrcPort == 0 && c.Apps[i].PeerNode == app.PeerNode) { // memapp
			if i == len(c.Apps)-1 {
				c.Apps = c.Apps[:i]
			} else {
				c.Apps = append(c.Apps[:i], c.Apps[i+1:]...)
			}
			return
		}
	}
}

func (c *Config) save() {
	// c.mtx.Lock()
	// defer c.mtx.Unlock()  // internal call
	if c.Network.Token == 0 {
		return
	}
	data, _ := json.MarshalIndent(c, "", "  ")
	err := os.WriteFile("config.json", data, 0644)
	if err != nil {
		gLog.Println(LvERROR, "save config.json error:", err)
	}
}

func (c *Config) saveCache() {
	// c.mtx.Lock()
	// defer c.mtx.Unlock()  // internal call
	if c.Network.Token == 0 {
		return
	}
	data, _ := json.MarshalIndent(c, "", "  ")
	err := os.WriteFile("config.json0", data, 0644)
	if err != nil {
		gLog.Println(LvERROR, "save config.json0 error:", err)
	}
}

func init() {
	gConf.LogLevel = int(LvINFO)
	gConf.MaxLogSize = 1024 * 1024
	gConf.Network.ShareBandwidth = 10
	gConf.Network.ServerHost = "api.openp2p.cn"
	gConf.Network.ServerPort = WsPort

}

func (c *Config) load() error {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	data, err := os.ReadFile("config.json")
	if err != nil {
		return c.loadCache()
	}
	err = json.Unmarshal(data, &c)
	if err != nil {
		gLog.Println(LvERROR, "parse config.json error:", err)
		// try cache
		return c.loadCache()
	}
	// load ok. cache it
	var filteredApps []*AppConfig // filter memapp
	for _, app := range c.Apps {
		if app.SrcPort != 0 {
			filteredApps = append(filteredApps, app)
		}
	}
	c.Apps = filteredApps
	c.saveCache()
	return err
}

func (c *Config) loadCache() error {
	data, err := os.ReadFile("config.json0")
	if err != nil {
		return err
	}
	err = json.Unmarshal(data, &c)
	if err != nil {
		gLog.Println(LvERROR, "parse config.json0 error:", err)
	}
	return err
}

// deal with multi-thread r/w
func (c *Config) setToken(token uint64) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	defer c.save()
	if token != 0 {
		c.Network.Token = token
	}
}
func (c *Config) setUser(user string) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	defer c.save()
	c.Network.User = user
}
func (c *Config) setNode(node string) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	defer c.save()
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
	natType         int
	hasIPv4         int
	publicIPv6      string // must lowwer-case not save json
	hasUPNPorNATPMP int
	ShareBandwidth  int
	// server info
	ServerHost string
	ServerPort int
	UDPPort1   int
	UDPPort2   int
	TCPPort    int
}

func parseParams(subCommand string, cmd string) {
	fset := flag.NewFlagSet(subCommand, flag.ExitOnError)
	serverHost := fset.String("serverhost", "api.openp2p.cn", "server host ")
	serverPort := fset.Int("serverport", WsPort, "server port ")
	// serverHost := flag.String("serverhost", "127.0.0.1", "server host ") // for debug
	token := fset.Uint64("token", 0, "token")
	node := fset.String("node", "", "node name. 8-31 characters. if not set, it will be hostname")
	peerNode := fset.String("peernode", "", "peer node name that you want to connect")
	dstIP := fset.String("dstip", "127.0.0.1", "destination ip ")
	whiteList := fset.String("whitelist", "", "whitelist for p2pApp ")
	dstPort := fset.Int("dstport", 0, "destination port ")
	srcPort := fset.Int("srcport", 0, "source port ")
	tcpPort := fset.Int("tcpport", 0, "tcp port for upnp or publicip")
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
		gLog.Println(LvINFO, "cmd=", cmd)
		args := strings.Split(cmd, " ")
		fset.Parse(args)
	}

	gLog.setMaxSize(int64(*maxLogSize))
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
		if f.Name == "tcpport" {
			gConf.Network.TCPPort = *tcpPort
		}
		if f.Name == "token" {
			gConf.setToken(*token)
		}
	})
	// set default value
	if gConf.Network.ServerHost == "" {
		gConf.Network.ServerHost = *serverHost
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
	if gConf.Network.TCPPort == 0 {
		if *tcpPort == 0 {
			p := int(gConf.nodeID()%15000 + 50000)
			tcpPort = &p
		}
		gConf.Network.TCPPort = *tcpPort
	}
	if *token == 0 {
		envToken := os.Getenv("OPENP2P_TOKEN")
		if envToken != "" {
			if n, err := strconv.ParseUint(envToken, 10, 64); n != 0 && err == nil {
				gConf.setToken(n)
			}
		}
	}
	gConf.Network.ServerPort = *serverPort
	gConf.Network.UDPPort1 = UDPPort1
	gConf.Network.UDPPort2 = UDPPort2
	gLog.setLevel(LogLevel(gConf.LogLevel))
	if *notVerbose {
		gLog.setMode(LogFile)
	}
	// gConf.mtx.Unlock()
	gConf.save()
}
