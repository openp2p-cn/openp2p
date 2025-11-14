package openp2p

import (
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

type LogLevel int32

var gLog *logger

const (
	LvDev   LogLevel = -1
	LvDEBUG LogLevel = 0
	LvINFO  LogLevel = 1
	LvWARN  LogLevel = 2
	LvERROR LogLevel = 3
)

const logFileNames string = ".log"

var loglevel = map[LogLevel]string{
	LvDEBUG: "DEBUG",
	LvINFO:  "INFO",
	LvWARN:  "WARN",
	LvERROR: "ERROR",
	LvDev:   "Dev",
}

const (
	LogFile    = 1
	LogConsole = 1 << 1
)

type logger struct {
	logger           *log.Logger
	files            *os.File
	level            atomic.Int32
	logDir           string
	mtx              sync.Mutex
	lineEnding       string
	pid              int
	maxLogSize       atomic.Int64
	mode             int
	stdLogger        *log.Logger
	checkFileRunning bool
}

func NewLogger(path string, filePrefix string, level LogLevel, maxLogSize int64, mode int) *logger {
	logdir := filepath.Join(path, "log")
	if err := os.MkdirAll(logdir, 0755); err != nil && mode&LogFile != 0 {
		return nil
	}
	logFilePath := filepath.Join(logdir, filePrefix+logFileNames)
	f, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil && mode&LogFile != 0 {
		log.Fatal(err)
	}
	stdLog := log.New(f, "", log.LstdFlags|log.Lmicroseconds)
	le := "\n"
	if runtime.GOOS == "windows" {
		le = "\r\n"
	}
	pLog := &logger{logger: stdLog,
		files:      f,
		logDir:     logdir,
		lineEnding: le,
		pid:        os.Getpid(),
		mode:       mode,
		stdLogger:  log.New(os.Stdout, "", 0)}
	pLog.setMaxSize(maxLogSize)
	pLog.setLevel(level)
	pLog.stdLogger.SetFlags(log.LstdFlags | log.Lmicroseconds)
	go pLog.checkFile()
	return pLog
}

func (l *logger) setLevel(level LogLevel) {
	l.level.Store(int32(level))
}

func (l *logger) setMaxSize(size int64) {
	l.maxLogSize.Store(size)
}

func (l *logger) setMode(mode int) {
	l.mtx.Lock()
	defer l.mtx.Unlock()
	l.mode = mode
}

func (l *logger) close() {
	l.checkFileRunning = false
	l.files.Close()
}

func (l *logger) checkFile() {
	if l.maxLogSize.Load() <= 0 {
		return
	}
	l.checkFileRunning = true
	ticker := time.NewTicker(time.Minute)
	for l.checkFileRunning {
		select {
		case <-ticker.C:
			f, e := l.files.Stat()
			if e != nil {
				continue
			}
			if f.Size() <= l.maxLogSize.Load() {
				continue
			}
			l.mtx.Lock()
			l.files.Close()
			fname := f.Name()
			backupPath := filepath.Join(l.logDir, fname+".0")
			err := os.Remove(backupPath)
			if err != nil {
				log.Println("remove openp2p.log0 error:", err)
			}
			if err = os.Rename(filepath.Join(l.logDir, fname), backupPath); err != nil {
				log.Println("rename openp2p.log error:", err)
			}
			if newFile, e := os.OpenFile(filepath.Join(l.logDir, fname), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644); e == nil {

				l.logger.SetOutput(newFile)
				l.files = newFile
				l.mtx.Unlock()
			}
		case <-time.After(time.Second * 1):
		}

	}
}

func (l *logger) Printf(level LogLevel, format string, params ...interface{}) {
	if level < LogLevel(l.level.Load()) {
		return
	}
	l.mtx.Lock()
	defer l.mtx.Unlock()

	pidAndLevel := []interface{}{l.pid, loglevel[level]}
	params = append(pidAndLevel, params...)
	if l.mode&LogFile != 0 {
		l.logger.Printf("%d %s "+format+l.lineEnding, params...)
	}
	if l.mode&LogConsole != 0 {
		l.stdLogger.Printf("%d %s "+format+l.lineEnding, params...)
	}
}

func (l *logger) Println(level LogLevel, params ...interface{}) {
	if level < LogLevel(l.level.Load()) {
		return
	}
	l.mtx.Lock()
	defer l.mtx.Unlock()
	pidAndLevel := []interface{}{l.pid, " ", loglevel[level], " "}
	params = append(pidAndLevel, params...)
	params = append(params, l.lineEnding)
	if l.mode&LogFile != 0 {
		l.logger.Print(params...)
	}
	if l.mode&LogConsole != 0 {
		l.stdLogger.Print(params...)
	}
}

func (l *logger) d(format string, params ...interface{}) {
	l.Printf(LvDEBUG, format, params...)
}

func (l *logger) i(format string, params ...interface{}) {
	l.Printf(LvINFO, format, params...)
}

func (l *logger) w(format string, params ...interface{}) {
	l.Printf(LvWARN, format, params...)
}

func (l *logger) e(format string, params ...interface{}) {
	l.Printf(LvERROR, format, params...)
}

func (l *logger) dev(format string, params ...interface{}) {
	l.Printf(LvDev, format, params...)
}
