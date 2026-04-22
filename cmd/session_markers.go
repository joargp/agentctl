package cmd

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/joargp/agentctl/internal/session"
)

func markSessionCancelled(s *session.Session) error {
	if s == nil || s.CancelFile == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.CancelFile), 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.CancelFile, []byte("cancelled\n"), 0o644)
}

func sessionCancelled(s *session.Session) bool {
	if s == nil || s.CancelFile == "" {
		return false
	}
	_, err := os.Stat(s.CancelFile)
	return err == nil
}

func clearSessionCancelled(s *session.Session) error {
	if s == nil || s.CancelFile == "" {
		return nil
	}
	if err := os.Remove(s.CancelFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
