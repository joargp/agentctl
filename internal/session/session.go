package session

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
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
	if err := os.MkdirAll(filepath.Join(dir, "sessions"), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "sessions", s.ID+".json"), data, 0o644)
}

func Load(id string) (*Session, error) {
	dir, err := DataDir()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "sessions", id+".json"))
	if err != nil {
		return nil, err
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func List() ([]*Session, error) {
	dir, err := DataDir()
	if err != nil {
		return nil, err
	}
	sessDir := filepath.Join(dir, "sessions")
	entries, err := os.ReadDir(sessDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var sessions []*Session
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		id := e.Name()[:len(e.Name())-5]
		s, err := Load(id)
		if err != nil {
			continue
		}
		sessions = append(sessions, s)
	}
	return sessions, nil
}

func Remove(id string) error {
	dir, err := DataDir()
	if err != nil {
		return err
	}
	return os.Remove(filepath.Join(dir, "sessions", id+".json"))
}
