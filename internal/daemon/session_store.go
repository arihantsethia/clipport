package daemon

import (
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

const sessionBindingTTL = 30 * 24 * time.Hour

func defaultSessionStorePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".cache", "clipport", "sessions.db")
}

func (s *Server) loadSessionBindings() error {
	if s.sessionStorePath == "" {
		return nil
	}
	db, err := openSessionDB(s.sessionStorePath)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := pruneSessionDB(db, time.Now()); err != nil {
		return err
	}
	rows, err := db.Query(`
SELECT session_key, machine, ssh_alias, ssh_host, ssh_port, ssh_user, created_at, last_used
FROM session_bindings
ORDER BY last_used DESC`)
	if err != nil {
		return err
	}
	defer rows.Close()

	loaded := map[string]sessionBinding{}
	for rows.Next() {
		var key string
		var binding sessionBinding
		var createdAt, lastUsed string
		if err := rows.Scan(&key, &binding.Machine, &binding.SSHAlias, &binding.SSHHost, &binding.SSHPort, &binding.SSHUser, &createdAt, &lastUsed); err != nil {
			return err
		}
		binding.CreatedAt = parseSessionTime(createdAt)
		binding.LastUsed = parseSessionTime(lastUsed)
		if key != "" && binding.Machine != "" {
			loaded[key] = binding
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.registered = loaded
	s.recentBinds = recentBindingsFromMap(s.registered)
	return nil
}

func (s *Server) saveSessionBindings() error {
	if s.sessionStorePath == "" {
		return nil
	}
	s.mu.Lock()
	bindings := make(map[string]sessionBinding, len(s.registered))
	now := time.Now()
	for key, binding := range s.registered {
		if key != "" && binding.Machine != "" && !bindingLastUsed(binding).Before(now.Add(-sessionBindingTTL)) {
			bindings[key] = binding
		}
	}
	s.registered = bindings
	s.recentBinds = recentBindingsFromMap(s.registered)
	s.mu.Unlock()

	db, err := openSessionDB(s.sessionStorePath)
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM session_bindings`); err != nil {
		_ = tx.Rollback()
		return err
	}
	for key, binding := range bindings {
		if _, err := tx.Exec(`
INSERT INTO session_bindings (session_key, machine, ssh_alias, ssh_host, ssh_port, ssh_user, created_at, last_used)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			key,
			binding.Machine,
			binding.SSHAlias,
			binding.SSHHost,
			binding.SSHPort,
			binding.SSHUser,
			formatSessionTime(binding.CreatedAt),
			formatSessionTime(bindingLastUsed(binding)),
		); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func openSessionDB(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`
PRAGMA busy_timeout = 1000;
CREATE TABLE IF NOT EXISTS session_bindings (
	session_key TEXT PRIMARY KEY,
	machine TEXT NOT NULL,
	ssh_alias TEXT NOT NULL DEFAULT '',
	ssh_host TEXT NOT NULL DEFAULT '',
	ssh_port TEXT NOT NULL DEFAULT '',
	ssh_user TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	last_used TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS session_bindings_last_used_idx ON session_bindings(last_used);`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func pruneSessionDB(db *sql.DB, now time.Time) error {
	_, err := db.Exec(`DELETE FROM session_bindings WHERE last_used < ? OR machine = ''`, formatSessionTime(now.Add(-sessionBindingTTL)))
	return err
}

func recentBindingsFromMap(bindings map[string]sessionBinding) []SessionBinding {
	recent := make([]SessionBinding, 0, len(bindings))
	for key, binding := range bindings {
		recent = append(recent, SessionBinding{
			SessionKey: key,
			Machine:    binding.Machine,
			SSHAlias:   binding.SSHAlias,
			SSHHost:    binding.SSHHost,
			SSHPort:    binding.SSHPort,
			SSHUser:    binding.SSHUser,
			CreatedAt:  formatSessionTime(binding.CreatedAt),
		})
	}
	sort.Slice(recent, func(i, j int) bool {
		return bindingLastUsed(bindings[recent[i].SessionKey]).After(bindingLastUsed(bindings[recent[j].SessionKey]))
	})
	if len(recent) > 10 {
		recent = recent[:10]
	}
	return recent
}

func bindingLastUsed(binding sessionBinding) time.Time {
	if !binding.LastUsed.IsZero() {
		return binding.LastUsed
	}
	return binding.CreatedAt
}

func formatSessionTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339Nano)
}

func parseSessionTime(text string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return time.Time{}
	}
	return t
}
