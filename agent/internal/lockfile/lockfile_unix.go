//go:build unix

package lockfile

import (
	"fmt"
	"os"
	"syscall"
)

// on unix, use flock to lock the file.
func lock(file *os.File) error {
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fmt.Errorf("acquiring agent lock file (agent already running?): %w", err)
	}
	if _, err := file.WriteString(fmt.Sprintf("%d", os.Getpid())); err != nil {
		return fmt.Errorf("writing pid to agent lock file: %w", err)
	}
	return nil
}

// on unix, use flock to unlock the file.
func unlock(file *os.File) error {
	return syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
}
