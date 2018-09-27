package decoder

import (
	stdlog "log"
	"os"
	"sync"
)

type Logger interface {
	Println(...interface{})
	Printf(string, ...interface{})
}

var (
	debug bool
	log   Logger
	logMu sync.RWMutex
)

func init() {
	log = stdlog.New(os.Stdout, "", stdlog.LstdFlags)
}

func SetLogger(l Logger) {
	logMu.Lock()
	log = l
	logMu.Unlock()
}

func Debug() {
	logMu.Lock()
	debug = true
	logMu.Unlock()
}

func DisableDebug() {
	logMu.Lock()
	debug = false
	logMu.Unlock()
}

func logPrintln(v ...interface{}) {
	logMu.RLock()
	log.Println(v...)
	logMu.RUnlock()
}

func logPrintf(f string, v ...interface{}) {
	logMu.RLock()
	log.Printf(f, v...)
	logMu.RUnlock()
}

func debugPrintln(v ...interface{}) {
	if debug {
		logPrintln(v...)
	}
}

func debugPrintf(f string, v ...interface{}) {
	if debug {
		logPrintf(f, v...)
	}
}
