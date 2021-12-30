package main

import (
	"encoding/json"
	"io/ioutil"
	"sync"
	"time"
)

var gConf Config

type AppConfig struct {
	// required
	AppName      string
	Protocol     string
	SrcPort      int
	PeerNode     string
	DstPort      int
	DstHost      string
	PeerUser     string
	PeerPassword string
	// runtime info
	peerToken       uint64
	peerNatType     int
	peerIP          string
	peerConeNatPort int
	retryNum        int
	retryTime       time.Time
	shareBandwidth  int
}

// TODO: add loglevel, maxlogfilesize
type Config struct {
	Network    NetworkConfig `json:"network"`
	Apps       []AppConfig   `json:"apps"`
	daemonMode bool
	logLevel   int
	mtx        sync.Mutex
}

func (c *Config) add(app AppConfig) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	if app.SrcPort == 0 || app.DstPort == 0 {
		return
	}
	for i := 0; i < len(c.Apps); i++ {
		if c.Apps[i].Protocol == app.Protocol && c.Apps[i].SrcPort == app.SrcPort {
			return
		}
	}
	c.Apps = append(c.Apps, app)
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
	defer c.mtx.Unlock()
	data, err := ioutil.ReadFile("config.json")
	if err != nil {
		gLog.Println(LevelERROR, "read config.json error:", err)
		return err
	}
	err = json.Unmarshal(data, &c)
	if err != nil {
		gLog.Println(LevelERROR, "parse config.json error:", err)
	}
	return err
}

type NetworkConfig struct {
	// local info
	Node           string
	User           string
	Password       string
	localIP        string
	ipv6           string
	hostName       string
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
