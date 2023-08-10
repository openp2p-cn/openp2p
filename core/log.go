package openp2p

import (
	"log"
	"os"
	"runtime"
	"sync"
	"time"
)

type LogLevel int

var gLog *logger

const (
	LvDEBUG LogLevel = iota
	LvINFO
	LvWARN
	LvERROR
)

var (
	logFileNames map[LogLevel]string
	loglevel     map[LogLevel]string
)

func init() {
	logFileNames = make(map[LogLevel]string)
	loglevel = make(map[LogLevel]string)
	logFileNames[0] = ".log"
	loglevel[LvDEBUG] = "DEBUG"
	loglevel[LvINFO] = "INFO"
	loglevel[LvWARN] = "WARN"
	loglevel[LvERROR] = "ERROR"

}

const (
	LogFile = 1 << iota
	LogConsole
)

type logger struct {
	loggers    map[LogLevel]*log.Logger
	files      map[LogLevel]*os.File
	level      LogLevel
	logDir     string
	mtx        *sync.Mutex
	lineEnding string
	pid        int
	maxLogSize int64
	mode       int
	stdLogger  *log.Logger
}

func NewLogger(path string, filePrefix string, level LogLevel, maxLogSize int64, mode int) *logger {
	loggers := make(map[LogLevel]*log.Logger)
	logfiles := make(map[LogLevel]*os.File)
	var (
		logdir string
	)
	if path == "" {
		logdir = "log/"
	} else {
		logdir = path + "/log/"
	}
	os.MkdirAll(logdir, 0777)
	for lv := range logFileNames {
		logFilePath := logdir + filePrefix + logFileNames[lv]
		f, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			log.Fatal(err)
		}
		os.Chmod(logFilePath, 0644)
		logfiles[lv] = f
		loggers[lv] = log.New(f, "", log.LstdFlags|log.Lmicroseconds)
	}
	var le string
	if runtime.GOOS == "windows" {
		le = "\r\n"
	} else {
		le = "\n"
	}
	pLog := &logger{loggers, logfiles, level, logdir, &sync.Mutex{}, le, os.Getpid(), maxLogSize, mode, log.New(os.Stdout, "", 0)}
	pLog.stdLogger.SetFlags(log.LstdFlags | log.Lmicroseconds)
	go pLog.checkFile()
	return pLog
}

func (l *logger) setLevel(level LogLevel) {
	l.mtx.Lock()
	defer l.mtx.Unlock()
	l.level = level
}
func (l *logger) setMode(mode int) {
	l.mtx.Lock()
	defer l.mtx.Unlock()
	l.mode = mode
}

func (l *logger) checkFile() {
	if l.maxLogSize <= 0 {
		return
	}
	ticker := time.NewTicker(time.Minute)
	for {
		select {
		case <-ticker.C:
			l.mtx.Lock()
			for lv, logFile := range l.files {
				f, e := logFile.Stat()
				if e != nil {
					continue
				}
				if f.Size() <= l.maxLogSize {
					continue
				}
				logFile.Close()
				fname := f.Name()
				backupPath := l.logDir + fname + ".0"
				os.Remove(backupPath)
				os.Rename(l.logDir+fname, backupPath)
				newFile, e := os.OpenFile(l.logDir+fname, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
				if e == nil {
					l.loggers[lv].SetOutput(newFile)
					l.files[lv] = newFile
				}
			}
			l.mtx.Unlock()
		}
	}
}

func (l *logger) Printf(level LogLevel, format string, params ...interface{}) {
	l.mtx.Lock()
	defer l.mtx.Unlock()
	if level < l.level {
		return
	}
	pidAndLevel := []interface{}{l.pid, loglevel[level]}
	params = append(pidAndLevel, params...)
	if l.mode & LogFile != 0 {
		l.loggers[0].Printf("%d %s "+format+l.lineEnding, params...)
	}
	if l.mode & LogConsole != 0 {
		l.stdLogger.Printf("%d %s "+format+l.lineEnding, params...)
	}
}

func (l *logger) Println(level LogLevel, params ...interface{}) {
	l.mtx.Lock()
	defer l.mtx.Unlock()
	if level < l.level {
		return
	}
	pidAndLevel := []interface{}{l.pid, " ", loglevel[level], " "}
	params = append(pidAndLevel, params...)
	params = append(params, l.lineEnding)
	if l.mode & LogFile != 0 {
		l.loggers[0].Print(params...)
	}
	if l.mode & LogConsole != 0 {
		l.stdLogger.Print(params...)
	}
}
