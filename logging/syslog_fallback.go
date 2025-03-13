//go:build !linux

package logging

// Outside of linux, we aren't guaranteed to have syslog available.
// So we'll just fall back to the standard logging functions.
func SyslogDebugf(format string, args ...any) {
	Debugf(format, args...)
}
