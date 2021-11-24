package main

import (
	"encoding/json"
	"io/ioutil"
	"time"
)

var gConf Config

type AppConfig struct {
	// required
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

type Config struct {
	Network    NetworkConfig `json:"network"`
	Apps       []AppConfig   `json:"apps"`
	daemonMode bool
}

func (c *Config) add(app AppConfig) {
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

// func (c *Config) save() {
// 	data, _ := json.MarshalIndent(c, "", "")
// 	ioutil.WriteFile("config.json", data, 0644)
// }

func (c *Config) load() error {
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
	NoShare        bool
	localIP        string
	ipv6           string
	hostName       string
	mac            string
	os             string
	publicIP       string
	natType        int
	shareBandwidth int
	// server info
	ServerHost string
	ServerPort int
	UDPPort1   int
	UDPPort2   int
}
