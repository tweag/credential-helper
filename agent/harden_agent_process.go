package agent

import "syscall"

func hardenAgentProcess() {
	// sets up the umask of the process early to restrict the
	// access on created files (like unix domain sockets).
	// This umask clears the access bits for group and others,
	// restricting any file access to the owner.
	_ = syscall.Umask(0o077)
}
