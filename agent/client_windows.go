//go:build windows

package agent

import (
	"os"
	"syscall"
)

func procAttrForAgentProcess() *os.ProcAttr {
	return &os.ProcAttr{
		Files: []*os.File{nil, nil, nil},
		Sys: &syscall.SysProcAttr{
			CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
		},
	}
}
