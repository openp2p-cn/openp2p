package openp2p

import (
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/openp2p-cn/service"
)

type daemon struct {
	running bool
	proc    *os.Process
}

func (d *daemon) Start(s service.Service) error {
	gLog.i("system service start")
	return nil
}

func (d *daemon) Stop(s service.Service) error {
	gLog.i("system service stop")
	d.running = false
	if d.proc != nil {
		gLog.i("stop worker")
		d.proc.Kill()
	}
	if service.Interactive() {
		gLog.i("stop daemon")
		os.Exit(0)
	}
	return nil
}

func (d *daemon) run() {
	gLog.close()
	baseDir := filepath.Dir(os.Args[0])
	gLog = NewLogger(baseDir, "daemon", LogLevel(gConf.LogLevel), 1024*1024, LogFile|LogConsole)
	gLog.i("daemon run start")
	defer gLog.i("daemon run end")
	d.running = true
	binPath, _ := os.Executable()
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
		tmpDump := filepath.Join(filepath.Dir(binPath), "log", "dump.log.tmp")
		dumpFile := filepath.Join(filepath.Dir(binPath), "log", "dump.log")
		// f, err := os.Create(filepath.Join(tmpDump))
		f, err := os.OpenFile(filepath.Join(tmpDump), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0775)
		if err != nil {
			gLog.e("OpenFile %s error:%s", tmpDump, err)
			return
		}
		gLog.i("start worker process, args:%v", args)
		execSpec := &os.ProcAttr{Env: append(os.Environ(), "GOTRACEBACK=crash"), Files: []*os.File{os.Stdin, os.Stdout, f}}
		lastRebootTime := time.Now()
		p, err := os.StartProcess(binPath, args, execSpec)
		if err != nil {
			gLog.e("start worker error:%s", err)
			return
		}
		d.proc = p
		processState, err := p.Wait()
		if err != nil {
			gLog.e("wait process error:%s", err)
		}

		if processState != nil {
			exitCode := processState.ExitCode()
			gLog.i("worker process exited with code: %d", exitCode)

			if exitCode == 9 {
				gLog.i("worker process update with code: %d", exitCode)
				// os.Exit(9) // old client installed system service will not auto restart. fuck
			}
		}
		// Write the current time to the end of the dump file
		currentTime := time.Now().Format("2006-01-02 15:04:05")
		_, err = f.WriteString("\nProcess ended at: " + currentTime + "\n")
		if err != nil {
			gLog.e("Failed to write time to dump file: %s", err)
		}

		f.Close()
		time.Sleep(time.Second)
		err = os.Rename(tmpDump, dumpFile)
		if err != nil {
			gLog.e("rename dump error:%s", err)
		}
		if !d.running {
			return
		}
		if time.Since(lastRebootTime) < time.Second*10 {
			gLog.e("worker stop, restart it after 10s")
			time.Sleep(time.Second * 10)
		}

	}
}

func (d *daemon) Control(ctrlComm string, exeAbsPath string, args []string) error {
	svcConfig := getServiceConfig(exeAbsPath, args)

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

func getServiceConfig(exeAbsPath string, args []string) *service.Config {
	config := &service.Config{
		Name:        ProductName,
		DisplayName: ProductName,
		Description: ProductName,
		Executable:  exeAbsPath,
		Arguments:   args,
		Option:      make(map[string]interface{}),
	}

	if runtime.GOOS == "windows" {
		setupWindowsConfig(config)
	} else {
		setupLinuxConfig(config)
	}

	return config
}

func setupWindowsConfig(config *service.Config) {
	failureActions := []map[string]interface{}{
		{
			"Type":  "restart",
			"Delay": "10000",
		},
		{
			"Type":  "restart",
			"Delay": "10000",
		},
		{
			"Type":  "restart",
			"Delay": "10000",
		},
	}

	config.Option = map[string]interface{}{
		"OnFailure":            "restart",
		"OnFailureDelay":       "10s",
		"OnFailureResetPeriod": "3600",
		"FailureActions":       failureActions,
		"DelayedAutoStart":     true,
	}
}

func setupLinuxConfig(config *service.Config) {
	config.Option = map[string]interface{}{
		"Restart":           "always",
		"RestartSec":        "10",
		"StartLimitBurst":   64,
		"SuccessExitStatus": "1 2 8 SIGKILL",
	}
}
