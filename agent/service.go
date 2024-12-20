package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/tweag/credential-helper/api"
	"github.com/tweag/credential-helper/logging"
)

type CachingAgent struct {
	cache           api.Cache
	lis             net.Listener
	lockFile        *os.File
	shutdownChan    chan struct{}
	shutdownStarted atomic.Bool
	idleTimeout     time.Duration
	idleTimer       *time.Timer
	pruneInterval   time.Duration
	pruneTimer      *time.Timer
	wg              sync.WaitGroup
}

func NewCachingAgent(socketPath string, agentLockPath string, cache api.Cache, idleTimeout time.Duration, pruneInterval time.Duration) (*CachingAgent, func() error, error) {
	hardenAgentProcess()

	_ = os.MkdirAll(filepath.Dir(agentLockPath), 0o755)
	if !strings.HasPrefix(socketPath, "@") {
		_ = os.MkdirAll(filepath.Dir(socketPath), 0o755)
	}

	agentLock, err := os.OpenFile(agentLockPath, os.O_RDWR|os.O_CREATE, 0o666)
	if err != nil {
		return nil, func() error { return nil }, err
	}

	if err := syscall.Flock(int(agentLock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		agentLock.Close()
		return nil, func() error { return nil }, fmt.Errorf("failed to lock agent lock file (agent already running?): %w", err)
	}
	if _, err := agentLock.WriteString(fmt.Sprintf("%d", os.Getpid())); err != nil {
		agentLock.Close()
		return nil, func() error { return nil }, fmt.Errorf("failed to write pid to agent lock file: %w", err)
	}

	// delete the socket file if it already exists from a previous, dead agent
	if !strings.HasPrefix(socketPath, "@") {
		_ = os.Remove(socketPath)
	}

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, func() error { return nil }, err
	}
	agent := &CachingAgent{
		cache:         cache,
		lis:           listener,
		lockFile:      agentLock,
		shutdownChan:  make(chan struct{}),
		idleTimeout:   idleTimeout,
		pruneInterval: pruneInterval,
	}
	return agent, agent.cleanup, nil
}

func (a *CachingAgent) Serve(ctx context.Context) error {
	var acceptErr error

	defer func() {
		// TODO: perform error handling including acceptErr and anything that happens in handleConn
		if acceptErr != nil && !errors.Is(acceptErr, net.ErrClosed) {
			logging.Errorf("failed to accept connection: %v\n", acceptErr)
		}
	}()
	defer a.wg.Wait()
	defer a.lis.Close()

	a.idleTimer = time.NewTimer(a.idleTimeout)
	a.pruneTimer = time.NewTimer(0)
	acceptChan := make(chan net.Conn)
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		// TODO: error handling
		acceptErr = acceptLoop(a.lis, acceptChan)
	}()

	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		<-a.idleTimer.C
		// use negative idleTimeout value to ignore the timeout signal
		if a.idleTimeout >= 0 && !a.shutdownStarted.Load() {
			logging.Debugf("agent shutting down after idle timeout of %v reached", a.idleTimeout)
			_, _ = a.handleShutdown()
		}
	}()

	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		for !a.shutdownStarted.Load() {
			<-a.pruneTimer.C
			// use negative pruneInterval value to ignore the signal
			if a.pruneInterval >= 0 && !a.shutdownStarted.Load() {
				logging.Debugf("pruning the cache")
				if err := a.cache.Prune(ctx); err != nil {
					logging.Errorf("scheduled cache prune: %v\n", err)
				}
			}
			a.pruneTimer.Reset(a.pruneInterval)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-a.shutdownChan:
			return nil
		case conn, ok := <-acceptChan:
			if !ok {
				return nil
			}
			a.wg.Add(1)
			go func() {
				defer a.wg.Done()
				a.handleConn(ctx, conn)
			}()
		}
	}
}

