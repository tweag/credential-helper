package locate

import (
	"os"
	"path"
	"runtime"

	"github.com/tweag/credential-helper/api"
)

func Base() (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}

	return path.Join(cacheDir, "credential-helper"), nil
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
	}
	if !havePidPathFromEnv {
		pidPath = path.Join(run, "agent.pid")
	}

	return socketPath, pidPath, nil
}
