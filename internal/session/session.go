package session

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Session struct {
	ID          string    `json:"id"`
	Name        string    `json:"name,omitempty"`
	Model       string    `json:"model"`
	Task        string    `json:"task"`
	Cwd         string    `json:"cwd"`
	TmuxSession string    `json:"tmux_session"`
	LogFile     string    `json:"log_file"`
	ScriptFile  string    `json:"script_file"`
	TaskFile    string    `json:"task_file"`
	RuntimeFile string    `json:"runtime_file,omitempty"`
	CancelFile  string    `json:"cancel_file,omitempty"`
	StartedAt   time.Time `json:"started_at"`
	Turns       int       `json:"turns,omitempty"`
	TotalCost   float64   `json:"total_cost,omitempty"`
}

// Label returns the display name for monitor output.
// Uses Name if set, otherwise falls back to Model.
func (s *Session) Label() string {
	if s.Name != "" {
		return s.Name
	}
	return s.Model
}

func NewID() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func DataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "agentctl"), nil
}

func Save(s *Session) error {
	dir, err := DataDir()
	if err != nil {
		return err
	}
	hydrateDerivedPaths(s, dir)
	if err := os.MkdirAll(filepath.Join(dir, "sessions"), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	final := sessionFilePath(dir, s.ID)
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func Load(id string) (*Session, error) {
	dir, err := DataDir()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(sessionFilePath(dir, id))
	if err == nil {
		var s Session
		if err := json.Unmarshal(data, &s); err != nil {
			return nil, err
		}
		hydrateDerivedPaths(&s, dir)
		return &s, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	return recoverSession(dir, id)
}

func List() ([]*Session, error) {
	dir, err := DataDir()
	if err != nil {
		return nil, err
	}

	sessionsByID := make(map[string]*Session)

	sessDir := filepath.Join(dir, "sessions")
	entries, err := os.ReadDir(sessDir)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		s, err := Load(id)
		if err != nil {
			continue
		}
		sessionsByID[id] = s
	}

	logDir := filepath.Join(dir, "logs")
	logEntries, err := os.ReadDir(logDir)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	for _, e := range logEntries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".log" {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".log")
		if _, ok := sessionsByID[id]; ok {
			continue
		}
		s, err := recoverSession(dir, id)
		if err != nil {
			continue
		}
		sessionsByID[id] = s
	}

	var sessions []*Session
	for _, s := range sessionsByID {
		sessions = append(sessions, s)
	}
	return sessions, nil
}

func Remove(id string) error {
	dir, err := DataDir()
	if err != nil {
		return err
	}
	return os.Remove(sessionFilePath(dir, id))
}

func sessionFilePath(dir, id string) string {
	return filepath.Join(dir, "sessions", id+".json")
}

func hydrateDerivedPaths(s *Session, dir string) {
	if s == nil || s.ID == "" {
		return
	}
	if s.TmuxSession == "" {
		s.TmuxSession = "agentctl-" + s.ID
	}
	if s.LogFile == "" {
		s.LogFile = filepath.Join(dir, "logs", s.ID+".log")
	}
	if s.ScriptFile == "" {
		s.ScriptFile = filepath.Join(dir, "scripts", s.ID+".sh")
	}
	if s.TaskFile == "" {
		s.TaskFile = filepath.Join(dir, "scripts", s.ID+".task")
	}
	if s.RuntimeFile == "" {
		s.RuntimeFile = filepath.Join(dir, "runtime", s.ID+".json")
	}
	if s.CancelFile == "" {
		s.CancelFile = filepath.Join(dir, "runtime", s.ID+".cancelled")
	}
}
