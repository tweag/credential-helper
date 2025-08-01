package main

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/bazelbuild/rules_go/go/runfiles"
)

type commandLine struct {
	name string
	args []string
}

func prepareWorkspace(workspaceDir, sourceDir string) error {
	localBCR, err := runfiles.Rlocation("_main/bzl/private/release/bcr.local")
	if err != nil {
		return fmt.Errorf("failed to find local bcr: %v", err)
	}
	distdir, err := runfiles.Rlocation("_main/bzl/private/release/airgapped.distdir")
	if err != nil {
		return fmt.Errorf("failed to find distdir: %v", err)
	}
	bazelDepOverride, err := runfiles.Rlocation("_main/bzl/private/release/bcr_local_module_tweag-credential-helper.bazel_dep")
	if err != nil {
		return fmt.Errorf("failed to find bazel dep override: %v", err)
	}
	if err := copyFSWithSymlinks(workspaceDir, sourceDir); err != nil {
		return fmt.Errorf("failed to copy source dir: %v", err)
	}

	// replace parts of MODULE.bazel with dep override:
	// anything between the markers is replaced
	// with the contents of the dep override file
	moduleFile := filepath.Join(workspaceDir, "MODULE.bazel")
	moduleData, err := os.ReadFile(moduleFile)
	if err != nil {
		return fmt.Errorf("failed to read module file: %v", err)
	}
	depData, err := os.ReadFile(bazelDepOverride)
	if err != nil {
		return fmt.Errorf("failed to read dep override file: %v", err)
	}
	startMarker := "# BEGIN BAZEL_DEP"
	endMarker := "# END BAZEL_DEP"
	startIndex := strings.Index(string(moduleData), startMarker)
	endIndex := strings.Index(string(moduleData), endMarker)
	if startIndex == -1 || endIndex == -1 {
		return fmt.Errorf("failed to find markers in module file")
	}

	patchedModuleData := bytes.NewBuffer(nil)
	patchedModuleData.Write(moduleData[:startIndex])
	patchedModuleData.Write(depData)
	patchedModuleData.Write(moduleData[endIndex+len(endMarker):])
	os.Remove(moduleFile)
	if err := os.WriteFile(moduleFile, patchedModuleData.Bytes(), 0o644); err != nil {
		return fmt.Errorf("failed to write patched module file: %v", err)
	}
	localBCRUrlPath := filepath.ToSlash(localBCR)
	if runtime.GOOS == "windows" {
		localBCRUrlPath = "file:///" + localBCRUrlPath
	} else {
		localBCRUrlPath = "file://" + localBCRUrlPath
	}

	bazelrc := fmt.Sprintf(`common --registry=%s --registry=https://bcr.bazel.build/
common --distdir=%s
`, localBCRUrlPath, filepath.ToSlash(distdir))
	return os.WriteFile(filepath.Join(workspaceDir, ".bazelrc.generated"), []byte(bazelrc), 0o644)
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
	// Test that the prebuilt helper are correctly installed and available for use
	setupCommands = append(setupCommands, bazelCommand(bazel, []string{"cquery", "--@tweag-credential-helper//bzl/config:helper_build_mode=auto", `somepath(@tweag-credential-helper//installer, @tweag-credential-helper)`}, startupFlags))
	setupCommands = append(setupCommands, bazelCommand(bazel, []string{"cquery", "--@tweag-credential-helper//bzl/config:helper_build_mode=from_source", `somepath(@tweag-credential-helper//installer, @tweag-credential-helper)`}, startupFlags))
	setupCommands = append(setupCommands, bazelCommand(bazel, []string{"cquery", "--@tweag-credential-helper//bzl/config:helper_build_mode=prebuilt", `somepath(@tweag-credential-helper//installer, @tweag-credential-helper)`}, startupFlags))
	// shutdown Bazel after install to ensure
	// any failed fetches are retried with a helper in place
	setupCommands = append(setupCommands, bazelCommand(bazel, []string{"shutdown"}, startupFlags))

	return setupCommands, []commandLine{bazelCommand(bazel, []string{"test", "--cache_test_results=no", "//..."}, startupFlags)}, []commandLine{bazelCommand(bazel, []string{"shutdown"}, startupFlags)}
}

func runBazelCommands(bazel, helper, workspaceDir string) error {
	var startupFlags []string

	root, cleanupRoot := outputUserRoot()
	defer cleanupRoot()
	if len(root) > 0 {
		startupFlags = append(startupFlags, "--output_user_root="+root)
	}
	startupFlags = append(startupFlags, "--bazelrc="+filepath.Join(".bazelrc"))
	startupFlags = append(startupFlags, "--bazelrc="+filepath.Join(".bazelrc.generated"))

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
		fmt.Printf("\nrunning setup command $ bazel %s\n", strings.Join(command.args, " "))
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

	fmt.Println("\nrunning tests")
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

func copyFSWithSymlinks(destination, source string) error {
	canonicalBase := filepath.Clean(source)
	return filepath.Walk(source, func(path string, currentInfo os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		canoncialPath := filepath.Clean(path)
		relativePath, err := filepath.Rel(canonicalBase, canoncialPath)
		if err != nil {
			return err
		}

		newPath := filepath.Join(destination, relativePath)
		if currentInfo.IsDir() {
			return os.MkdirAll(newPath, 0o777)
		}

		if currentInfo.Mode()&fs.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(target, newPath)
		}

		if !currentInfo.Mode().IsRegular() {
			return &os.PathError{Op: "CopyFS", Path: path, Err: os.ErrInvalid}
		}

		r, err := os.Open(path)
		if err != nil {
			return err
		}
		defer r.Close()
		info, err := r.Stat()
		if err != nil {
			return err
		}
		w, err := os.OpenFile(newPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o666|info.Mode()&0o777)
		if err != nil {
			return err
		}

		if _, err := io.Copy(w, r); err != nil {
			w.Close()
			return &os.PathError{Op: "Copy", Path: newPath, Err: err}
		}
		return w.Close()
	})
}

func runHttpbinServer() error {
	binaryPath, err := runfiles.Rlocation("+_repo_rules+go_httpbin/cmd/go-httpbin/go-httpbin_/go-httpbin")
	if err != nil {
		return fmt.Errorf("failed to find go-httpbin binary: %v\n", err)
	}

	cmd := exec.Command(binaryPath, "-port", "9494")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err = cmd.Start()
	if err != nil {
		return fmt.Errorf("failed to start go-httpbin: %w\n", err)
	}

	fmt.Printf("go-httpbin started with PID: %d\n", cmd.Process.Pid)

	go func() {
		err := cmd.Wait()
		if err != nil {
			log.Printf("go-httpbin process finished with error: %v\n", err)
		} else {
			log.Printf("go-httpbin process finished successfully\n")
		}
	}()

	return nil
}

func main() {
	bazel := os.Getenv("BIT_BAZEL_BINARY")
	workspaceDir := os.Getenv("BIT_WORKSPACE_DIR") + ".scratch"
	defer os.RemoveAll(workspaceDir)
	helper := filepath.Join(workspaceDir, "tools", "credential-helper")
	if runtime.GOOS == "windows" {
		helper += ".exe"
	}

	if err := absolutifyEnvVars(); err != nil {
		panic(err)
	}

	err := runHttpbinServer()
	if err != nil {
		fmt.Println("failed to run go-httpbin")
		os.Exit(1)
	}

	var failed bool

	if err := prepareWorkspace(workspaceDir, os.Getenv("BIT_WORKSPACE_DIR")); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}

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
