package openp2p

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/openp2p-cn/service"
)

type daemon struct {
	running bool
	proc    *os.Process
}

func (d *daemon) Start(s service.Service) error {
	gLog.Println(LvINFO, "daemon start")
	return nil
}

func (d *daemon) Stop(s service.Service) error {
	gLog.Println(LvINFO, "service stop")
	d.running = false
	if d.proc != nil {
		gLog.Println(LvINFO, "stop worker")
		d.proc.Kill()
	}
	if service.Interactive() {
		gLog.Println(LvINFO, "stop daemon")
		os.Exit(0)
	}
	return nil
}

func (d *daemon) run() {
	gLog.Println(LvINFO, "daemon run start")
	defer gLog.Println(LvINFO, "daemon run end")
	d.running = true
	binPath, _ := os.Executable()
	mydir, err := os.Getwd()
	if err != nil {
		fmt.Println(err)
	}
	gLog.Println(LvINFO, mydir)
	conf := &service.Config{
		Name:        ProductName,
		DisplayName: ProductName,
		Description: ProductName,
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

	args = append(args, "-nv")
	for {
		// start worker
		tmpDump := filepath.Join("log", "dump.log.tmp")
		dumpFile := filepath.Join("log", "dump.log")
		f, err := os.Create(filepath.Join(tmpDump))
		if err != nil {
			gLog.Printf(LvERROR, "start worker error:%s", err)
			return
		}
		gLog.Println(LvINFO, "start worker process, args:", args)
		execSpec := &os.ProcAttr{Env: append(os.Environ(), "GOTRACEBACK=crash"), Files: []*os.File{os.Stdin, os.Stdout, f}}
		lastRebootTime := time.Now()
		p, err := os.StartProcess(binPath, args, execSpec)
		if err != nil {
			gLog.Printf(LvERROR, "start worker error:%s", err)
			return
		}
		d.proc = p
		_, _ = p.Wait()
		f.Close()
		time.Sleep(time.Second)
		err = os.Rename(tmpDump, dumpFile)
		if err != nil {
			gLog.Printf(LvERROR, "rename dump error:%s", err)
		}
		if !d.running {
			return
		}
		if time.Since(lastRebootTime) < time.Second*10 {
			gLog.Printf(LvERROR, "worker stop, restart it after 10s")
			time.Sleep(time.Second * 10)
		}

	}
}

func (d *daemon) Control(ctrlComm string, exeAbsPath string, args []string) error {
	svcConfig := &service.Config{
		Name:        ProductName,
		DisplayName: ProductName,
		Description: ProductName,
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
