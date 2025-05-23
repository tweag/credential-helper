package root

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tweag/credential-helper/agent"
	"github.com/tweag/credential-helper/agent/locate"
	"github.com/tweag/credential-helper/api"
	"github.com/tweag/credential-helper/cache"
	"github.com/tweag/credential-helper/cmd/installer"
	"github.com/tweag/credential-helper/cmd/internal/util"
	"github.com/tweag/credential-helper/cmd/setup"
	"github.com/tweag/credential-helper/config"
	"github.com/tweag/credential-helper/logging"
)

// the value of this variable is intended to be substituted with the actual tool
// version using the linker's stamping feature (i.e., using a `-X` argument)
var version = "0.0.0"

const usage = `Usage: credential-helper [COMMAND] [ARGS...]

Commands:
  get            get credentials in the form of http headers for the uri provided on stdin and print result to stdout (see https://github.com/EngFlow/credential-helper-spec for more information)
  setup-uri      prints setup instructions for a given uri
  setup-keyring  stores a secret in the system keyring
  version        displays the version of this tool`

func Run(ctx context.Context, helperFactory api.HelperFactory, newCache api.NewCache, args []string) {
	setLogLevel()
	if len(args) < 2 {
		if installer.WantInstallerRun() {
			installer.InstallerProcess()
			return
		}
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(1)
	}
	if err := locate.SetupEnvironment(); err != nil {
		logging.Fatalf("setting up process environment: %v", err)
	}
	command := args[1]
	switch command {
	case "get":
		clientProcess(ctx, helperFactory)
	case "setup-uri":
		setup.URIProcess(args[2:], helperFactory, config.OSReader{})
	case "setup-keyring":
		setup.KeyringProcess(args[2:])
	case "agent-launch":
		agentProcess(ctx, newCache)
	case "agent-shutdown":
		clientCommandProcess(api.AgentRequestShutdown, nil)
	case "agent-prune":
		clientCommandProcess(api.AgentRequestPrune, nil)
	case "agent-logs":
		agentLogsProcess()
	case "agent-raw":
		if len(args) < 3 {
			logging.Fatalf("missing command argument")
		}
		clientCommandProcess(args[2], os.Stdin)
	case "installer-install":
		installer.InstallerProcess()
	case "version":
		fmt.Println(version)
	default:
		logging.Fatalf("unknown command")
	}
}

// foreground immediately responds to the get command and exits.
// If possible, it sends the response to the agent for caching.
func foreground(ctx context.Context, cache api.Cache, helperFactory api.HelperFactory, configReader config.ConfigReader) {
	req := api.GetCredentialsRequest{}

	err := json.NewDecoder(os.Stdin).Decode(&req)
	if err != nil {
		logging.Fatalf("%v", err)
	}

	// The credential helper is often invoked by other tools,
	// so there is no reliable way to ensure that the stderr
	// of the credential helper is visible to the user.
	// Therefore, we log every request to syslog in debug mode.
	logging.SyslogDebugf(req.URI)

	ctx, authenticator := util.Configure(ctx, helperFactory, configReader, req.URI)

	cacheKey := authenticator.CacheKey(req)
	if len(cacheKey) == 0 {
		logging.Basicf("no cache key returned - not caching")
	} else {
		logging.Debugf("cache key: %s", cacheKey)
	}
	resp, err := cache.Retrieve(ctx, cacheKey)
	if err == nil {
		// early return on cache hit
		logging.Debugf("cache hit")
		err := json.NewEncoder(os.Stdout).Encode(resp)
		if err != nil {
			logging.Fatalf("printing cached response to stdout: %s", err)
		}
		return
	} else if !errors.Is(err, api.CacheMiss) {
		logging.Errorf("retrieving credentials from agent cache: %s", err)
	} else {
		logging.Debugf("cache miss")
	}

	resolver, err := authenticator.Resolver(ctx)
	if err != nil {
		logging.Fatalf("instantiating resolver: %s", err)
	}

	resp, err = resolver.Get(ctx, req)
	if err != nil {
		var extraMessage string
		_, canSetupViaAuthenticator := authenticator.(api.URISetupper)
		_, canSetupViaResolver := resolver.(api.URISetupper)
		if canSetupViaAuthenticator || canSetupViaResolver {
			extraMessage = fmt.Sprintf("\n\nTip: try running the following command for setup instructions:\n  $ %s setup-uri %s", os.Args[0], req.URI)
		}
		logging.Fatalf("%s%s", err, extraMessage)
	}

	err = json.NewEncoder(os.Stdout).Encode(resp)
	if err != nil {
		logging.Fatalf("printing response to stdout: %s", err)
	}

	cacheValue := api.CachableGetCredentialsResponse{
		CacheKey: cacheKey,
		Response: resp,
	}
	if err := cache.Store(ctx, cacheValue); err != nil {
		logging.Fatalf("storing response in cache: %s", err)
	}
}

