//go:build linux

package logging

import "log/syslog"

// On Linux, we attempt to write to the syslog.
func SyslogDebugf(format string, args ...any) {
	if level < LogLevelDebug {
		return
	}
	syslogger, err := syslog.NewLogger(syslog.LOG_DEBUG, 0)
	if err == nil {
		allArgs := append([]any{format}, args...)
		syslogger.Println(allArgs...)
	}
}
