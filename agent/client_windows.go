//go:build windows

package agent

import (
	"os"
	"syscall"
)

const (
	DETACHED_PROCESS         = 0x00000008
	CREATE_NEW_PROCESS_GROUP = 0x00000200
)

func procAttrForAgentProcess(stdout, stderr *os.File) *os.ProcAttr {
	return &os.ProcAttr{
		Files: []*os.File{nil, stdout, stderr},
		Sys: &syscall.SysProcAttr{
			CreationFlags: DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP,
		},
	}
}
