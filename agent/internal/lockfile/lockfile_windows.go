//go:build windows

package lockfile

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx   = kernel32.NewProc("LockFileEx")
	procUnlockFileEx = kernel32.NewProc("UnlockFileEx")
)

const (
	reserved                  = 0
	allBytes                  = ^uint32(0)
	LOCKFILE_FAIL_IMMEDIATELY = 1
	LOCKFILE_EXCLUSIVE_LOCK   = 2
)

func lockFileEx(file syscall.Handle, flags uint32, reserved uint32, bytesLow uint32, bytesHigh uint32, overlapped *syscall.Overlapped) error {
	r1, _, e1 := procLockFileEx.Call(uintptr(file), uintptr(flags), uintptr(reserved), uintptr(bytesLow), uintptr(bytesHigh), uintptr(unsafe.Pointer(overlapped)))
	if r1 == 0 {
		if e1 != nil {
			return e1
		}
		return syscall.EINVAL
	}
	return nil
}

func unlockFileEx(file syscall.Handle, reserved uint32, bytesLow uint32, bytesHigh uint32, overlapped *syscall.Overlapped) (err error) {
	r1, _, e1 := procUnlockFileEx.Call(uintptr(file), uintptr(reserved), uintptr(bytesLow), uintptr(bytesHigh), uintptr(unsafe.Pointer(overlapped)), 0)
	if r1 == 0 {
		return e1
	}
	return nil
}

// on windows, use LockFileEx to lock the file.
func lock(file *os.File) error {
	ol := new(syscall.Overlapped)

	if err := lockFileEx(syscall.Handle(file.Fd()), LOCKFILE_EXCLUSIVE_LOCK|LOCKFILE_FAIL_IMMEDIATELY, reserved, allBytes, allBytes, ol); err != nil {
		return fmt.Errorf("acquiring agent lock file (agent already running?): %w", err)
	}
	if _, err := file.WriteString(fmt.Sprintf("%d", os.Getpid())); err != nil {
		return fmt.Errorf("writing pid to agent lock file: %w", err)
	}
	return nil
}

// on windows, use UnlockFileEx to unlock the file.
func unlock(file *os.File) error {
	ol := new(syscall.Overlapped)
	return unlockFileEx(syscall.Handle(file.Fd()), reserved, allBytes, allBytes, ol)
}
