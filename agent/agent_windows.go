//go:build windows

package agent

func hardenAgentProcess() {
	// the unix version of this function sets up the umask of the process to limit who can
	// access the agent's files (and socket).
	// On Windows, we don't have the concept of umask, so this function is a no-op.
}

func hardenSocketDir(socketDir string) error {
	// the unix version of this function changes the ownership and permissions of the socket directory
	// to limit who can access the agent's socket.
	// On Windows, this would require changing the ACLs of the directory, which is more complex.
	// For now, we don't do this on Windows.
	return nil
}
