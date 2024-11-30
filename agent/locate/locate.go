package locate

import (
	"crypto/md5"
	"fmt"
	"os"
	"path"
	"runtime"

	"github.com/tweag/credential-helper/api"
)

func Base() (string, error) {
	if base, ok := os.LookupEnv(api.CredentialHelperInstallBase); ok {
		return base, nil
	}

	// In Bazel integration tests we can't (and shouldn't)
	// touch the user's home directory.
	// Instead, we operate under $TEST_TMPDIR
	var cacheDir string
	var err error
	cacheDir, ok := os.LookupEnv("TEST_TMPDIR")
	if !ok {
		// On a normal run, we want to operate in $HOME/.cache (or $XDG_CACHE_HOME)
		cacheDir, err = os.UserCacheDir()
		if err != nil {
			return "", err
		}
	}

	workspaceDirectory, err := workspaceDirectory()
	if err != nil {
		return "", err
	}

	return path.Join(cacheDir, "tweag-credential-helper", installBasePathComponent(workspaceDirectory)), nil
}

func Bin() (string, error) {
	base, err := Base()
	if err != nil {
		return "", err
	}
	return path.Join(base, "bin"), nil
}

func Run() (string, error) {
	base, err := Base()
	if err != nil {
		return "", err
	}
	return path.Join(base, "run"), nil
}

func CredentialHelper() (string, error) {
	if path, ok := os.LookupEnv(api.CredentialHelperBin); ok {
		return path, nil
	}
	bin, err := Bin()
	if err != nil {
		return "", err
	}
	filename := "credential-helper"
	if runtime.GOOS == "windows" {
		filename += ".exe"
	}
	return path.Join(bin, filename), nil
}

func AgentPaths() (string, string, error) {
	socketPath, haveSocketPathFromEnv := os.LookupEnv(api.AgentSocketPath)
	pidPath, havePidPathFromEnv := os.LookupEnv(api.AgentPidPath)
	run, err := Run()
	if err != nil {
		return "", "", err
	}
	if !haveSocketPathFromEnv {
		socketPath = path.Join(run, "agent.sock")
		if len(socketPath) >= 108 {
			// In many environments
			// we are not allowed to use
			// a socket path longer than 108 bytes
			//
			// in those cases, fall back to a unique
			// abstract uds (prefixed with @ in Go)
			workspaceDirectory, err := workspaceDirectory()
			if err != nil {
				return "", "", err
			}
			socketPath = "@" + installBasePathComponent(workspaceDirectory)
		}

	}
	if !havePidPathFromEnv {
		pidPath = path.Join(run, "agent.pid")
	}

	return socketPath, pidPath, nil
}

func installBasePathComponent(workspaceDirectory string) string {
	return fmt.Sprintf("%x", md5.Sum([]byte(workspaceDirectory)))
}

func workspaceDirectory() (string, error) {
	workspaceDirectory, ok := os.LookupEnv("BUILD_WORKSPACE_DIRECTORY")
	if ok {
		return workspaceDirectory, nil
	}
	workspaceDirectory, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return workspaceDirectory, nil
}
