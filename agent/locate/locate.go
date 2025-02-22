package locate

import (
	"crypto/md5"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/tweag/credential-helper/api"
)

// SetupEnvironment has to be called early in
// the agent and client process.
// It changes the working directory and exports
// environment variables to ensure
// a consistent working environment.
func SetupEnvironment() error {
	originalWorkingDirectory, err := os.Getwd()
	if err != nil {
		return err
	}
	if err := os.Setenv(api.OriginalWorkingDirectoryEnv, originalWorkingDirectory); err != nil {
		return err
	}

	workspacePath, err := setupWorkspaceDirectory()
	if err != nil {
		return err
	}

	workdirPath, err := setupWorkdir(workspacePath)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(workdirPath, os.ModePerm); err != nil {
		return err
	}

	return os.Chdir(workdirPath)
}

func setupWorkspaceDirectory() (string, error) {
	// try helper-specific workspace directory env var
	workspacePath, haveWorkspacePath := os.LookupEnv(api.WorkspaceEnv)
	if haveWorkspacePath {
		return workspacePath, nil
	}

	// maybe we are running under Bazel?
	// in that case $BUILD_WORKSPACE_DIRECTORY is the root of the workspace
	workspacePath, haveWorkspacePath = os.LookupEnv("BUILD_WORKSPACE_DIRECTORY")

	if !haveWorkspacePath {
		// as a last resort
		// assume that current working directory
		// is the Bazel workspace
		var lookupErr error
		workspacePath, lookupErr = os.Getwd()
		if lookupErr != nil {
			return "", lookupErr
		}
	}

	workspacePath = filepath.FromSlash(workspacePath)
	return workspacePath, os.Setenv(api.WorkspaceEnv, workspacePath)
}

func setupWorkdir(workspacePath string) (string, error) {
	// try helper-specifc workdir directory env var
	workdirPath, haveWorkdirPath := os.LookupEnv(api.WorkdirEnv)
	if haveWorkdirPath {
		return workdirPath, nil
	}
	// assume that helper workdir
	// is ${cache_dir}/tweag-credential-helper/${workdir_hash}
	cacheDir := cacheDir()
	workdirPath = filepath.Join(cacheDir, "tweag-credential-helper", workdirHash(workspacePath))

	workdirPath = filepath.FromSlash(workdirPath)
	return workdirPath, os.Setenv(api.WorkdirEnv, workdirPath)
}

func LookupPathEnv(key, fallback string, shortPath bool) string {
	unexpanded, ok := os.LookupEnv(key)
	if !ok {
		unexpanded = fallback
	}
	return expandPath(unexpanded, shortPath)
}

func Workdir() string {
	return os.Getenv(api.WorkdirEnv)
}

func Bin() string {
	return filepath.Join(Workdir(), "bin")
}

func Run() string {
	return filepath.Join(Workdir(), "run")
}

func CredentialHelper() string {
	filename := "credential-helper"
	if runtime.GOOS == "windows" {
		filename += ".exe"
	}

	return LookupPathEnv(api.CredentialHelperBin, filepath.Join("%workdir%", "bin", filename), false)
}

func AgentPaths() (string, string) {
	socketPath := LookupPathEnv(api.AgentSocketPath, filepath.Join("%workdir%", "run", "agent.sock"), true)
	pidPath := LookupPathEnv(api.AgentPidPath, filepath.Join("%workdir%", "run", "agent.pid"), false)

	return socketPath, pidPath
}

func RemapToOriginalWorkingDirectory(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	prefix := os.Getenv(api.OriginalWorkingDirectoryEnv)
	return filepath.Join(prefix, p)
}

func tmpDir() string {
	// In Bazel integration tests we can't (and shouldn't)
	// touch /tmp
	// Instead, we operate under $TEST_TMPDIR
	if cacheDir, ok := os.LookupEnv("TEST_TMPDIR"); ok {
		return cacheDir
	}
	return os.TempDir()
}

func cacheDir() string {
	// In Bazel integration tests we can't (and shouldn't)
	// touch the user's home directory.
	// Instead, we operate under $TEST_TMPDIR
	if cacheDir, ok := os.LookupEnv("TEST_TMPDIR"); ok {
		return cacheDir
	}
	// On a normal run, we want to operate in $HOME/.cache (or $XDG_CACHE_HOME)
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return tmpDir()
	}
	return cacheDir
}

func homeDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	// if we are in an environment where the current
	// user doesn't have a resolvable
	// home dir, it is probably safer to write to a temp
	// dir instead.
	return tmpDir()
}

func workdirHash(workspaceDirectory string) string {
	if runtime.GOOS == "windows" {
		// Windows filesystems are case-insensitive
		// And some APIs in Windows return paths in lowercase
		// while others return them in mixed case.
		// Normalize for correctness.
		workspaceDirectory = strings.ToLower(workspaceDirectory)
	}
	return fmt.Sprintf("%x", md5.Sum([]byte(workspaceDirectory)))
}

func expandPath(input string, shortPath bool) string {
	var prefix, suffix string
	var canShortPath bool
	switch {
	case strings.HasPrefix(input, api.PlaceholderWorkdir):
		prefix = os.Getenv(api.WorkdirEnv)
		suffix = strings.TrimPrefix(input, api.PlaceholderWorkdir)
		// we know that every process chdir's into
		// the workdir early, so we can omit the prefix safely.
		canShortPath = true
	case strings.HasPrefix(input, api.PlaceholderWorkspaceDir):
		prefix = os.Getenv(api.WorkspaceEnv)
		suffix = strings.TrimPrefix(input, api.PlaceholderWorkspaceDir)
	case strings.HasPrefix(input, api.PlaceholderTmpdir):
		prefix = tmpDir()
		suffix = strings.TrimPrefix(input, api.PlaceholderTmpdir)
	case strings.HasPrefix(input, api.PlaceholderCachedir):
		prefix = cacheDir()
		suffix = strings.TrimPrefix(input, api.PlaceholderCachedir)
	case strings.HasPrefix(input, api.PlaceholderHomedir):
		prefix = homeDir()
		suffix = strings.TrimPrefix(input, api.PlaceholderHomedir)
	default:
		return input
	}
	if shortPath && canShortPath {
		return filepath.Join(".", suffix)
	}
	return filepath.Join(prefix, suffix)
}
