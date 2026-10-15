package shims

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Opt in on a Linux host with OpenSSH installed. Everything, including host and
// client keys and authorized_keys, is temporary; no account setup is modified.
func TestLinuxSSHIndependentClients(t *testing.T) {
	if os.Getenv("CLIPPORT_SSH_INTEGRATION") != "1" {
		t.Skip("set CLIPPORT_SSH_INTEGRATION=1 for isolated loopback SSH integration")
	}
	home := linuxHome(t)
	for _, cmd := range []string{"ssh", "ssh-keygen", "/usr/sbin/sshd"} {
		if _, err := exec.LookPath(cmd); err != nil {
			t.Fatal(err)
		}
	}
	current, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	makeKey := func(name string) string {
		path := filepath.Join(dir, name)
		if out, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", path).CombinedOutput(); err != nil {
			t.Fatalf("keygen: %v: %s", err, out)
		}
		return path
	}
	hostKey := makeKey("host")
	clientKeys := []string{makeKey("client-a"), makeKey("client-b")}
	auth := readFile(t, clientKeys[0]+".pub") + readFile(t, clientKeys[1]+".pub")
	write := func(path, data string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(dir, "authorized_keys"), auth)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	// A forced command confines every session to the disposable target HOME.
	config := fmt.Sprintf("ListenAddress 127.0.0.1\nPort %d\nHostKey %s\nPidFile %s\nAuthorizedKeysFile %s\nPasswordAuthentication no\nKbdInteractiveAuthentication no\nUsePAM no\nStrictModes no\nAllowUsers %s\nForceCommand env HOME='%s' sh\nLogLevel ERROR\n", port, hostKey, filepath.Join(dir, "pid"), filepath.Join(dir, "authorized_keys"), current.Username, home)
	configPath := filepath.Join(dir, "sshd_config")
	write(configPath, config)
	logFile, err := os.Create(filepath.Join(dir, "sshd.log"))
	if err != nil {
		t.Fatal(err)
	}
	server := exec.Command("/usr/sbin/sshd", "-D", "-e", "-f", configPath)
	server.Stdout = logFile
	server.Stderr = logFile
	if err := server.Start(); err != nil {
		logFile.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { server.Process.Kill(); server.Wait(); logFile.Close() })
	ready := false
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 100*time.Millisecond)
		if err == nil {
			conn.Close()
			ready = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !ready {
		t.Fatalf("temporary sshd did not start: %s", readFile(t, filepath.Join(dir, "sshd.log")))
	}
	// Pin the known temporary host key instead of weakening host authentication.
	knownHosts := filepath.Join(dir, "known_hosts")
	write(knownHosts, fmt.Sprintf("[127.0.0.1]:%d %s", port, readFile(t, hostKey+".pub")))
	configs := make([]string, 2)
	tokens := []string{"independent_ssh_client_A", "independent_ssh_client_B"}
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/clipboard" || r.Header.Get("Authorization") != "Bearer "+tokens[0] {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, "original client's clipboard still works")
	}))
	defer fixture.Close()
	remoteListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	clipboardRemotePort := remoteListener.Addr().(*net.TCPAddr).Port
	remoteListener.Close()

	for i, key := range clientKeys {
		clientHome := filepath.Join(dir, fmt.Sprintf("client-%d", i))
		if err := os.Mkdir(clientHome, 0700); err != nil {
			t.Fatal(err)
		}
		configs[i] = filepath.Join(clientHome, "ssh_config")
		write(configs[i], fmt.Sprintf("Host target\n HostName 127.0.0.1\n Port %d\n User %s\n IdentityFile %s\n IdentitiesOnly yes\n BatchMode yes\n StrictHostKeyChecking yes\n UserKnownHostsFile %s\n ConnectTimeout 5\n", port, current.Username, key, knownHosts))
	}
	run := func(i int) (string, error) {
		cmd := exec.Command("ssh", "-F", configs[i], "-o", "PermitLocalCommand=no", "-o", "ClearAllForwardings=yes", "target", "sh")
		cmd.Stdin = strings.NewReader(renderInstallPayload(tokens[i], clipboardRemotePort))
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	if out, err := run(0); err != nil {
		t.Fatalf("first SSH install: %v: %s; server: %s", err, out, readFile(t, filepath.Join(dir, "sshd.log")))
	}
	beforeToken := readFile(t, filepath.Join(home, ".config/clipport/token"))
	beforeShim := readFile(t, filepath.Join(home, ".local/bin/clipport-wl-paste"))
	out, err := run(1)
	if err == nil || !strings.Contains(out, "remote token differs") {
		t.Fatalf("second SSH identity did not refuse: %v: %s", err, out)
	}
	for _, token := range tokens {
		if strings.Contains(out, token) {
			t.Fatal("SSH refusal leaked token")
		}
	}
	if readFile(t, filepath.Join(home, ".config/clipport/token")) != beforeToken || readFile(t, filepath.Join(home, ".local/bin/clipport-wl-paste")) != beforeShim {
		t.Fatal("second SSH client changed protected state")
	}

	// Exercise the installed shim over an actual reverse SSH forward after the
	// second client's refusal, including the first client's bearer authentication.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	forward := fmt.Sprintf("127.0.0.1:%d:%s", clipboardRemotePort, strings.TrimPrefix(fixture.URL, "http://"))
	readCmd := exec.CommandContext(ctx, "ssh", "-F", configs[0], "-o", "PermitLocalCommand=no", "-o", "ExitOnForwardFailure=yes", "-R", forward, "target", "sh")
	readCmd.Stdin = strings.NewReader("exec \"$HOME/.local/bin/wl-paste\"\n")
	clipboard, err := readCmd.CombinedOutput()
	if err != nil || string(clipboard) != "original client's clipboard still works" {
		t.Fatalf("first client's clipboard failed after refusal: %v: %s", err, clipboard)
	}
	if out, err := run(0); err != nil {
		t.Fatalf("same SSH client retry: %v: %s", err, out)
	}
}
