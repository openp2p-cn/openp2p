package main

import (
	"os"
	"time"
)

type daemon struct {
}

func (d *daemon) run() {
	gLog.Println(LevelINFO, "daemon start")
	defer gLog.Println(LevelINFO, "daemon end")
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
		p, err := os.StartProcess(os.Args[0], args, execSpec)
		if err != nil {
			gLog.Printf(LevelERROR, "start worker error:%s", err)
			return
		}
		_, _ = p.Wait()
		gLog.Printf(LevelERROR, "worker stop, restart it after 10s")
		time.Sleep(time.Second * 10)
	}
}