func (a *CachingAgent) handleConn(ctx context.Context, conn net.Conn) {
	logging.Debugf("handling connection")
	defer logging.Debugf("done handling connection")
	defer conn.Close()
	req := api.AgentRequest{}

	reader := json.NewDecoder(conn)

	for {
		err := reader.Decode(&req)
		if err != nil {
			if errors.Is(err, io.EOF) {
				logging.Errorf("connection is closed")
				return
			} else {
				logging.Errorf("failed to decode request: %v\n", err)
			}
			if err := json.NewEncoder(conn).Encode(api.AgentResponse{Status: api.AgentResponseError, Payload: []byte("\"invalid json in request\"")}); err != nil {
				logging.Errorf("failed to encode response: %v\n", err)
			}
			return
		}
		// reset the timeout for each request in a connection
		// allows for correct keepalive when using long-living connections
		a.idleTimer.Reset(a.idleTimeout)
		logging.Debugf("received request: { method: %q, payload: %s }\n", req.Method, string(req.Payload))

		var resp api.AgentResponse
		var respErr error
		switch req.Method {
		case api.AgentRequestRetrieve:
			resp, respErr = a.handleRetrieve(ctx, req)
		case api.AgentRequestStore:
			resp, respErr = a.handleStore(ctx, req)
		case api.AgentRequestPrune:
			resp, respErr = a.handlePrune(ctx)
		case api.AgentRequestShutdown:
			resp, respErr = a.handleShutdown()
		default:
			logging.Errorf("unknown method: %s\n", req.Method)
			resp = api.AgentResponse{Status: api.AgentResponseError, Payload: []byte("\"unknown method\"")}
		}

		if respErr != nil {
			logging.Errorf("failed to handle request: %v\n", respErr)
			rawError, err := json.Marshal(respErr.Error())
			if err != nil {
				rawError = []byte("\"unknown error\"")
			}
			resp = api.AgentResponse{Status: api.AgentResponseError, Payload: rawError}
		}

		logging.Debugf("sending response: { status: %q, payload: %s }\n", resp.Status, string(resp.Payload))
		if err := json.NewEncoder(conn).Encode(resp); err != nil {
			logging.Errorf("failed to encode response: %v\n", err)
		}
	}
}

func (a *CachingAgent) handleRetrieve(ctx context.Context, req api.AgentRequest) (api.AgentResponse, error) {
	var cacheKey string
	if err := json.Unmarshal(req.Payload, &cacheKey); err != nil {
		return api.AgentResponse{}, fmt.Errorf("retrieve: failed to unmarshal cache key from request: %w", err)
	}

	resp, err := a.cache.Retrieve(ctx, cacheKey)
	// check for api.CacheMiss and return status accordingly
	if err != nil {
		if errors.Is(err, api.CacheMiss) {
			return api.AgentResponse{Status: api.AgentResponseCacheMiss}, nil
		}
		return api.AgentResponse{}, err
	}

	rawPayload, err := json.Marshal(resp)
	if err != nil {
		return api.AgentResponse{}, fmt.Errorf("retrieve: failed to marshal cache value: %w", err)
	}

	return api.AgentResponse{Status: api.AgentResponseOK, Payload: rawPayload}, nil
}

func (a *CachingAgent) handleStore(ctx context.Context, req api.AgentRequest) (api.AgentResponse, error) {
	var cachableResp api.CachableGetCredentialsResponse
	if err := json.Unmarshal(req.Payload, &cachableResp); err != nil {
		return api.AgentResponse{}, fmt.Errorf("store: failed to unmarshal cache value from request: %w", err)
	}

	err := a.cache.Store(ctx, cachableResp)
	if err != nil {
		return api.AgentResponse{}, err
	}

	return api.AgentResponse{Status: api.AgentResponseOK}, nil
}

func (a *CachingAgent) handlePrune(ctx context.Context) (api.AgentResponse, error) {
	err := a.cache.Prune(ctx)
	if err != nil {
		return api.AgentResponse{}, err
	}
	return api.AgentResponse{Status: api.AgentResponseOK}, nil
}

func (a *CachingAgent) handleShutdown() (api.AgentResponse, error) {
	logging.Debugf("shutdown requested")
	if !a.shutdownStarted.CompareAndSwap(false, true) {
		logging.Debugf("shutdown already started")
		return api.AgentResponse{Status: api.AgentResponseOK}, nil
	}
	logging.Basicf("shutting down")

	// force timer goroutines to stop early
	a.idleTimeout = -time.Microsecond
	a.idleTimer.Reset(a.idleTimeout)
	a.pruneInterval = -time.Microsecond
	a.pruneTimer.Reset(a.pruneInterval)

	close(a.shutdownChan)
	return api.AgentResponse{Status: api.AgentResponseOK}, nil
}

func (a *CachingAgent) cleanup() error {
	// This function must be called after Serve returns
	// to ensure that all resources are cleaned up.
	a.wg.Wait()
	logging.Debugf("cleaning up")
	_ = syscall.Flock(int(a.lockFile.Fd()), syscall.LOCK_UN)
	return a.lockFile.Close()
}

func acceptLoop(lis net.Listener, out chan net.Conn) error {
	for {
		conn, err := lis.Accept()
		if err != nil {
			return err
		}
		out <- conn
	}
}
