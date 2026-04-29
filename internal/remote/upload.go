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

func sshCatUpload(data []byte, target, remotePath string) error {
	dir := path.Dir(remotePath)
	cmd := exec.Command("ssh", target, "mkdir -p "+shellQuote(dir)+" && cat > "+shellQuote(remotePath))
	cmd.Stdin = bytes.NewReader(data)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("upload to %s failed: %w: %s", target, err, strings.TrimSpace(string(out)))
	}
	return nil
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
	return b.String()
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
