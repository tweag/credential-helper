package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func outputUserRoot() (string, func() error) {
	if runtime.GOOS != "windows" {
		return "", func() error { return nil }
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		cache = os.TempDir()
	}
	tmpDir, err := os.MkdirTemp(cache, "bit-")
	if err != nil {
		panic(err)
	}
	return tmpDir, func() error {
		return os.RemoveAll(tmpDir)
	}
}

func installerTarget() string {
	if installerTarget, ok := os.LookupEnv("BAZEL_INTEGRATION_TEST_INSTALL_TARGET"); ok {
		return installerTarget
	}
	return "@tweag-credential-helper//installer"
}

func bazelCommand(command []string, startupFlags []string) []string {
	var out []string
	out = append(out, startupFlags...)
	out = append(out, command...)
	return out
}

func bazelCommands(startupFlags []string) [][]string {
	var commands [][]string

	commands = append(commands, bazelCommand([]string{"info"}, startupFlags))
	commands = append(commands, bazelCommand([]string{"run", installerTarget()}, startupFlags))
	commands = append(commands, bazelCommand([]string{"shutdown"}, startupFlags))
	commands = append(commands, bazelCommand([]string{"test", "//..."}, startupFlags))

	return commands
}

func runBazelCommands(bazel, workspaceDir string) error {
	var startupFlags []string

	root, cleanupRoot := outputUserRoot()
	defer cleanupRoot()
	if len(root) > 0 {
		startupFlags = append(startupFlags, "--output_user_root="+root)
	}

	commands := bazelCommands(startupFlags)
	for _, command := range commands {
		cmd := exec.Command(bazel, command...)
		cmd.Dir = workspaceDir
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err := cmd.Run()
		if err != nil {
			return fmt.Errorf("bazel integration test failed for command %v: %v", command, err)
		}
	}
	return nil
}

func absolutifyEnvVars() error {
	keys := strings.Fields(os.Getenv("ENV_VARS_TO_ABSOLUTIFY"))
	for _, key := range keys {
		if value, ok := os.LookupEnv(key); ok {
			absPath, err := filepath.Abs(value)
			if err != nil {
				return err
			}
			if err := os.Setenv(key, absPath); err != nil {
				return err
			}
		}
	}
	return nil
}

func main() {
	bazel := os.Getenv("BIT_BAZEL_BINARY")
	workspaceDir := os.Getenv("BIT_WORKSPACE_DIR")

	if err := absolutifyEnvVars(); err != nil {
		panic(err)
	}

	var failed bool

	integrationTestErr := runBazelCommands(bazel, workspaceDir)
	if integrationTestErr != nil {
		fmt.Fprintln(os.Stderr, integrationTestErr.Error())
		failed = true
	}

	// try to collect the logs from the agent
	helper := filepath.Join(workspaceDir, "tools", "credential-helper")
	if runtime.GOOS == "windows" {
		helper += ".exe"
	}
	fmt.Fprintln(os.Stderr, "Collecting agent logs...")
	cmd := exec.Command(helper, "agent-logs")
	cmd.Dir = workspaceDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to collect agent logs: %v\n", err)
	}

	if failed {
		os.Exit(1)
	}
}
