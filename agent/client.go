package agent

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/tweag/credential-helper/agent/locate"
	"github.com/tweag/credential-helper/api"
	"github.com/tweag/credential-helper/logging"
)

func LaunchAgentProcess() error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding path to own executable: %w", err)
	}
	var stdout, stderr *os.File
	if logging.GetLevel() >= logging.LogLevelDebug {
		// In debug mode, we want to see the agent's logs.
		_ = os.MkdirAll(locate.Run(), 0700)
		agentStdout, err := os.OpenFile(filepath.Join(locate.Run(), "agent.stdout"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			return fmt.Errorf("opening agent stdout file for logging: %w", err)
		}
		agentStderr, err := os.OpenFile(filepath.Join(locate.Run(), "agent.stderr"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			return fmt.Errorf("opening agent stderr file for logging: %w", err)
		}
		stdout, stderr = agentStdout, agentStderr
		defer stdout.Close()
		defer stderr.Close()
	}
	proc, err := os.StartProcess(self, []string{self, "agent-launch"}, procAttrForAgentProcess(stdout, stderr))
	if err != nil {
		return fmt.Errorf("starting agent process: %w", err)
	}
	return proc.Release()
}

type AgentCommandClient struct {
	conn net.Conn
}

func NewAgentCommandClient(socketPath string) (*AgentCommandClient, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, err
	}
	return &AgentCommandClient{conn: conn}, nil
}

func (c *AgentCommandClient) Close() error {
	return c.conn.Close()
}

func (c *AgentCommandClient) Command(req api.AgentRequest) (api.AgentResponse, error) {
	if err := json.NewEncoder(c.conn).Encode(req); err != nil {
		return api.AgentResponse{}, err
	}

	var resp api.AgentResponse
	if err := json.NewDecoder(c.conn).Decode(&resp); err != nil {
		return api.AgentResponse{}, err
	}
	return resp, nil
}
