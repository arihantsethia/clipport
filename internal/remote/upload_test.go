package remote

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Run the exact remote command locally, without making an SSH connection.
func runUploadScript(t *testing.T, filename, data string) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	upload := sshCatUploadCommand([]byte(data), "unused", filename)
	cmd := exec.CommandContext(ctx, "sh", "-c", "umask 022; "+upload.Args[len(upload.Args)-1])
	cmd.Stdin = upload.Stdin
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("upload command timed out: %s", out)
	}
	if err != nil {
		t.Logf("upload command: %s", out)
	}
	return err
}

func TestUploadScriptUsesPrivatePermissions(t *testing.T) {
	for _, existing := range []bool{false, true} {
		name := "new directories"
		if existing {
			name = "existing public directories"
		}
		t.Run(name, func(t *testing.T) {
			base := filepath.Join(t.TempDir(), "clipport's uploads")
			dir := filepath.Join(base, "user name")
			filename := filepath.Join(dir, "clipboard.png")
			if existing {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if err := runUploadScript(t, filename, "private image"); err != nil {
				t.Fatal(err)
			}
			for path, want := range map[string]os.FileMode{base: 0o700, dir: 0o700, filename: 0o600} {
				info, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				if got := info.Mode().Perm(); got != want {
					t.Errorf("permissions for %s = %04o, want %04o", path, got, want)
				}
			}
			data, err := os.ReadFile(filename)
			if err != nil || string(data) != "private image" {
				t.Fatalf("uploaded content = %q, error = %v", data, err)
			}
		})
	}
}

func TestUploadScriptRejectsExistingPaths(t *testing.T) {
	for _, kind := range []string{"file", "directory", "FIFO", "hard link", "file symlink", "dangling symlink", "base symlink", "user symlink"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			base := filepath.Join(root, "clipport")
			dir := filepath.Join(base, "dev")
			filename := filepath.Join(dir, "clipboard.png")
			outside := filepath.Join(root, "outside")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(outside, 0o755); err != nil {
				t.Fatal(err)
			}
			sentinel := filepath.Join(outside, "original.png")
			if err := os.WriteFile(sentinel, []byte("original"), 0o644); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "file":
				if err := os.WriteFile(filename, []byte("original"), 0o644); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(filename, 0o755); err != nil {
					t.Fatal(err)
				}
			case "FIFO":
				if err := syscall.Mkfifo(filename, 0o600); err != nil {
					t.Fatal(err)
				}
			case "hard link":
				if err := os.Link(sentinel, filename); err != nil {
					t.Fatal(err)
				}
			case "file symlink":
				if err := os.Symlink(sentinel, filename); err != nil {
					t.Fatal(err)
				}
			case "dangling symlink":
				if err := os.Symlink(filepath.Join(outside, "missing"), filename); err != nil {
					t.Fatal(err)
				}
			case "base symlink":
				if err := os.Remove(dir); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(base); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, base); err != nil {
					t.Fatal(err)
				}
			case "user symlink":
				if err := os.Remove(dir); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, dir); err != nil {
					t.Fatal(err)
				}
			}
			if err := runUploadScript(t, filename, "private image"); err == nil {
				t.Fatal("upload accepted an existing or symlinked destination")
			}
			data, err := os.ReadFile(sentinel)
			if err != nil || string(data) != "original" {
				t.Fatalf("outside file changed: %q, error = %v", data, err)
			}
			if kind == "file" {
				data, err := os.ReadFile(filename)
				if err != nil || string(data) != "original" {
					t.Fatalf("existing file changed: %q, error = %v", data, err)
				}
			}
			if _, err := os.Stat(filepath.Join(outside, "missing")); !os.IsNotExist(err) {
				t.Fatalf("dangling symlink target was created: %v", err)
			}
		})
	}
}

func TestRemotePathRejectsDotDirectoryNames(t *testing.T) {
	for _, user := range []string{".", "..", " ../ "} {
		got := RemotePath(user, "png", time.Unix(0, 0))
		if !strings.HasPrefix(got, "/tmp/clipport/") || filepath.Dir(got) == "/tmp/clipport" {
			t.Errorf("RemotePath(%q) escaped the user directory: %q", user, got)
		}
	}
}

func TestUploadScriptDoesNotClobberConcurrentUpload(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "clipport", "dev", "clipboard.png")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, data := range []string{"first image", "second image"} {
		go func() {
			<-start
			upload := sshCatUploadCommand([]byte(data), "unused", filename)
			cmd := exec.CommandContext(ctx, "sh", "-c", upload.Args[len(upload.Args)-1])
			cmd.Stdin = upload.Stdin
			results <- cmd.Run()
		}()
	}
	close(start)
	successes := 0
	for range 2 {
		if err := <-results; err == nil {
			successes++
		}
	}
	if ctx.Err() != nil {
		t.Fatal(ctx.Err())
	}
	if successes != 1 {
		t.Fatalf("successful uploads = %d, want 1", successes)
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "first image" && string(data) != "second image" {
		t.Fatalf("upload was truncated or combined: %q", data)
	}
}
