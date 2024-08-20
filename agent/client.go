package agent

import (
	"os"
	"syscall"
)

func LaunchAgentProcess() error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	sys := syscall.SysProcAttr{
		Setpgid: true,
	}
	procAttr := &os.ProcAttr{
		Sys: &sys,
	}
	proc, err := os.StartProcess(self, []string{self, "agent"}, procAttr)
	if err != nil {
		return err
	}
	return proc.Release()
}
