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

type ClipboardReader interface {
	Read() (clipboard.Item, error)
}

type PasteExecutor interface {
	Paste() error
}

const PasteUnavailable = "clipctl: paste unavailable"

type Server struct {
	Config    *config.Config
	Sessions  terminal.ActiveSessionProvider
	Clipboard ClipboardReader
	Paster    PasteExecutor
	Routes    *remote.Manager
	Uploader  remote.Uploader

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
		Clipboard:  clipboard.Provider{},
		Paster:     AppleScriptPaster{},
		Routes:     remote.NewManager(nil),
		registered: map[string]sessionBinding{},
	}
	for _, h := range cfg.Hosts {
		s.Routes.WarmHost(h)
	}
	return s
}

func (s *Server) Listen(socketPath string) error {
	if err := prepareUnixSocket(socketPath); err != nil {
		return err
	}
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

func prepareUnixSocket(socketPath string) error {
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o700); err != nil {
		return err
	}
	conn, err := net.DialTimeout("unix", socketPath, 100*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		return fmt.Errorf("daemon socket already in use: %s", socketPath)
	}
	if !errors.Is(err, os.ErrNotExist) {
		if removeErr := os.Remove(socketPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return removeErr
		}
	}
	return nil
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
	case "paste":
		session, err := s.sessionFromRequest(req)
		if err != nil {
			return pasteError(err)
		}
		resp, err := s.Paste(session)
		if err != nil {
			return pasteError(err)
		}
		return resp
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
		return terminal.Session{SessionKey: key, DetectedHost: strings.TrimSpace(req.Host)}, nil
	}
	return s.Sessions.ActiveSession()
}

func (s *Server) Paste(session terminal.Session) (Response, error) {
	start := time.Now()
	if s.Config == nil {
		return Response{}, errors.New("server has no config")
	}
	host, ok := s.resolveSessionHost(session)
	if !ok {
		if session.DetectedHost == "" && s.Sessions != nil {
			if active, err := s.Sessions.ActiveSession(); err == nil {
				session = active
				host, ok = s.resolveSessionHost(session)
			}
		}
		if !ok {
			if session.Kind != terminal.SessionLocal {
				return Response{}, s.sessionMatchError(session)
			}
			if s.Paster == nil {
				return Response{}, s.sessionMatchError(session)
			}
			if err := s.Paster.Paste(); err != nil {
				return Response{}, fmt.Errorf("local paste failed: %w", err)
			}
			return Response{}, nil
		}
	}
	if s.Clipboard == nil {
		return Response{}, errors.New("server has no clipboard reader")
	}
	item, err := s.Clipboard.Read()
	if err != nil {
		return Response{}, err
	}
	if item.Kind == clipboard.KindText {
		return Response{Text: string(item.Data)}, nil
	}
	route := s.Routes.BestRoute(host)
	localUser := "unknown"
	if u, err := user.Current(); err == nil && u.Username != "" {
		localUser = filepath.Base(u.Username)
	}
	path, usedRoute, err := s.Uploader.UploadWithRetry(item.Data, localUser, host, route, extensionForKind(item.Kind), func() config.Route {
		s.Routes.InvalidateHost(host.Name)
		return s.Routes.BestRoute(host)
	})
	if err != nil {
		return Response{}, err
	}
	s.recordTransfer(Transfer{
		Host:      host.Name,
		Route:     usedRoute.Name,
		Target:    usedRoute.SSHTarget,
		Path:      path,
		Bytes:     len(item.Data),
		CreatedAt: time.Now().Format(time.RFC3339),
	})
	_ = recordRegistry(host.Name, usedRoute.Name, path, time.Since(start))
	return Response{Path: path}, nil
}

func extensionForKind(kind clipboard.Kind) string {
	return "png"
}

func pasteError(err error) Response {
	return Response{Error: PasteUnavailable, Debug: err.Error()}
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
	if strings.TrimSpace(session.DetectedHost) == "" {
		return config.Host{}, false
	}
	return s.Config.ResolveHost(session.DetectedHost)
}

func (s *Server) Status() Status {
	st := Status{}
	if s.Config != nil {
		for _, h := range s.Config.Hosts {
			st.ConfigHosts = append(st.ConfigHosts, h.Name)
			hostStatus := HostStatus{Name: h.Name}
			if s.Routes != nil {
				route := s.Routes.CachedRoute(h)
				hostStatus.Route = route.Name
				hostStatus.Target = route.SSHTarget
			}
			st.Hosts = append(st.Hosts, hostStatus)
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
	details = append(details, `run: clipctl session register --machine <name>`)
	return fmt.Errorf("failed to match active iTerm session (%s)", strings.Join(details, "; "))
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
