package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	os.Chdir(filepath.Dir(os.Args[0])) // for system service
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
	args = append(args, "-bydaemon")
	for {
		// start worker
		gLog.Println(LevelINFO, "start worker process")
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
// ./openp2p install -node hhd1207-222 -user tenderiron -password 13760636579 -noshare
// listen and build p2papp:
// ./openp2p install -node hhd1207-222 -user tenderiron -password 13760636579 -noshare -peernode hhdhome-n1 -dstip 127.0.0.1 -dstport 50022 -protocol tcp -srcport 22
func install() {
	gLog = InitLogger(filepath.Dir(os.Args[0]), "openp2p-install", LevelDEBUG, 1024*1024, LogConsole)
	// save config file
	installFlag := flag.NewFlagSet("install", flag.ExitOnError)
	serverHost := installFlag.String("serverhost", "api.openp2p.cn", "server host ")
	// serverHost := flag.String("serverhost", "127.0.0.1", "server host ") // for debug
	user := installFlag.String("user", "", "user name. 8-31 characters")
	node := installFlag.String("node", "", "node name. 8-31 characters")
	password := installFlag.String("password", "", "user password. 8-31 characters")
	peerNode := installFlag.String("peernode", "", "peer node name that you want to connect")
	peerUser := installFlag.String("peeruser", "", "peer node user (default peeruser=user)")
	peerPassword := installFlag.String("peerpassword", "", "peer node password (default peerpassword=password)")
	dstIP := installFlag.String("dstip", "127.0.0.1", "destination ip ")
	dstPort := installFlag.Int("dstport", 0, "destination port ")
	srcPort := installFlag.Int("srcport", 0, "source port ")
	protocol := installFlag.String("protocol", "tcp", "tcp or udp")
	noShare := installFlag.Bool("noshare", false, "disable using the huge numbers of shared nodes in OpenP2P network, your connectivity will be weak. also this node will not shared with others")
	shareBandwidth := installFlag.Int("sharebandwidth", 10, "N mbps share bandwidth limit, private node no limit")
	// logLevel := installFlag.Int("loglevel", 1, "0:debug 1:info 2:warn 3:error")
	installFlag.Parse(os.Args[2:])
	checkParams(*node, *user, *password)
	gConf.Network.ServerHost = *serverHost
	gConf.Network.User = *user
	gConf.Network.Node = *node
	gConf.Network.Password = *password
	gConf.Network.ServerPort = 27182
	gConf.Network.UDPPort1 = 27182
	gConf.Network.UDPPort2 = 27183
	gConf.Network.NoShare = *noShare
	gConf.Network.ShareBandwidth = *shareBandwidth
	config := AppConfig{}
	config.PeerNode = *peerNode
	config.PeerUser = *peerUser
	config.PeerPassword = *peerPassword
	config.DstHost = *dstIP
	config.DstPort = *dstPort
	config.SrcPort = *srcPort
	config.Protocol = *protocol
	gConf.add(config)
	os.MkdirAll(defaultInstallPath, 0775)
	err := os.Chdir(defaultInstallPath)
	if err != nil {
		gLog.Println(LevelERROR, "cd error:", err)
	}
	gConf.save()

	// copy files

	targetPath := filepath.Join(defaultInstallPath, defaultBinName)
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
	d := daemon{}

	// args := []string{""}
	gLog.Println(LevelINFO, "targetPath:", targetPath)
	err = d.Control("install", targetPath, []string{"-d", "-f"})
	if err != nil {
		gLog.Println(LevelERROR, "install system service error:", err)
	} else {
		gLog.Println(LevelINFO, "install system service ok.")
	}
	time.Sleep(time.Second * 2)
	err = d.Control("start", targetPath, []string{"-d", "-f"})
	if err != nil {
		gLog.Println(LevelERROR, "start openp2p service error:", err)
	} else {
		gLog.Println(LevelINFO, "start openp2p service ok.")
	}
}

func uninstall() {
	gLog = InitLogger(filepath.Dir(os.Args[0]), "openp2p-install", LevelDEBUG, 1024*1024, LogFileAndConsole)
	d := daemon{}
	d.Control("stop", "", nil)
	err := d.Control("uninstall", "", nil)
	if err != nil {
		gLog.Println(LevelERROR, "uninstall system service error:", err)
	} else {
		gLog.Println(LevelINFO, "uninstall system service ok.")
	}
	binPath := filepath.Join(defaultInstallPath, defaultBinName)
	os.Remove(binPath + "0")
	os.Rename(binPath, binPath+"0")
	os.RemoveAll(defaultInstallPath)
}

func checkParams(node, user, password string) {
	if len(node) < 8 {
		gLog.Println(LevelERROR, "node name too short, it must >=8 charaters")
		os.Exit(9)
	}
	if len(user) < 8 {
		gLog.Println(LevelERROR, "user name too short, it must >=8 charaters")
		os.Exit(9)
	}
	if len(password) < 8 {
		gLog.Println(LevelERROR, "password too short, it must >=8 charaters")
		os.Exit(9)
	}
}