func launchOrConnectAgent() (api.Cache, func() error, error) {
	if shouldRunStandalone() {
		logging.Debugf("running in standalone mode")
		return &cache.NoCache{}, func() error { return nil }, nil
	}
	logging.Debugf("running in agent mode")

	// try to launch the agent process
	// this will fail if the agent is already running, which is fine
	if err := agent.LaunchAgentProcess(); err != nil {
		return nil, func() error { return nil }, err
	}

	logging.Debugf("launched agent")

	sockPath, _ := locate.AgentPaths()
	logging.Debugf("connecting to agent on %s in %s", sockPath, locate.Workdir())
	socketCache, err := cache.NewSocketCache(sockPath, time.Second)
	if err != nil {
		return nil, func() error { return nil }, err
	}

	logging.Debugf("connected to agent")

	return socketCache, func() error { return socketCache.Close() }, nil
}

func clientProcess(ctx context.Context, helperFactory api.HelperFactory) {
	cache, cleanup, err := launchOrConnectAgent()
	if err != nil {
		logging.Errorf("failed to launch or connect to agent: %v", err)
		os.Exit(1)
	}
	defer cleanup()

	foreground(ctx, cache, helperFactory, config.OSReader{})
}

func clientCommandProcess(command string, r io.Reader) {
	socketPath, _ := locate.AgentPaths()
	conn, err := agent.NewAgentCommandClient(socketPath)
	if err != nil {
		if command == api.AgentRequestShutdown {
			return // ignore connection errors for shutdown. The agent may not be running.
		}
		logging.Fatalf("%v", err)
	}
	defer conn.Close()
	var payload []byte
	if r != nil {
		payload, err = io.ReadAll(r)
		if err != nil {
			logging.Fatalf("%v", err)
		}
	}
	resp, err := conn.Command(api.AgentRequest{
		Method:  command,
		Payload: payload,
	})
	if err != nil {
		logging.Fatalf("%v", err)
	}
	if resp.Status != api.AgentResponseOK {
		logging.Fatalf("agent response: %s %s", resp.Status, string(resp.Payload))
	}
	_, _ = os.Stdout.Write(resp.Payload)
}

// agent process runs in the background and caches responses.
func agentProcess(ctx context.Context, newCache api.NewCache) {
	logging.Debugf("starting agent %v", os.Getpid())
	defer logging.Debugf("agent %v shutting down", os.Getpid())
	if shouldRunStandalone() {
		logging.Fatalf("running as agent is not supported in standalone mode")
	}

	sockPath, pidPath := locate.AgentPaths()
	idleTimeout, err := getDurationFromEnvOrDefault(api.IdleTimeoutEnv, 3*time.Hour)
	if err != nil {
		logging.Fatalf("determining idle timeout from $%s: %v", api.IdleTimeoutEnv, err)
	}
	pruneInterval, err := getDurationFromEnvOrDefault(api.PruneIntervalEnv, time.Minute)
	if err != nil {
		logging.Fatalf("determining idle timeout from $%s: %v", api.PruneIntervalEnv, err)
	}
	service, cleanup, err := agent.NewCachingAgent(sockPath, pidPath, newCache(), idleTimeout, pruneInterval)
	if err != nil {
		logging.Errorf("%v", err)
		return
	}
	defer cleanup()
	if err := service.Serve(ctx); err != nil {
		logging.Errorf("%v", err)
	}
}

// agentLogsProcess prints the agent logs to stdout and stderr, then exits.
func agentLogsProcess() {
	stdoutPath := filepath.Join(locate.Run(), "agent.stdout")
	stderrPath := filepath.Join(locate.Run(), "agent.stderr")

	stdoutLog, err := os.Open(stdoutPath)
	if err != nil {
		logging.Fatalf("opening agent stdout log: %v", err)
	}
	defer stdoutLog.Close()
	stderrLog, err := os.Open(stderrPath)
	if err != nil {
		logging.Fatalf("opening agent stderr log: %v", err)
	}
	defer stderrLog.Close()

	_, err = io.Copy(os.Stdout, stdoutLog)
	if err != nil {
		logging.Fatalf("copying agent stdout log to stdout: %v", err)
	}
	_, err = io.Copy(os.Stderr, stderrLog)
	if err != nil {
		logging.Fatalf("copying agent stderr log to stderr: %v", err)
	}
}

func shouldRunStandalone() bool {
	standalone := strings.ToLower(os.Getenv(api.Standalone))
	if standalone == "true" || standalone == "1" {
		return true
	}
	return false
}

func setLogLevel() {
	level, ok := os.LookupEnv(api.LogLevelEnv)
	if !ok {
		return
	}
	logging.SetLevel(logging.FromString(level))
}

func getDurationFromEnvOrDefault(key string, fallback time.Duration) (time.Duration, error) {
	timeoutString, ok := os.LookupEnv(key)
	if !ok {
		return fallback, nil
	}
	return time.ParseDuration(timeoutString)
}
