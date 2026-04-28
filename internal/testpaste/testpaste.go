package testpaste

import (
	"encoding/base64"
	"fmt"
	"os/user"
	"path/filepath"
	"time"

	"github.com/arihantsethia/clipport/internal/config"
	"github.com/arihantsethia/clipport/internal/registry"
	"github.com/arihantsethia/clipport/internal/remote"
)

const onePixelPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/p9sAAAAASUVORK5CYII="

type Result struct {
	Host     string
	Route    string
	Target   string
	Path     string
	Upload   time.Duration
	Verify   time.Duration
	Total    time.Duration
	Verified bool
}

func Run(cfg *config.Config, hostName string) (Result, error) {
	start := time.Now()
	host, ok := resolveHost(cfg, hostName)
	if !ok {
		return Result{}, fmt.Errorf("host %q not found", hostName)
	}
	data, err := base64.StdEncoding.DecodeString(onePixelPNG)
	if err != nil {
		return Result{}, err
	}
	localUser := "unknown"
	if u, err := user.Current(); err == nil && u.Username != "" {
		localUser = filepath.Base(u.Username)
	}
	route, cached := cachedRoute(host)
	if route.Name == "" {
		route = remote.NewManager(nil).BestRoute(host)
	}
	result, err := runWithRoute(start, host, route, data, localUser)
	if err != nil && cached {
		fresh := remote.NewManager(nil).BestRoute(host)
		if fresh.Name != "" && fresh.Name != route.Name {
			result, err = runWithRoute(start, host, fresh, data, localUser)
		}
	}
	if err != nil {
		return Result{}, err
	}
	_ = record(result)
	return result, nil
}

func runWithRoute(start time.Time, host config.Host, route config.Route, data []byte, localUser string) (Result, error) {
	uploadStart := time.Now()
	path, err := remote.Uploader{}.Upload(data, localUser, host, route)
	if err != nil {
		return Result{}, err
	}
	verifyStart := time.Now()
	if err := remote.VerifyFile(route.SSHTarget, path); err != nil {
		return Result{}, err
	}
	result := Result{
		Host:     host.Name,
		Route:    route.Name,
		Target:   route.SSHTarget,
		Path:     path,
		Upload:   verifyStart.Sub(uploadStart),
		Verify:   time.Since(verifyStart),
		Total:    time.Since(start),
		Verified: true,
	}
	return result, nil
}

func Print(r Result) {
	fmt.Printf("host       %s\n", r.Host)
	fmt.Printf("route      %s (%s)\n", r.Route, r.Target)
	fmt.Printf("upload     %s\n", r.Upload.Round(time.Millisecond))
	fmt.Printf("verify     %s\n", r.Verify.Round(time.Millisecond))
	fmt.Printf("total      %s\n", r.Total.Round(time.Millisecond))
	fmt.Printf("path       %s\n", r.Path)
}

func resolveHost(cfg *config.Config, name string) (config.Host, bool) {
	if name != "" {
		return cfg.HostByName(name)
	}
	if cfg.DefaultHost != "" {
		return cfg.HostByName(cfg.DefaultHost)
	}
	if len(cfg.Hosts) > 0 {
		return cfg.Hosts[0], true
	}
	return config.Host{}, false
}

func cachedRoute(host config.Host) (config.Route, bool) {
	reg, err := registry.Load("")
	if err != nil {
		return config.Route{}, false
	}
	name := reg.Hosts[host.Name].LastHealthyRoute
	if name == "" {
		return config.Route{}, false
	}
	for _, route := range host.SortedRoutes() {
		if route.Name == name {
			return route, true
		}
	}
	return config.Route{}, false
}

func record(r Result) error {
	reg, err := registry.Load("")
	if err != nil {
		return err
	}
	reg.UpdateHost(r.Host, func(st registry.HostState) registry.HostState {
		st.LastHealthyRoute = r.Route
		st.LastPastePath = r.Path
		st.LastPasteLatency = r.Total.Round(time.Millisecond).String()
		st.LastPasteAt = time.Now().Format(time.RFC3339)
		return st
	})
	return reg.Save("")
}
