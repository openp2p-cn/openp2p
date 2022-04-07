package main

import (
	"encoding/json"
	"flag"
	"io/ioutil"
	"sync"
	"time"
)

var gConf Config

const IntValueNotSet int = -99999999

type AppConfig struct {
	// required
	AppName  string
	Protocol string
	SrcPort  int
	PeerNode string
	DstPort  int
	DstHost  string
	PeerUser string
	Enabled  int // default:1
	// runtime info
	peerToken       uint64
	peerNatType     int
	peerIP          string
	peerConeNatPort int
	retryNum        int
	retryTime       time.Time
	nextRetryTime   time.Time
	shareBandwidth  int
	errMsg          string
	connectTime     time.Time
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
			return
		}
	}
}

func (c *Config) add(app AppConfig, override bool) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	if app.SrcPort == 0 || app.DstPort == 0 {
		gLog.Println(LevelERROR, "invalid app ", app)
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
	for i := 0; i < len(c.Apps); i++ {
		if c.Apps[i].Protocol == app.Protocol && c.Apps[i].SrcPort == app.SrcPort {
			c.Apps = append(c.Apps[:i], c.Apps[i+1:]...)
			return
		}
	}
}

func (c *Config) save() {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	data, _ := json.MarshalIndent(c, "", "  ")
	err := ioutil.WriteFile("config.json", data, 0644)
	if err != nil {
		gLog.Println(LevelERROR, "save config.json error:", err)
	}
}

func (c *Config) load() error {
	c.mtx.Lock()
	c.LogLevel = IntValueNotSet
	c.Network.ShareBandwidth = IntValueNotSet
	defer c.mtx.Unlock()
	data, err := ioutil.ReadFile("config.json")
	if err != nil {
		// gLog.Println(LevelERROR, "read config.json error:", err)
		return err
	}
	err = json.Unmarshal(data, &c)
	if err != nil {
		gLog.Println(LevelERROR, "parse config.json error:", err)
	}
	return err
}

func (c *Config) setToken(token uint64) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.Network.Token = token
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
}
func (c *Config) setShareBandwidth(bw int) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.Network.ShareBandwidth = bw
}

type NetworkConfig struct {
	// local info
	Token          uint64
	Node           string
	User           string
	localIP        string
	ipv6           string
	mac            string
	os             string
	publicIP       string
	natType        int
	ShareBandwidth int
	// server info
	ServerHost string
	ServerPort int
	UDPPort1   int
	UDPPort2   int
}

func parseParams() {
	serverHost := flag.String("serverhost", "api.openp2p.cn", "server host ")
	// serverHost := flag.String("serverhost", "127.0.0.1", "server host ") // for debug
	node := flag.String("node", "", "node name. 8-31 characters")
	token := flag.Uint64("token", 0, "token")
	peerNode := flag.String("peernode", "", "peer node name that you want to connect")
	dstIP := flag.String("dstip", "127.0.0.1", "destination ip ")
	dstPort := flag.Int("dstport", 0, "destination port ")
	srcPort := flag.Int("srcport", 0, "source port ")
	protocol := flag.String("protocol", "tcp", "tcp or udp")
	appName := flag.String("appname", "", "app name")
	shareBandwidth := flag.Int("sharebandwidth", 10, "N mbps share bandwidth limit, private network no limit")
	daemonMode := flag.Bool("d", false, "daemonMode")
	notVerbose := flag.Bool("nv", false, "not log console")
	logLevel := flag.Int("loglevel", 1, "0:debug 1:info 2:warn 3:error")
	flag.Parse()

	config := AppConfig{Enabled: 1}
	config.PeerNode = *peerNode
	config.DstHost = *dstIP
	config.DstPort = *dstPort
	config.SrcPort = *srcPort
	config.Protocol = *protocol
	config.AppName = *appName
	gConf.load()
	if config.SrcPort != 0 {
		gConf.add(config, true)
	}
	// gConf.mtx.Lock() // when calling this func it's single-thread no lock
	gConf.daemonMode = *daemonMode
	// spec paramters in commandline will always be used
	flag.Visit(func(f *flag.Flag) {
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
	})

	if gConf.Network.ServerHost == "" {
		gConf.Network.ServerHost = *serverHost
	}
	if gConf.Network.Node == "" {
		if *node == "" { // config and param's node both empty
			hostname := defaultNodeName()
			node = &hostname
		}
		gConf.Network.Node = *node
	}
	if *token != 0 {
		gConf.Network.Token = *token
	}
	if gConf.LogLevel == IntValueNotSet {
		gConf.LogLevel = *logLevel
	}
	if gConf.Network.ShareBandwidth == IntValueNotSet {
		gConf.Network.ShareBandwidth = *shareBandwidth
	}

	gConf.Network.ServerPort = 27183
	gConf.Network.UDPPort1 = 27182
	gConf.Network.UDPPort2 = 27183
	gLog.setLevel(LogLevel(gConf.LogLevel))
	if *notVerbose {
		gLog.setMode(LogFile)
	}
	// gConf.mtx.Unlock()
	gConf.save()
}
