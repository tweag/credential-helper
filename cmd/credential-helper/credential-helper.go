package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/tweag/credential-helper/agent"
	"github.com/tweag/credential-helper/api"
	authenticateGCS "github.com/tweag/credential-helper/authenticate/gcs"
	authenticateGitHub "github.com/tweag/credential-helper/authenticate/github"
	authenticateNull "github.com/tweag/credential-helper/authenticate/null"
	authenticateS3 "github.com/tweag/credential-helper/authenticate/s3"
	"github.com/tweag/credential-helper/cache"
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
	case strings.EqualFold(u.Host, "objects.githubusercontent.com"):
		return authenticateGitHub.New(ctx)
	default:
		fmt.Fprintln(os.Stderr, "no matching credential helper found - returning empty response")
		return authenticateNull.Null{}, nil
	}
}

// foreground immediately responds to the get command and exits.
// If possible, it sends the response to the agent for caching.
func foreground(ctx context.Context, cache api.Cache) {
	req := api.GetCredentialsRequest{}

	err := json.NewDecoder(os.Stdin).Decode(&req)
	if err != nil {
		log.Fatal(err)
	}

	authenticator, err := chooseHelper(ctx, req.URI)
	if err != nil {
		log.Fatal(err)
	}

	cacheKey := authenticator.CacheKey(req)
	if len(cacheKey) == 0 {
		fmt.Fprintln(os.Stderr, "no cache key returned - not caching")
	} else {
		fmt.Fprintln(os.Stderr, "cache key:", cacheKey)
	}
	resp, err := cache.Retrieve(ctx, cacheKey)
	if err == nil {
		err := json.NewEncoder(os.Stdout).Encode(resp)
		if err != nil {
			log.Fatal(err)
		}
		return
	}

	resp, err = authenticator.Get(ctx, req)
	if err != nil {
		log.Fatal(err)
	}

	err = json.NewEncoder(os.Stdout).Encode(resp)
	if err != nil {
		log.Fatal(err)
	}

	cacheValue := api.CachableGetCredentialsResponse{
		CacheKey: cacheKey,
		Response: resp,
	}
	if err := cache.Store(ctx, cacheValue); err != nil {
		log.Fatal(err)
	}
}

func launchOrConnectAgent() (api.Cache, func() error, error) {
	if shouldRunStandalone() {
		fmt.Fprintln(os.Stderr, "running in standalone mode")
		return &cache.NoCache{}, func() error { return nil }, nil
	}

	fmt.Fprintln(os.Stderr, "running in agent mode")

	// try to launch the agent process
	// this will fail if the agent is already running, which is fine
	if err := agent.LaunchAgentProcess(); err != nil {
		return nil, func() error { return nil }, err
	}

	fmt.Fprintln(os.Stderr, "launched agent")

	// TODO: make socket path configurable
	socketCache, err := cache.NewSocketCache("agent.sock", time.Second)
	if err != nil {
		return nil, func() error { return nil }, err
	}

	fmt.Fprintln(os.Stderr, "connected to agent")

	return socketCache, func() error { return socketCache.Close() }, nil
}

func clientProcess(ctx context.Context) {
	cache, cleanup, err := launchOrConnectAgent()
	if err != nil {
		log.Fatal(err)
	}
	defer cleanup()

	foreground(ctx, cache)
}

// agent process runs in the background and caches responses.
func agentProcess(ctx context.Context) {
	if shouldRunStandalone() {
		log.Fatal("running as agent is not supported in standalone mode")
	}

	// TODO: make socket path configurable
	service, cleanup, err := agent.NewCachingAgent("agent.sock", "agent.pid", cache.NewMemCache())
	if err != nil {
		log.Fatal(err)
	}
	defer cleanup()
	if err := service.Serve(ctx); err != nil {
		log.Fatal(err)
	}
}

func shouldRunStandalone() bool {
	standalone := strings.ToLower(os.Getenv(api.Standalone))
	if standalone == "true" || standalone == "1" {
		return true
	}
	return false
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, usage)
		os.Exit(1)
	}
	ctx := context.Background()
	command := os.Args[1]
	switch command {
	case "get":
		clientProcess(ctx)
	case "agent":
		agentProcess(ctx)
	default:
		log.Fatal("unknown command")
	}
}
