package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/tweag/credential-helper/agent"
	"github.com/tweag/credential-helper/agent/locate"
	"github.com/tweag/credential-helper/api"
	authenticateGCS "github.com/tweag/credential-helper/authenticate/gcs"
	authenticateGitHub "github.com/tweag/credential-helper/authenticate/github"
	authenticateNull "github.com/tweag/credential-helper/authenticate/null"
	authenticateS3 "github.com/tweag/credential-helper/authenticate/s3"
	"github.com/tweag/credential-helper/cache"
	"github.com/tweag/credential-helper/logging"
)

const usage = `Usage:
  credential-helper get
`

func chooseHelper(ctx context.Context, rawURL string) (api.Getter, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	switch {
	case strings.HasSuffix(u.Host, ".amazonaws.com"):
		return authenticateS3.New(ctx)
	case strings.EqualFold(u.Host, "storage.googleapis.com"):
		return authenticateGCS.New(ctx)
	case strings.EqualFold(u.Host, "github.com"):
		fallthrough
	case strings.HasSuffix(strings.ToLower(u.Host), ".github.com"):
		return authenticateGitHub.New(ctx)
	default:
		logging.Basicf("no matching credential helper found for %s - returning empty response\n", rawURL)
		return authenticateNull.Null{}, nil
	}
}

// foreground immediately responds to the get command and exits.
// If possible, it sends the response to the agent for caching.
func foreground(ctx context.Context, cache api.Cache) {
	req := api.GetCredentialsRequest{}

	err := json.NewDecoder(os.Stdin).Decode(&req)
	if err != nil {
		logging.Fatalf("%v", err)
	}

	authenticator, err := chooseHelper(ctx, req.URI)
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

	resp, err = authenticator.Get(ctx, req)
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

	// TODO: make socket path configurable
	sockPath, _, err := locate.AgentPaths()
	if err != nil {
		return nil, func() error { return nil }, err
	}
	socketCache, err := cache.NewSocketCache(sockPath, time.Second)
	if err != nil {
		return nil, func() error { return nil }, err
	}

	logging.Debugf("connected to agent")

	return socketCache, func() error { return socketCache.Close() }, nil
}

func clientProcess(ctx context.Context) {
	cache, cleanup, err := launchOrConnectAgent()
	if err != nil {
		logging.Errorf("failed to launch or connect to agent: %v", err)
		os.Exit(1)
	}
	defer cleanup()

	foreground(ctx, cache)
}

func clientCommandProcess(command string, r io.Reader) {
	socketPath, _, err := locate.AgentPaths()
	if err != nil {
		logging.Fatalf("%v", err)
	}
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
func agentProcess(ctx context.Context) {
	if shouldRunStandalone() {
		logging.Fatalf("running as agent is not supported in standalone mode")
	}

	sockPath, pidPath, err := locate.AgentPaths()
	if err != nil {
		logging.Fatalf("%v", err)
	}
	service, cleanup, err := agent.NewCachingAgent(sockPath, pidPath, cache.NewMemCache())
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

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, usage)
		os.Exit(1)
	}
	setLogLevel()
	ctx := context.Background()
	command := os.Args[1]
	switch command {
	case "get":
		clientProcess(ctx)
	case "agent":
		agentProcess(ctx)
	case "shutdown":
		clientCommandProcess(api.AgentRequestShutdown, nil)
	case "prune":
		clientCommandProcess(api.AgentRequestPrune, nil)
	case "raw":
		if len(os.Args) < 3 {
			logging.Fatalf("missing command argument")
		}
		clientCommandProcess(os.Args[2], os.Stdin)
	default:
		logging.Fatalf("unknown command")
	}
}
