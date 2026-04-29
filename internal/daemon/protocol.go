package daemon

import (
	"encoding/json"
	"errors"
	"net"
)

type Request struct {
	Command    string `json:"command"`
	Host       string `json:"host,omitempty"`
	Machine    string `json:"machine,omitempty"`
	SessionKey string `json:"session_key,omitempty"`
	SSHAlias   string `json:"ssh_alias,omitempty"`
	SSHHost    string `json:"ssh_host,omitempty"`
	SSHPort    string `json:"ssh_port,omitempty"`
	SSHUser    string `json:"ssh_user,omitempty"`
}

type Response struct {
	Path   string `json:"path,omitempty"`
	Text   string `json:"text,omitempty"`
	Status Status `json:"status,omitempty"`
	Error  string `json:"error,omitempty"`
	Debug  string `json:"debug,omitempty"`
}

type Status struct {
	ConfigHosts    []string         `json:"config_hosts,omitempty"`
	Recent         []Transfer       `json:"recent,omitempty"`
	Registered     int              `json:"registered,omitempty"`
	RecentBindings []SessionBinding `json:"recent_bindings,omitempty"`
}

type Transfer struct {
	Host      string `json:"host"`
	Route     string `json:"route"`
	Target    string `json:"target"`
	Path      string `json:"path"`
	Bytes     int    `json:"bytes"`
	CreatedAt string `json:"created_at"`
}

type SessionBinding struct {
	SessionKey string `json:"session_key"`
	Machine    string `json:"machine"`
	SSHAlias   string `json:"ssh_alias,omitempty"`
	SSHHost    string `json:"ssh_host,omitempty"`
	SSHPort    string `json:"ssh_port,omitempty"`
	SSHUser    string `json:"ssh_user,omitempty"`
	CreatedAt  string `json:"created_at"`
}

func Send(socketPath string, req Request) (Response, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return Response{}, err
	}
	defer conn.Close()
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return Response{}, err
	}
	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return Response{}, err
	}
	if resp.Error != "" {
		return resp, errors.New(resp.Error)
	}
	return resp, nil
}
