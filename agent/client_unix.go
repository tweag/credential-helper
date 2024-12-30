//go:build unix

package agent

import (
	"os"
	"syscall"
)

func procAttrForAgentProcess() *os.ProcAttr {
	sys := syscall.SysProcAttr{
		Setpgid: true,
	}
	return &os.ProcAttr{
		Sys: &sys,
	}
}
