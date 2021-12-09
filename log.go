package main

import (
	"log"
	"os"
	"runtime"
	"sync"
	"time"
)

// LogLevel ...
type LogLevel int

var gLog *V8log

// LevelDEBUG ...
const (
	LevelDEBUG LogLevel = iota
	LevelINFO
	LevelWARN
	LevelERROR
)

var (
	logFileNames map[LogLevel]string
	loglevel     map[LogLevel]string
)

func init() {
	logFileNames = make(map[LogLevel]string)
	loglevel = make(map[LogLevel]string)
	logFileNames[0] = ".log"
	loglevel[LevelDEBUG] = "DEBUG"
	loglevel[LevelINFO] = "INFO"
	loglevel[LevelWARN] = "WARN"
	loglevel[LevelERROR] = "ERROR"

}

const (
	LogFile = iota
	LogConsole
	LogFileAndConsole
)

// V8log ...
type V8log struct {
	loggers    map[LogLevel]*log.Logger
	files      map[LogLevel]*os.File
	llevel     LogLevel
	stopSig    chan bool
	logDir     string
	mtx        *sync.Mutex
	stoped     bool
	lineEnding string
	pid        int
	maxLogSize int64
	mode       int
}

// InitLogger ...
func InitLogger(path string, filePrefix string, level LogLevel, maxLogSize int64, mode int) *V8log {
	logger := make(map[LogLevel]*log.Logger)
	openedfile := make(map[LogLevel]*os.File)
	var (
		logdir string
	)
	if path == "" {
		logdir = "log/"
	} else {
		logdir = path + "/log/"
	}
	os.MkdirAll(logdir, 0777)
	for l := range logFileNames {
		logFilePath := logdir + filePrefix + logFileNames[l]
		f, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			log.Fatal(err)
		}
		os.Chmod(logFilePath, 0666)
		openedfile[l] = f
		logger[l] = log.New(f, "", log.LstdFlags)
	}
	var le string
	if runtime.GOOS == "windows" {
		le = "\r\n"
	} else {
		le = "\n"
	}
	pLog := &V8log{logger, openedfile, level, make(chan bool, 10), logdir, &sync.Mutex{}, false, le, os.Getpid(), maxLogSize, mode}
	go pLog.checkFile()
	return pLog
}

// UninitLogger ...
func (vl *V8log) UninitLogger() {
	if !vl.stoped {
		vl.stoped = true
		close(vl.stopSig)
		for l := range logFileNames {
			if l >= vl.llevel {
				vl.files[l].Close()
			}
		}
	}
}

func (vl *V8log) checkFile() {
	if vl.maxLogSize <= 0 {
		return
	}
	ticker := time.NewTicker(time.Minute)
	for {
		select {
		case <-ticker.C:
			vl.mtx.Lock()
			for l, logFile := range vl.files {
				f, e := logFile.Stat()
				if e != nil {
					break
				}
				if f.Size() <= vl.maxLogSize {
					break
				}
				logFile.Close()
				fname := f.Name()
				backupPath := vl.logDir + fname + ".0"
				os.Remove(backupPath)
				os.Rename(vl.logDir+fname, backupPath)
				newFile, e := os.OpenFile(vl.logDir+fname, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
				if e == nil {
					vl.loggers[l].SetOutput(newFile)
					vl.files[l] = newFile
				}

			}
			vl.mtx.Unlock()
		case <-vl.stopSig:
		}
		if vl.stoped {
			break
		}
	}
}

// Printf  Warning: report error log depends on this Print format.
func (vl *V8log) Printf(level LogLevel, format string, params ...interface{}) {
	vl.mtx.Lock()
	defer vl.mtx.Unlock()
	if vl.stoped {
		return
	}
	if level < vl.llevel {
		return
	}
	pidAndLevel := []interface{}{vl.pid, loglevel[level]}
	params = append(pidAndLevel, params...)
	if vl.mode == LogFile || vl.mode == LogFileAndConsole {
		vl.loggers[0].Printf("%d %s "+format+vl.lineEnding, params...)
	}
	if vl.mode == LogConsole || vl.mode == LogFileAndConsole {
		log.Printf("%d %s "+format+vl.lineEnding, params...)
	}
}

// Println ...
func (vl *V8log) Println(level LogLevel, params ...interface{}) {
	vl.mtx.Lock()
	defer vl.mtx.Unlock()
	if vl.stoped {
		return
	}
	if level < vl.llevel {
		return
	}
	pidAndLevel := []interface{}{vl.pid, " ", loglevel[level], " "}
	params = append(pidAndLevel, params...)
	params = append(params, vl.lineEnding)
	if vl.mode == LogFile || vl.mode == LogFileAndConsole {
		vl.loggers[0].Print(params...)
	}
	if vl.mode == LogConsole || vl.mode == LogFileAndConsole {
		log.Print(params...)
	}
}
