package agent

import (
	"encoding/json"
	"net"
	"os"
	"syscall"

	"github.com/tweag/credential-helper/api"
)

func LaunchAgentProcess() error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	sys := syscall.SysProcAttr{
		Setpgid: true,
	}
	procAttr := &os.ProcAttr{
		Sys: &sys,
	}
	proc, err := os.StartProcess(self, []string{self, "agent-launch"}, procAttr)
	if err != nil {
		return err
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
