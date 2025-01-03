package logging

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type LogLevel int

const (
	LogLevelOff LogLevel = iota
	LogLevelBasic
	LogLevelDebug
)

var level = LogLevelOff

func SetLevel(l LogLevel) {
	level = l
}

func GetLevel() LogLevel {
	return level
}

func FromString(s string) LogLevel {
	if numericLogLevel, err := strconv.Atoi(s); err == nil {
		return boundedLogLevel(numericLogLevel)
	}
	switch strings.ToLower(s) {
	case "off":
		return LogLevelOff
	case "basic":
		return LogLevelBasic
	case "debug":
		return LogLevelDebug
	}

	return LogLevelOff
}

func Debugf(format string, args ...any) {
	if level >= LogLevelDebug {
		fPrintOut(format, args...)
	}
}

func Basicf(format string, args ...any) {
	if level >= LogLevelBasic {
		fPrintOut(format, args...)
	}
}

func Errorf(format string, args ...any) {
	fPrintOut(format, args...)
}

func Fatalf(format string, args ...any) {
	fPrintOut(format, args...)
	os.Exit(1)
}

func boundedLogLevel(numericLevel int) LogLevel {
	if numericLevel < 0 {
		return LogLevelOff
	}
	if numericLevel > 2 {
		return LogLevelDebug
	}
	return LogLevel(numericLevel)
}

func fPrintOut(format string, args ...any) {
	fmt.Fprintf(os.Stderr, fmtWithNewline(format), args...)
}

func fmtWithNewline(format string) string {
	if !strings.HasSuffix(format, "\n") {
		return format + "\n"
	}
	return format
}
