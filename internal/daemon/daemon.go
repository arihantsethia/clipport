package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/arihantsethia/clipport/internal/clipboard"
	"github.com/arihantsethia/clipport/internal/config"
	"github.com/arihantsethia/clipport/internal/registry"
	"github.com/arihantsethia/clipport/internal/remote"
	"github.com/arihantsethia/clipport/internal/terminal"
)

type ImageProvider interface {
	ReadPNG() ([]byte, error)
}

type Server struct {
	Config   *config.Config
	Sessions terminal.ActiveSessionProvider
	Images   ImageProvider
	Routes   *remote.Manager
	Uploader remote.Uploader

	mu          sync.RWMutex
	registered  map[string]sessionBinding
	recent      []Transfer
	recentBinds []SessionBinding
}

type sessionBinding struct {
	Machine   string
	SSHAlias  string
	SSHHost   string
	SSHPort   string
	SSHUser   string
	CreatedAt time.Time
}

func DefaultSocketPath() string {
	return filepath.Join(os.TempDir(), "clipport", fmt.Sprint(os.Getuid()), "clipportd.sock")
}

func NewServer(cfg *config.Config) *Server {
	s := &Server{
		Config:     cfg,
		Sessions:   terminal.ItermProvider{},
		Images:     clipboard.ImageProvider{},
		Routes:     remote.NewManager(nil),
		registered: map[string]sessionBinding{},
	}
	for _, h := range cfg.Hosts {
		s.Routes.WarmHost(h)
	}
	return s
}

func (s *Server) Listen(socketPath string) error {
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o700); err != nil {
		return err
	}
	_ = os.Remove(socketPath)
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return err
	}
	defer ln.Close()
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()
	var req Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		_ = json.NewEncoder(conn).Encode(Response{Error: err.Error()})
		return
	}
	resp := s.Handle(req)
	_ = json.NewEncoder(conn).Encode(resp)
}

func (s *Server) Handle(req Request) Response {
	switch req.Command {
	case "paste_image":
		session, err := s.sessionFromRequest(req)
		if err != nil {
			return Response{Error: err.Error()}
		}
		path, err := s.PasteImage(session)
		if err != nil {
			return Response{Error: err.Error()}
		}
		return Response{Path: path}
	case "register_session":
		machine := strings.TrimSpace(req.Machine)
		if machine == "" {
			machine = strings.TrimSpace(req.Host)
		}
		if machine == "" {
			return Response{Error: "register_session requires machine"}
		}
		if _, ok := s.Config.HostByName(machine); !ok {
			return Response{Error: fmt.Sprintf("machine %q not found in config", machine)}
		}
		session, err := s.sessionFromRequest(req)
		if err != nil {
			return Response{Error: err.Error()}
		}
		s.registerSession(session, sessionBinding{
			Machine:   machine,
			SSHAlias:  strings.TrimSpace(req.SSHAlias),
			SSHHost:   strings.TrimSpace(req.SSHHost),
			SSHPort:   strings.TrimSpace(req.SSHPort),
			SSHUser:   strings.TrimSpace(req.SSHUser),
			CreatedAt: time.Now(),
		})
		return Response{}
	case "status":
		return Response{Status: s.Status()}
	default:
		return Response{Error: "unknown command"}
	}
}

func (s *Server) sessionFromRequest(req Request) (terminal.Session, error) {
	if key := strings.TrimSpace(req.SessionKey); key != "" {
		return terminal.Session{SessionKey: key}, nil
	}
	return s.Sessions.ActiveSession()
}

