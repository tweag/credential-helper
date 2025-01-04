package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type commandLine struct {
	name string
	args []string
}

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

func bazelCommand(name string, command []string, startupFlags []string) commandLine {
	var args []string
	args = append(args, startupFlags...)
	args = append(args, command...)
	return commandLine{name: name, args: args}
}

func bazelCommands(bazel string, startupFlags []string) (setup []commandLine, tests []commandLine, shutdown []commandLine) {
	var setupCommands []commandLine

	setupCommands = append(setupCommands, bazelCommand(bazel, []string{"info"}, startupFlags))
	setupCommands = append(setupCommands, bazelCommand(bazel, []string{"run", installerTarget()}, startupFlags))
	// shutdown Bazel after install to ensure
	// any failed fetches are retried with a helper in place
	setupCommands = append(setupCommands, bazelCommand(bazel, []string{"shutdown"}, startupFlags))

	return setupCommands, []commandLine{bazelCommand(bazel, []string{"test", "//..."}, startupFlags)}, []commandLine{bazelCommand(bazel, []string{"shutdown"}, startupFlags)}
}

func runBazelCommands(bazel, helper, workspaceDir string) error {
	var startupFlags []string

	root, cleanupRoot := outputUserRoot()
	defer cleanupRoot()
	if len(root) > 0 {
		startupFlags = append(startupFlags, "--output_user_root="+root)
	}

	setupCommands, testCommands, shutdownCommands := bazelCommands(bazel, startupFlags)

	defer func() {
		// shut down Bazel after all tests to conserve memory
		for _, shutdownCmd := range shutdownCommands {
			cmd := exec.Command(shutdownCmd.name, shutdownCmd.args...)
			cmd.Dir = workspaceDir
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			_ = cmd.Run()
		}
	}()

	for _, command := range setupCommands {
		cmd := exec.Command(command.name, command.args...)
		cmd.Dir = workspaceDir
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err := cmd.Run()
		if err != nil {
			return fmt.Errorf("bazel integration test setup step failed for command %v: %v", command, err)
		}
	}

	agentCmd := exec.Command(helper, "agent-launch")
	agentCmd.Dir = workspaceDir
	agentCmd.Stdout = os.Stdout
	agentCmd.Stderr = os.Stderr
	agentStartErr := agentCmd.Start()
	if agentStartErr != nil {
		return fmt.Errorf("failed to start agent: %v", agentStartErr)
	}
	// TODO: handle shutdown of agent more gracefully
	defer agentCmd.Wait()
	defer agentCmd.Process.Kill()

	for _, command := range testCommands {
		cmd := exec.Command(command.name, command.args...)
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
	helper := filepath.Join(workspaceDir, "tools", "credential-helper")
	if runtime.GOOS == "windows" {
		helper += ".exe"
	}

	if err := absolutifyEnvVars(); err != nil {
		panic(err)
	}

	var failed bool

	integrationTestErr := runBazelCommands(bazel, helper, workspaceDir)
	if integrationTestErr != nil {
		fmt.Fprintln(os.Stderr, integrationTestErr.Error())
		failed = true
	}

	// try to collect the logs from the agent
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
