package main

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"

	"github.com/bazelbuild/rules_go/go/runfiles"
	"github.com/tweag/credential-helper/agent/locate"
)

func main() {
	pathFromEnv := os.Getenv("CREDENTIAL_HELPER_INSTALLER_SOURCE")
	path, err := runfiles.Rlocation(pathFromEnv)
	if err != nil {
		fatalFmt("Failed to find %s: %v", pathFromEnv, err)
	}

	if _, err := os.Stat(path); err != nil {
		fatalFmt("Failed to stat %s: %v", path, err)
	}
	destination, err := install(path)
	if err != nil {
		fatalFmt("Failed to install %s: %v", path, err)
	}
	fmt.Println(destination)
}

func install(credentialHelperBin string) (string, error) {
	destination, err := locate.CredentialHelper()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(path.Dir(destination), 0o755); err != nil {
		return "", err
	}
	shutdownOut := attemptAgentShutdown(destination)
	if len(shutdownOut) > 0 {
		fmt.Fprintf(os.Stderr, "Shutting down old agent before uninstall: %s", shutdownOut)
	}

	_ = os.Remove(destination)
	return destination, os.Link(credentialHelperBin, destination)
}

func attemptAgentShutdown(agentPath string) string {
	out, err := exec.Command(agentPath, "agent-shutdown").CombinedOutput()
	if err != nil {
		return ""
	}
	return string(out)
}

func fatalFmt(format string, args ...any) {
	if !strings.HasSuffix(format, "\n") {
		format += "\n"
	}
	fmt.Fprintf(os.Stderr, format, args...)
	os.Exit(1)
}
