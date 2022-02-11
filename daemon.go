package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/kardianos/service"
)

type daemon struct {
	running bool
	proc    *os.Process
}

func (d *daemon) Start(s service.Service) error {
	gLog.Println(LevelINFO, "daemon start")
	return nil
}

func (d *daemon) Stop(s service.Service) error {
	gLog.Println(LevelINFO, "service stop")
	d.running = false
	if d.proc != nil {
		gLog.Println(LevelINFO, "stop worker")
		d.proc.Kill()
	}
	if service.Interactive() {
		gLog.Println(LevelINFO, "stop daemon")
		os.Exit(0)
	}
	return nil
}

func (d *daemon) run() {
	gLog.Println(LevelINFO, "daemon run start")
	defer gLog.Println(LevelINFO, "daemon run end")
	d.running = true
	binPath, _ := os.Executable()
	mydir, err := os.Getwd()
	if err != nil {
		fmt.Println(err)
	}
	gLog.Println(LevelINFO, mydir)
	conf := &service.Config{
		Name:        ProducnName,
		DisplayName: ProducnName,
		Description: ProducnName,
		Executable:  binPath,
	}

	s, _ := service.New(d, conf)
	go s.Run()
	var args []string
	// rm -d parameter
	for i := 0; i < len(os.Args); i++ {
		if os.Args[i] == "-d" {
			args = append(os.Args[0:i], os.Args[i+1:]...)
			break
		}
	}
	for {
		// start worker
		gLog.Println(LevelINFO, "start worker process, args:", args)
		execSpec := &os.ProcAttr{Files: []*os.File{os.Stdin, os.Stdout, os.Stderr}}
		p, err := os.StartProcess(binPath, args, execSpec)
		if err != nil {
			gLog.Printf(LevelERROR, "start worker error:%s", err)
			return
		}
		d.proc = p
		_, _ = p.Wait()
		if !d.running {
			return
		}
		gLog.Printf(LevelERROR, "worker stop, restart it after 10s")
		time.Sleep(time.Second * 10)
	}
}

func (d *daemon) Control(ctrlComm string, exeAbsPath string, args []string) error {
	svcConfig := &service.Config{
		Name:        ProducnName,
		DisplayName: ProducnName,
		Description: ProducnName,
		Executable:  exeAbsPath,
		Arguments:   args,
	}

	s, e := service.New(d, svcConfig)
	if e != nil {
		return e
	}
	e = service.Control(s, ctrlComm)
	if e != nil {
		return e
	}

	return nil
}

