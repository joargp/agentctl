package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func recoverSession(dir, id string) (*Session, error) {
	logFile := filepath.Join(dir, "logs", id+".log")
	if _, err := os.Stat(logFile); err != nil {
		return nil, err
	}

	s := &Session{
		ID:          id,
		LogFile:     logFile,
		ScriptFile:  filepath.Join(dir, "scripts", id+".sh"),
		TaskFile:    filepath.Join(dir, "scripts", id+".task"),
		TmuxSession: "agentctl-" + id,
	}
	if err := populateFromLog(s); err != nil {
		return nil, err
	}

	if s.StartedAt.IsZero() {
		info, err := os.Stat(logFile)
		if err == nil {
			s.StartedAt = info.ModTime()
		}
	}
	return s, nil
}

func populateFromLog(s *Session) error {
	f, err := os.Open(s.LogFile)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)

	for scanner.Scan() {
		var ev struct {
			Type      string          `json:"type"`
			Timestamp json.RawMessage `json:"timestamp"`
			Cwd       string          `json:"cwd"`
			Message   struct {
				Role      string          `json:"role"`
				Model     string          `json:"model"`
				Timestamp json.RawMessage `json:"timestamp"`
				Content   []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
				Usage struct {
					Cost struct {
						Total float64 `json:"total"`
					} `json:"cost"`
				} `json:"usage"`
			} `json:"message"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			continue
		}

		setStartedAt := func(raw json.RawMessage) {
			if len(raw) == 0 || string(raw) == "null" {
				return
			}
			var t time.Time
			var ts string
			if err := json.Unmarshal(raw, &ts); err == nil {
				parsed, err := time.Parse(time.RFC3339Nano, ts)
				if err != nil {
					return
				}
				t = parsed
			} else {
				var ms int64
				if err := json.Unmarshal(raw, &ms); err != nil {
					return
				}
				t = time.UnixMilli(ms)
			}
			if s.StartedAt.IsZero() || t.Before(s.StartedAt) {
				s.StartedAt = t
			}
		}

		switch ev.Type {
		case "session":
			setStartedAt(ev.Timestamp)
			if s.Cwd == "" {
				s.Cwd = ev.Cwd
			}
		case "message_start":
			setStartedAt(ev.Message.Timestamp)
			if s.Model == "" && ev.Message.Model != "" {
				s.Model = ev.Message.Model
			}
			if s.Task == "" && ev.Message.Role == "user" {
				var parts []string
				for _, c := range ev.Message.Content {
					if c.Type == "text" && c.Text != "" {
						parts = append(parts, c.Text)
					}
				}
				s.Task = strings.Join(parts, "\n")
			}
		case "turn_end":
			setStartedAt(ev.Message.Timestamp)
			if s.Model == "" && ev.Message.Model != "" {
				s.Model = ev.Message.Model
			}
			s.Turns++
			s.TotalCost += ev.Message.Usage.Cost.Total
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan %s: %w", s.LogFile, err)
	}
	return nil
}
