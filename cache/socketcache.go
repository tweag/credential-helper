package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/tweag/credential-helper/api"
)

// SocketCache retrieves and stores responses from a socket.
type SocketCache struct {
	conn net.Conn
}

const wait = time.Millisecond

// NewSocketCache creates a new SocketCache.
func NewSocketCache(socketPath string, timeout time.Duration) (*SocketCache, error) {
	var conn net.Conn
	// TODO: think about using fsnotify to wait for the socket
	for waited := time.Duration(0); waited < timeout; waited += wait {
		var err error
		conn, err = net.Dial("unix", socketPath)
		if err == nil {
			break
		}
		time.Sleep(wait)
	}

	if conn == nil {
		return nil, fmt.Errorf("dialing socket: %w", os.ErrDeadlineExceeded)
	}

	return &SocketCache{conn: conn}, nil
}

// Retrieve retrieves a response from the socket.
func (c *SocketCache) Retrieve(ctx context.Context, cacheKey string) (api.GetCredentialsResponse, error) {
	if len(cacheKey) == 0 {
		return api.GetCredentialsResponse{}, api.CacheMiss
	}
	payload, err := json.Marshal(cacheKey)
	if err != nil {
		return api.GetCredentialsResponse{}, err
	}
	req := api.AgentRequest{
		Method:  api.AgentRequestRetrieve,
		Payload: payload,
	}
	if err := json.NewEncoder(c.conn).Encode(req); err != nil {
		return api.GetCredentialsResponse{}, err
	}

	var resp api.AgentResponse
	if err := json.NewDecoder(c.conn).Decode(&resp); err != nil {
		return api.GetCredentialsResponse{}, err
	}

	if resp.Status == api.AgentResponseCacheMiss {
		return api.GetCredentialsResponse{}, api.CacheMiss
	}

	if resp.Status != api.AgentResponseOK {
		return api.GetCredentialsResponse{}, fmt.Errorf("retrieving cacheKey from agent: %s %v", resp.Status, resp.Payload)
	}

	var respPayload api.GetCredentialsResponse
	if err := json.Unmarshal(resp.Payload, &respPayload); err != nil {
		return api.GetCredentialsResponse{}, fmt.Errorf("retrieving cacheKey from agent: umarshaling response: %w", err)
	}

	return respPayload, nil
}

// Store stores a response in the socket.
func (c *SocketCache) Store(ctx context.Context, cacheValue api.CachableGetCredentialsResponse) error {
	if len(cacheValue.CacheKey) == 0 || len(cacheValue.Response.Expires) == 0 {
		return nil
	}
	payload, err := json.Marshal(cacheValue)
	if err != nil {
		return err
	}
	req := api.AgentRequest{
		Method:  api.AgentRequestStore,
		Payload: payload,
	}
	if err := json.NewEncoder(c.conn).Encode(req); err != nil {
		return err
	}

	var agentResponse api.AgentResponse
	if err := json.NewDecoder(c.conn).Decode(&agentResponse); err != nil {
		return err
	}

	if agentResponse.Status != api.AgentResponseOK {
		return fmt.Errorf("storing response in agent: %s %v", agentResponse.Status, agentResponse.Payload)
	}

	return nil
}

// Prune prunes the cache in the socket.
func (c *SocketCache) Prune(ctx context.Context) error {
	req := api.AgentRequest{
		Method: api.AgentRequestPrune,
	}
	if err := json.NewEncoder(os.Stdout).Encode(req); err != nil {
		return err
	}

	var agentResponse api.AgentResponse
	if err := json.NewDecoder(os.Stdin).Decode(&agentResponse); err != nil {
		return err
	}

	if agentResponse.Status != api.AgentResponseOK {
		return fmt.Errorf("pruning cache in agent: %s %v", agentResponse.Status, agentResponse.Payload)
	}

	return nil
}

// Close closes the connection to the socket.
func (c *SocketCache) Close() error {
	return c.conn.Close()
}