// examples:
// listen:
// ./openp2p install -node hhd1207-222 -token YOUR-TOKEN -sharebandwidth 0
// listen and build p2papp:
// ./openp2p install -node hhd1207-222 -token YOUR-TOKEN -sharebandwidth 0 -peernode hhdhome-n1 -dstip 127.0.0.1 -dstport 50022 -protocol tcp -srcport 22
func install() {
	gLog.Println(LevelINFO, "install start")
	defer gLog.Println(LevelINFO, "install end")
	// auto uninstall

	uninstall()
	// save config file
	installFlag := flag.NewFlagSet("install", flag.ExitOnError)
	serverHost := installFlag.String("serverhost", "api.openp2p.cn", "server host ")
	// serverHost := flag.String("serverhost", "127.0.0.1", "server host ") // for debug
	token := installFlag.Uint64("token", 0, "token")
	node := installFlag.String("node", "", "node name. 8-31 characters. if not set, it will be hostname")
	peerNode := installFlag.String("peernode", "", "peer node name that you want to connect")
	dstIP := installFlag.String("dstip", "127.0.0.1", "destination ip ")
	dstPort := installFlag.Int("dstport", 0, "destination port ")
	srcPort := installFlag.Int("srcport", 0, "source port ")
	protocol := installFlag.String("protocol", "tcp", "tcp or udp")
	appName := flag.String("appname", "", "app name")
	installFlag.Bool("noshare", false, "deprecated. uses -sharebandwidth 0")
	shareBandwidth := installFlag.Int("sharebandwidth", 10, "N mbps share bandwidth limit, private node no limit")
	logLevel := installFlag.Int("loglevel", 1, "0:debug 1:info 2:warn 3:error")
	installFlag.Parse(os.Args[2:])
	if *node != "" && len(*node) < 8 {
		gLog.Println(LevelERROR, ErrNodeTooShort)
		os.Exit(9)
	}
	if *node == "" { // if node name not set. use os.Hostname
		hostname := defaultNodeName()
		node = &hostname
	}
	gConf.load() // load old config. otherwise will clear all apps
	gConf.LogLevel = *logLevel
	gConf.Network.ServerHost = *serverHost
	gConf.Network.Token = *token
	gConf.Network.Node = *node
	gConf.Network.ServerPort = 27183
	gConf.Network.UDPPort1 = 27182
	gConf.Network.UDPPort2 = 27183
	gConf.Network.ShareBandwidth = *shareBandwidth
	config := AppConfig{Enabled: 1}
	config.PeerNode = *peerNode
	config.DstHost = *dstIP
	config.DstPort = *dstPort
	config.SrcPort = *srcPort
	config.Protocol = *protocol
	config.AppName = *appName
	if config.SrcPort != 0 {
		gConf.add(config, true)
	}
	err := os.MkdirAll(defaultInstallPath, 0775)
	if err != nil {
		gLog.Printf(LevelERROR, "MkdirAll %s error:%s", defaultInstallPath, err)
		return
	}
	err = os.Chdir(defaultInstallPath)
	if err != nil {
		gLog.Println(LevelERROR, "cd error:", err)
		return
	}
	gConf.save()
	targetPath := filepath.Join(defaultInstallPath, defaultBinName)
	d := daemon{}
	// copy files

	binPath, _ := os.Executable()
	src, errFiles := os.Open(binPath) // can not use args[0], on Windows call openp2p is ok(=openp2p.exe)
	if errFiles != nil {
		gLog.Printf(LevelERROR, "os.OpenFile %s error:%s", os.Args[0], errFiles)
		return
	}

	dst, errFiles := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0775)
	if errFiles != nil {
		gLog.Printf(LevelERROR, "os.OpenFile %s error:%s", targetPath, errFiles)
		return
	}

	_, errFiles = io.Copy(dst, src)
	if errFiles != nil {
		gLog.Printf(LevelERROR, "io.Copy error:%s", errFiles)
		return
	}
	src.Close()
	dst.Close()

	// install system service
	// args := []string{""}
	gLog.Println(LevelINFO, "targetPath:", targetPath)
	err = d.Control("install", targetPath, []string{"-d"})
	if err == nil {
		gLog.Println(LevelINFO, "install system service ok.")
	}
	time.Sleep(time.Second * 2)
	err = d.Control("start", targetPath, []string{"-d"})
	if err != nil {
		gLog.Println(LevelERROR, "start openp2p service error:", err)
	} else {
		gLog.Println(LevelINFO, "start openp2p service ok.")
	}
}

func installByFilename() {
	params := strings.Split(filepath.Base(os.Args[0]), "-")
	if len(params) < 4 {
		return
	}
	serverHost := params[1]
	token := params[2]
	gLog.Println(LevelINFO, "install start")
	targetPath := os.Args[0]
	args := []string{"install"}
	args = append(args, "-serverhost")
	args = append(args, serverHost)
	args = append(args, "-token")
	args = append(args, token)
	env := os.Environ()
	cmd := exec.Command(targetPath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = env
	err := cmd.Run()
	if err != nil {
		gLog.Println(LevelERROR, "install by filename, start process error:", err)
		return
	}
	gLog.Println(LevelINFO, "install end")
	fmt.Println("Press the Any Key to exit")
	fmt.Scanln()
	os.Exit(0)
}
func uninstall() {
	gLog.Println(LevelINFO, "uninstall start")
	defer gLog.Println(LevelINFO, "uninstall end")
	d := daemon{}
	err := d.Control("stop", "", nil)
	if err != nil { // service maybe not install
		return
	}
	err = d.Control("uninstall", "", nil)
	if err != nil {
		gLog.Println(LevelERROR, "uninstall system service error:", err)
	} else {
		gLog.Println(LevelINFO, "uninstall system service ok.")
	}
	binPath := filepath.Join(defaultInstallPath, defaultBinName)
	os.Remove(binPath + "0")
	os.Remove(binPath)
	// os.RemoveAll(defaultInstallPath)  // reserve config.json
}