func (s *Server) PasteImage(session terminal.Session) (string, error) {
	start := time.Now()
	if s.Config == nil {
		return "", errors.New("server has no config")
	}
	host, ok := s.resolveSessionHost(session)
	if !ok {
		if session.DetectedHost == "" {
			if active, err := s.Sessions.ActiveSession(); err == nil {
				session = active
				host, ok = s.resolveSessionHost(session)
			}
		}
		if !ok {
			return "", s.sessionMatchError(session)
		}
	}
	data, err := s.Images.ReadPNG()
	if err != nil {
		return "", err
	}
	route := s.Routes.BestRoute(host)
	localUser := "unknown"
	if u, err := user.Current(); err == nil && u.Username != "" {
		localUser = filepath.Base(u.Username)
	}
	path, err := s.Uploader.Upload(data, localUser, host, route)
	if err != nil {
		return "", err
	}
	s.recordTransfer(Transfer{
		Host:      host.Name,
		Route:     route.Name,
		Target:    route.SSHTarget,
		Path:      path,
		Bytes:     len(data),
		CreatedAt: time.Now().Format(time.RFC3339),
	})
	_ = recordRegistry(host.Name, route.Name, path, time.Since(start))
	return path, nil
}

func (s *Server) resolveSessionHost(session terminal.Session) (config.Host, bool) {
	s.mu.RLock()
	binding := s.registered[session.SessionKey]
	s.mu.RUnlock()
	if binding.Machine != "" {
		if host, ok := s.Config.ResolveHost(binding.Machine); ok {
			return host, true
		}
	}
	return s.Config.ResolveHost(session.DetectedHost)
}

func (s *Server) Status() Status {
	st := Status{}
	if s.Config != nil {
		for _, h := range s.Config.Hosts {
			st.ConfigHosts = append(st.ConfigHosts, h.Name)
		}
	}
	s.mu.RLock()
	st.Registered = len(s.registered)
	st.Recent = append([]Transfer(nil), s.recent...)
	st.RecentBindings = append([]SessionBinding(nil), s.recentBinds...)
	s.mu.RUnlock()
	return st
}

func (s *Server) recordTransfer(t Transfer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recent = append([]Transfer{t}, s.recent...)
	if len(s.recent) > 10 {
		s.recent = s.recent[:10]
	}
}

func (s *Server) registerSession(session terminal.Session, binding sessionBinding) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.registered[session.SessionKey] = binding
	s.recentBinds = append([]SessionBinding{{
		SessionKey: session.SessionKey,
		Machine:    binding.Machine,
		SSHAlias:   binding.SSHAlias,
		SSHHost:    binding.SSHHost,
		SSHPort:    binding.SSHPort,
		SSHUser:    binding.SSHUser,
		CreatedAt:  binding.CreatedAt.Format(time.RFC3339),
	}}, s.recentBinds...)
	if len(s.recentBinds) > 10 {
		s.recentBinds = s.recentBinds[:10]
	}
}

func (s *Server) sessionMatchError(session terminal.Session) error {
	var details []string
	if session.RawTitle != "" {
		details = append(details, fmt.Sprintf("title %q", session.RawTitle))
	}
	if session.DetectedHost != "" {
		details = append(details, fmt.Sprintf("detected host %q", session.DetectedHost))
	} else {
		details = append(details, "detected host unavailable")
	}
	machines := s.configHostNames()
	if len(machines) > 0 {
		details = append(details, fmt.Sprintf("configured machines: %s", strings.Join(machines, ", ")))
	}
	details = append(details, `run: clipport session register --machine <name>`)
	return fmt.Errorf("could not match active iTerm session (%s)", strings.Join(details, "; "))
}

func (s *Server) configHostNames() []string {
	if s.Config == nil {
		return nil
	}
	names := make([]string, 0, len(s.Config.Hosts))
	for _, host := range s.Config.Hosts {
		names = append(names, host.Name)
	}
	sort.Strings(names)
	return names
}

func recordRegistry(hostName, routeName, path string, latency time.Duration) error {
	reg, err := registry.Load("")
	if err != nil {
		return err
	}
	reg.UpdateHost(hostName, func(st registry.HostState) registry.HostState {
		st.LastHealthyRoute = routeName
		st.LastPastePath = path
		st.LastPasteLatency = latency.Round(time.Millisecond).String()
		st.LastPasteAt = time.Now().Format(time.RFC3339)
		return st
	})
	return reg.Save("")
}
