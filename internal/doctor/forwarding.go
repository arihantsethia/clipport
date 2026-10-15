package doctor

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"time"

	"github.com/arihantsethia/clipport/internal/config"
	"github.com/arihantsethia/clipport/internal/remote"
)

func readSSHConfig(path, target string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	args := []string{"-G"}
	if path != "" {
		args = append(args, "-F", path)
	}
	args = append(args, "--", target)
	out, err := exec.CommandContext(ctx, "ssh", args...).Output()
	return string(out), err
}

func checkForward(hostName string, route config.Route, addr, sshConfigPath string) Check {
	name := fmt.Sprintf("forward %s/%s", hostName, route.Name)
	effective, err := readSSHConfig(sshConfigPath, route.SSHTarget)
	if err != nil {
		return Check{Name: name, Detail: fmt.Sprintf("cannot inspect SSH forwarding configuration: %v", err)}
	}
	remoteAddr, configured, err := forwardAddress(effective, addr)
	if err != nil {
		return Check{Name: name, Detail: err.Error()}
	}
	if !configured {
		return Check{Name: name, OK: true, Skipped: true, Detail: "optional clipboard tunnel not configured; shortcut paste does not need it"}
	}
	return checkRemoteForward(hostName, route, remoteAddr)
}

// OpenSSH prints endpoints as [address]:port. Never send the remote token
// through a tunnel unless its destination is the expected local loopback HTTP
// endpoint. A different remote port is supported, but a conflicting forwarding
// rule is an error rather than an optional skip or an authenticated probe.
func forwardAddress(effective, addr string) (string, bool, error) {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", false, fmt.Errorf("invalid clipboard HTTP address: %w", err)
	}
	candidate := ""
	for _, line := range strings.Split(effective, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 || fields[0] != "remoteforward" {
			continue
		}
		_, listenPort, listenErr := net.SplitHostPort(fields[1])
		if listenErr != nil {
			listenPort = fields[1]
		}
		destinationHost, destinationPort, destinationErr := net.SplitHostPort(fields[2])
		if listenPort != port && (destinationErr != nil || destinationPort != port) {
			continue
		}
		destinationIP := net.ParseIP(destinationHost)
		if destinationErr != nil || destinationPort != port ||
			(destinationHost != "localhost" && (destinationIP == nil || !destinationIP.IsLoopback())) {
			return "", true, fmt.Errorf("clipboard forwarding conflict: destination is not the expected local loopback HTTP endpoint; review SSH RemoteForward (health probe not sent)")
		}
		if candidate == "" || listenPort == port {
			candidate = fields[1]
		}
	}
	if candidate == "" {
		return "", false, nil
	}
	host, remotePort, err := net.SplitHostPort(candidate)
	if err != nil {
		host, remotePort = "localhost", candidate
	}
	if host == "" || host == "*" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	if host == "::" {
		host = "::1"
	}
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return "", true, fmt.Errorf("clipboard forward uses a non-loopback remote address; review SSH RemoteForward")
	}
	return net.JoinHostPort(host, remotePort), true, nil
}

var checkForwardHealth = remote.CheckForwardHealth

func checkRemoteForward(hostName string, route config.Route, addr string) Check {
	name := fmt.Sprintf("forward %s/%s", hostName, route.Name)
	if err := checkForwardHealth(route.SSHTarget, addr); err != nil {
		return Check{Name: name, OK: false, Detail: fmt.Sprintf("%v; reconnect SSH for this host", err)}
	}
	return Check{Name: name, OK: true, Detail: fmt.Sprintf("remote can reach %s", addr)}
}
