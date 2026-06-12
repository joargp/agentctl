package cmd

import (
	"reflect"
	"testing"

	"github.com/joargp/agentctl/internal/session"
)

func TestPiArgsWithoutThinking(t *testing.T) {
	args := piArgs(&session.Session{Model: "gpt-5.4"}, "do work")

	expected := []string{"--mode", "json", "--model", "gpt-5.4", "--no-session", "-p", "do work"}
	if !reflect.DeepEqual(args, expected) {
		t.Fatalf("expected %v, got %v", expected, args)
	}
}

func TestPiArgsWithThinking(t *testing.T) {
	args := piArgs(&session.Session{Model: "gpt-5.4", Thinking: "high"}, "do work")

	expected := []string{"--mode", "json", "--model", "gpt-5.4", "--no-session", "--thinking", "high", "-p", "do work"}
	if !reflect.DeepEqual(args, expected) {
		t.Fatalf("expected %v, got %v", expected, args)
	}
}
