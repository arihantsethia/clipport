package remote

import (
	"bytes"
	"fmt"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/arihantsethia/clipport/internal/config"
)

type Uploader struct {
	Runner func(data []byte, target, remotePath string) error
	Now    func() time.Time
}

func RemotePath(localUser, ext string, now time.Time) string {
	user := cleanPathPart(localUser)
	ext = strings.TrimPrefix(strings.TrimSpace(ext), ".")
	if ext == "" {
		ext = "bin"
	}
	filename := fmt.Sprintf("clipboard-%s.%s", now.Format("20060102-150405.000000"), cleanPathPart(ext))
	return path.Join("/tmp/clipport", user, filename)
}

func (u Uploader) Upload(data []byte, localUser string, _ config.Host, route config.Route, ext string) (string, error) {
	now := time.Now
	if u.Now != nil {
		now = u.Now
	}
	remotePath := RemotePath(localUser, ext, now())
	runner := u.Runner
	if runner == nil {
		runner = sshCatUpload
	}
	if err := runner(data, route.SSHTarget, remotePath); err != nil {
		return "", err
	}
	return remotePath, nil
}

func (u Uploader) UploadWithRetry(data []byte, localUser string, host config.Host, route config.Route, ext string, refresh func() config.Route) (string, config.Route, error) {
	path, err := u.Upload(data, localUser, host, route, ext)
	if err == nil {
		return path, route, nil
	}
	if refresh == nil {
		return "", route, err
	}
	fresh := refresh()
	if fresh.Name == "" || fresh.Name == route.Name {
		return "", route, err
	}
	path, retryErr := u.Upload(data, localUser, host, fresh, ext)
	if retryErr != nil {
		return "", fresh, retryErr
	}
	return path, fresh, nil
}

func sshCatUpload(data []byte, target, remotePath string) error {
	cmd := sshCatUploadCommand(data, target, remotePath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("upload to %s failed: %w: %s", target, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Work from checked directory inodes rather than reopening the full path for
// the write. Refuse symlinks and directories owned by another remote account.
const privateUploadScript = `set -eu
umask 077
enter_private_dir() {
    parent=$(pwd -P)
    mkdir -m 700 "./$1" 2>/dev/null || test -d "./$1"
    test ! -L "./$1" || { echo "clipport: symlinked upload directory" >&2; exit 1; }
    cd "./$1"
    test "$(pwd -P)" = "$parent/$1" || { echo "clipport: upload directory changed" >&2; exit 1; }
    test -O . || { echo "clipport: upload directory belongs to another account" >&2; exit 1; }
    chmod 700 .
}
cd "$1"
enter_private_dir "$2"
enter_private_dir "$3"
test ! -e "$4" && test ! -L "$4" || { echo "clipport: upload destination already exists" >&2; exit 1; }
set -C
cat > "$4"`

func sshCatUploadCommand(data []byte, target, remotePath string) *exec.Cmd {
	dir := path.Dir(remotePath)
	base := path.Dir(dir)
	// An explicit sh keeps the privacy checks independent of the login shell.
	script := "sh -c " + shellQuote(privateUploadScript) + " clipport-upload " +
		shellQuote(path.Dir(base)) + " " + shellQuote(path.Base(base)) + " " +
		shellQuote(path.Base(dir)) + " " + shellQuote(path.Base(remotePath))
	cmd := exec.Command(
		"ssh",
		"-o", "PermitLocalCommand=no",
		"-o", "ClearAllForwardings=yes",
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=3",
		"-o", "ConnectionAttempts=1",
		"-o", "ServerAliveInterval=5",
		"-o", "ServerAliveCountMax=1",
		"-o", "ControlMaster=no",
		target,
		script,
	)
	cmd.Stdin = bytes.NewReader(data)
	return cmd
}

func cleanPathPart(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "unknown"
	}
	var b strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	part := b.String()
	if part == "." || part == ".." {
		return "unknown"
	}
	return part
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
