package installer

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/tweag/credential-helper/agent/locate"
)

func InstallerProcess() {
	path, havePathFromEnv := os.LookupEnv("CREDENTIAL_HELPER_INSTALLER_SOURCE")
	if !havePathFromEnv {
		var getOwnPathErr error
		path, getOwnPathErr = os.Executable()
		if getOwnPathErr != nil {
			fatalFmt("finding path to own executable: %v", getOwnPathErr)
		}
	}
	var derefSymlinkErr error
	path, derefSymlinkErr = filepath.EvalSymlinks(path)
	if derefSymlinkErr != nil {
		fatalFmt("following source path symlink %s: %v", path, derefSymlinkErr)
	}
	if _, err := os.Stat(path); err != nil {
		fatalFmt("checking if install source exists %s: %v", path, err)
	}
	var absolutizeErr error
	path, absolutizeErr = filepath.Abs(path)
	if absolutizeErr != nil {
		fatalFmt("getting absolute path: %v", absolutizeErr)
	}
	if err := locate.SetupEnvironment(); err != nil {
		fatalFmt("setting up environment: %v", err)
	}
	destination, err := install(path)
	if err != nil {
		fatalFmt("Failed to install %s: %v", path, err)
	}
	fmt.Println(destination)
}

func install(credentialHelperBin string) (string, error) {
	destination := locate.CredentialHelper()
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return "", fmt.Errorf("making install destination directory: %w", err)
	}
	destination = locate.LookupPathEnv("CREDENTIAL_HELPER_INSTALLER_DESTINATION", destination, false)
	// NOTE: this stop-cleanup-install procedure is merely best effort.
	// It is clearly prone to race conditions
	// As an improvement,
	// The installer could take the agent pid lock before making changes.
	shutdownOut := attemptAgentShutdown(destination)
	if len(shutdownOut) > 0 {
		fmt.Fprintf(os.Stderr, "Shutting down old agent before uninstall: %s", shutdownOut)
	}

	if err := os.Remove(destination); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Removing old agent: %v", err)
	}
	return destination, hardlinkOrCopy(credentialHelperBin, destination)
}

func hardlinkOrCopy(src, dst string) error {
	if err := os.Link(src, dst); err == nil {
		return nil
	}
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("opening source file for copying: %w", err)
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("opening destination file for copying: %w", err)
	}
	defer dstFile.Close()
	_, err = io.Copy(dstFile, srcFile)
	return err
}

func attemptAgentShutdown(agentPath string) string {
	out, err := exec.Command(agentPath, "agent-shutdown").CombinedOutput()
	if err == nil {
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

func WantInstallerRun() bool {
	// To avoid recursively starting the installer
	// when trying to stop the agent,
	// we unset this env var after checking for it.
	want := os.Getenv("CREDENTIAL_HELPER_INSTALLER_RUN") == "1"
	_ = os.Unsetenv("CREDENTIAL_HELPER_INSTALLER_RUN")
	return want
}
