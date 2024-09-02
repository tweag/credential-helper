package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/tweag/credential-helper/api"
)

type CachingAgent struct {
	cache           api.Cache
	lis             net.Listener
	lockFile        *os.File
	shutdownChan    chan struct{}
	shutdownStarted atomic.Bool
	wg              sync.WaitGroup
}

func NewCachingAgent(socketPath string, agentLockPath string, cache api.Cache) (*CachingAgent, func() error, error) {
	_ = os.MkdirAll(filepath.Dir(agentLockPath), 0o755)
	_ = os.MkdirAll(filepath.Dir(socketPath), 0o755)

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
	_ = os.Remove(socketPath)

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, func() error { return nil }, err
	}
	agent := &CachingAgent{
		cache:        cache,
		lis:          listener,
		lockFile:     agentLock,
		shutdownChan: make(chan struct{}),
	}
	return agent, agent.cleanup, nil
}

func (a *CachingAgent) Serve(ctx context.Context) error {
	a.wg.Add(1)
	var acceptErr error

	defer func() {
		// TODO: perform error handling including acceptErr and anything that happens in handleConn
		if acceptErr != nil {
			fmt.Println(acceptErr)
		}
	}()
	defer a.wg.Wait()
	defer a.lis.Close()
	defer a.wg.Done()

	acceptChan := make(chan net.Conn)
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		// TODO: error handling
		acceptErr = acceptLoop(a.lis, acceptChan)
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
	fmt.Println("handling connection")
	defer fmt.Println("closing connection")
	defer conn.Close()
	req := api.AgentRequest{}

	reader := json.NewDecoder(conn)

	for {
		err := reader.Decode(&req)
		if err != nil {
			// handle eof
			if errors.Is(err, io.EOF) {
				fmt.Println("connection closed")
			} else {
				fmt.Printf("failed to decode request: %v\n", err)
			}
			return
		}
		fmt.Println("received request:", req)

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
			fmt.Printf("unknown method: %s\n", req.Method)
			resp = api.AgentResponse{Status: api.AgentResponseError, Payload: "unknown method"}
		}

		if respErr != nil {
			log.Printf("failed to handle request: %v\n", respErr)
			resp = api.AgentResponse{Status: api.AgentResponseError, Payload: respErr.Error()}
		}

		if err := json.NewEncoder(conn).Encode(resp); err != nil {
			log.Printf("failed to encode response: %v\n", err)
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

	return api.AgentResponse{Status: api.AgentResponseOK, Payload: resp}, nil
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
	fmt.Println("shutdown requested")
	if !a.shutdownStarted.CompareAndSwap(false, true) {
		fmt.Println("shutdown already started")
		return api.AgentResponse{Status: api.AgentResponseOK}, nil
	}
	fmt.Println("shutting down")
	close(a.shutdownChan)
	return api.AgentResponse{Status: api.AgentResponseOK}, nil
}

func (a *CachingAgent) cleanup() error {
	// This function must be called after Serve returns
	// to ensure that all resources are cleaned up.
	a.wg.Wait()
	fmt.Println("cleanup")
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
