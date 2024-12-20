package root

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/tweag/credential-helper/agent"
	"github.com/tweag/credential-helper/agent/locate"
	"github.com/tweag/credential-helper/api"
	"github.com/tweag/credential-helper/cache"
	"github.com/tweag/credential-helper/logging"
)

const usage = `Usage:
  credential-helper get`

func Run(ctx context.Context, helperFactory api.HelperFactory, newCache api.NewCache, args []string) {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(1)
	}
	setLogLevel()
	if err := locate.SetupEnvironment(); err != nil {
		logging.Fatalf("setting up process environment: %v", err)
	}
	command := os.Args[1]
	switch command {
	case "get":
		clientProcess(ctx, helperFactory)
	case "agent-launch":
		agentProcess(ctx, newCache)
	case "agent-shutdown":
		clientCommandProcess(api.AgentRequestShutdown, nil)
	case "agent-prune":
		clientCommandProcess(api.AgentRequestPrune, nil)
	case "agent-raw":
		if len(os.Args) < 3 {
			logging.Fatalf("missing command argument")
		}
		clientCommandProcess(os.Args[2], os.Stdin)
	default:
		logging.Fatalf("unknown command")
	}
}

// foreground immediately responds to the get command and exits.
// If possible, it sends the response to the agent for caching.
func foreground(ctx context.Context, helperFactory api.HelperFactory, cache api.Cache) {
	req := api.GetCredentialsRequest{}

	err := json.NewDecoder(os.Stdin).Decode(&req)
	if err != nil {
		logging.Fatalf("%v", err)
	}

	authenticator, err := helperFactory(req.URI)
	if err != nil {
		logging.Fatalf("%v", err)
	}

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
			logging.Fatalf("%s", err)
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
		logging.Fatalf("%s", err)
	}

	err = json.NewEncoder(os.Stdout).Encode(resp)
	if err != nil {
		logging.Fatalf("%s", err)
	}

	cacheValue := api.CachableGetCredentialsResponse{
		CacheKey: cacheKey,
		Response: resp,
	}
	if err := cache.Store(ctx, cacheValue); err != nil {
		logging.Fatalf("%s", err)
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
	logging.Debugf("connecting to agent at %s", sockPath)
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

	foreground(ctx, helperFactory, cache)
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
		logging.Fatalf("%v", err)
	}
	defer cleanup()
	if err := service.Serve(ctx); err != nil {
		logging.Fatalf("%v", err)
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
