//go:build unix

package agent

import (
	"os"
	"syscall"
)

func procAttrForAgentProcess(stdout, stderr *os.File) *os.ProcAttr {
	sys := syscall.SysProcAttr{
		Setpgid: true,
	}
	return &os.ProcAttr{
		Sys:   &sys,
		Files: []*os.File{nil, stdout, stderr},
	}
}
