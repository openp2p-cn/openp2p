package openp2p

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"sync"
	"time"
)

var gConf Config

type AppConfig struct {
	// required
	AppName   string
	Protocol  string
	Whitelist string
	SrcPort   int
	PeerNode  string
	DstPort   int
	DstHost   string
	PeerUser  string
	RelayNode string
	Enabled   int // default:1
	// runtime info
	peerVersion      string
	peerToken        uint64
	peerNatType      int
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
	isUnderlayServer int // TODO: bool?
}

func (c *AppConfig) ID() string {
	return fmt.Sprintf("%s%d", c.Protocol, c.SrcPort)
}

// TODO: add loglevel, maxlogfilesize
type Config struct {
	Network    NetworkConfig `json:"network"`
	Apps       []*AppConfig  `json:"apps"`
	LogLevel   int
	daemonMode bool
	mtx        sync.Mutex
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
	c.mtx.Lock()
	defer c.mtx.Unlock()
	for i := 0; i < len(c.Apps); i++ {
		if c.Apps[i].PeerNode == peerNode {
			c.Apps[i].retryNum = 0
			c.Apps[i].nextRetryTime = time.Now()
		}
	}
}

func (c *Config) add(app AppConfig, override bool) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	defer c.save()
	if app.SrcPort == 0 || app.DstPort == 0 {
		gLog.Println(LvERROR, "invalid app ", app)
		return
	}
	if override {
		for i := 0; i < len(c.Apps); i++ {
			if c.Apps[i].Protocol == app.Protocol && c.Apps[i].SrcPort == app.SrcPort {
				c.Apps[i] = &app // override it
				return
			}
		}
	}
	c.Apps = append(c.Apps, &app)
}

func (c *Config) delete(app AppConfig) {
	if app.SrcPort == 0 || app.DstPort == 0 {
		return
	}
	c.mtx.Lock()
	defer c.mtx.Unlock()
	defer c.save()
	for i := 0; i < len(c.Apps); i++ {
		if c.Apps[i].Protocol == app.Protocol && c.Apps[i].SrcPort == app.SrcPort {
			c.Apps = append(c.Apps[:i], c.Apps[i+1:]...)
			return
		}
	}

}

func (c *Config) save() {
	// c.mtx.Lock()
	// defer c.mtx.Unlock()  // internal call
	data, _ := json.MarshalIndent(c, "", "  ")
	err := ioutil.WriteFile("config.json", data, 0644)
	if err != nil {
		gLog.Println(LvERROR, "save config.json error:", err)
	}
}

func init() {
	gConf.LogLevel = int(LvINFO)
	gConf.Network.ShareBandwidth = 10
	gConf.Network.ServerHost = "api.openp2p.cn"
	gConf.Network.ServerPort = WsPort

}

func (c *Config) load() error {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	data, err := ioutil.ReadFile("config.json")
	if err != nil {
		// gLog.Println(LevelERROR, "read config.json error:", err)
		return err
	}
	err = json.Unmarshal(data, &c)
	if err != nil {
		gLog.Println(LvERROR, "parse config.json error:", err)
	}
	return err
}

// TODO: deal with multi-thread r/w
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

func parseParams(subCommand string) {
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
	appName := fset.String("appname", "", "app name")
	relayNode := fset.String("relaynode", "", "relaynode")
	shareBandwidth := fset.Int("sharebandwidth", 10, "N mbps share bandwidth limit, private network no limit")
	daemonMode := fset.Bool("d", false, "daemonMode")
	notVerbose := fset.Bool("nv", false, "not log console")
	newconfig := fset.Bool("newconfig", false, "not load existing config.json")
	logLevel := fset.Int("loglevel", 1, "0:debug 1:info 2:warn 3:error")
	if subCommand == "" { // no subcommand
		fset.Parse(os.Args[1:])
	} else {
		fset.Parse(os.Args[2:])
	}

	config := AppConfig{Enabled: 1}
	config.PeerNode = *peerNode
	config.DstHost = *dstIP
	config.Whitelist = *whiteList
	config.DstPort = *dstPort
	config.SrcPort = *srcPort
	config.Protocol = *protocol
	config.AppName = *appName
	config.RelayNode = *relayNode
	if !*newconfig {
		gConf.load() // load old config. otherwise will clear all apps
	}
	if config.SrcPort != 0 {
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
			gConf.Network.Node = *node
		}
		if f.Name == "serverhost" {
			gConf.Network.ServerHost = *serverHost
		}
		if f.Name == "loglevel" {
			gConf.LogLevel = *logLevel
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
		gConf.Network.Node = *node
	} else {
		envNode := os.Getenv("OPENP2P_NODE")
		if envNode != "" {
			gConf.Network.Node = envNode
		}
		if gConf.Network.Node == "" { // if node name not set. use os.Hostname
			gConf.Network.Node = defaultNodeName()
		}
	}
	if gConf.Network.TCPPort == 0 {
		if *tcpPort == 0 {
			p := int(nodeNameToID(gConf.Network.Node)%15000 + 50000)
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
