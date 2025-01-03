//go:build windows

package agent

import (
	"os"
	"syscall"
)

func procAttrForAgentProcess(stdout, stderr *os.File) *os.ProcAttr {
	return &os.ProcAttr{
		Files: []*os.File{nil, stdout, stderr},
		Sys: &syscall.SysProcAttr{
			CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
		},
	}
}
