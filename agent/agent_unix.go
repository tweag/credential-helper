//go:build unix

package agent

import (
	"fmt"
	"os"
	"syscall"
)

func hardenAgentProcess() {
	// sets up the umask of the process early to restrict the
	// access on created files (like unix domain sockets).
	// This umask clears the access bits for group and others,
	// restricting any file access to the owner.
	_ = syscall.Umask(0o077)
}

func hardenSocketDir(socketDir string) error {
	if err := os.Chown(socketDir, os.Getuid(), os.Getgid()); err != nil {
		return fmt.Errorf("chown socket directory: %w", err)
	}
	// set the socket directory to be only accessible by the owner
	// this is to prevent other users from connecting to the agent
	if err := os.Chmod(socketDir, 0o700); err != nil {
		return fmt.Errorf("chmod socket directory: %w", err)
	}
	return nil
}
