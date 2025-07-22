//go:build linux

package logging

import (
	"fmt"
	"log"
	"log/syslog"
)

var syslogger *syslog.Writer

func Init() {
	var err error
	syslogger, err = syslog.New(syslog.LOG_DEBUG, "")
	if err != nil {
		log.Printf("Failed to initialize syslog: %v", err)
	}
}

// On Linux, we attempt to write to the syslog.
func SyslogDebugf(format string, args ...any) {
	if level < LogLevelDebug {
		return
	}
	if syslogger == nil {
		Init()
	}
	syslogger.Debug(fmt.Sprintf(format, args...))
}
