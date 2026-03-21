package cmd

import (
	"os"
	"testing"
	"time"
)

func TestParseDurationStandard(t *testing.T) {
	d, err := parseDuration("1h")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != time.Hour {
		t.Fatalf("expected 1h, got %v", d)
	}
}

func TestParseDurationMinutes(t *testing.T) {
	d, err := parseDuration("30m")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != 30*time.Minute {
		t.Fatalf("expected 30m, got %v", d)
	}
}

func TestParseDurationDays(t *testing.T) {
	d, err := parseDuration("2d")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != 48*time.Hour {
		t.Fatalf("expected 48h, got %v", d)
	}
}

func TestParseDurationFractionalDays(t *testing.T) {
	d, err := parseDuration("0.5d")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != 12*time.Hour {
		t.Fatalf("expected 12h, got %v", d)
	}
}

func TestParseDurationInvalid(t *testing.T) {
	_, err := parseDuration("abc")
	if err == nil {
		t.Fatal("expected error for invalid duration")
	}
}

func TestParseDurationInvalidDayWithTrailingGarbage(t *testing.T) {
	_, err := parseDuration("2xd")
	if err == nil {
		t.Fatal("expected error for invalid day duration")
	}
}

func TestParseDurationInvalidMixedUnitsEndingInDay(t *testing.T) {
	_, err := parseDuration("1h2d")
	if err == nil {
		t.Fatal("expected error for malformed mixed duration")
	}
}

func TestCountTurnsSingleTurn(t *testing.T) {
	f, err := os.CreateTemp("", "agentctl-test-*.log")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	f.WriteString(`{"type":"turn_start"}
{"type":"turn_end"}
`)
	f.Close()

	turns := countTurns(f.Name())
	if turns != 1 {
		t.Fatalf("expected 1 turn, got %d", turns)
	}
}

func TestReadTailSmallFile(t *testing.T) {
	// readTail on a nonexistent file should return nil
	data := readTail("/nonexistent/file", 1024)
	if data != nil {
		t.Fatalf("expected nil for nonexistent file, got %d bytes", len(data))
	}
}

func TestExtractTotalCostNoFile(t *testing.T) {
	cost := extractTotalCost("/nonexistent/file")
	if cost != 0 {
		t.Fatalf("expected 0 cost for nonexistent file, got %f", cost)
	}
}

func TestCountTurnsNoFile(t *testing.T) {
	turns := countTurns("/nonexistent/file")
	if turns != 0 {
		t.Fatalf("expected 0 turns for nonexistent file, got %d", turns)
	}
}

func TestCountTurnsFromLog(t *testing.T) {
	f, err := os.CreateTemp("", "agentctl-test-*.log")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	f.WriteString(`{"type":"turn_start"}
{"type":"turn_end"}
{"type":"turn_start"}
{"type":"turn_end"}
{"type":"turn_start"}
{"type":"turn_end"}
`)
	f.Close()

	turns := countTurns(f.Name())
	if turns != 3 {
		t.Fatalf("expected 3 turns, got %d", turns)
	}
}

func TestExtractTotalCostFromLog(t *testing.T) {
	// Create a temp file with turn_end events
	f, err := os.CreateTemp("", "agentctl-test-*.log")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())

	f.WriteString(`{"type":"turn_end","message":{"usage":{"totalTokens":100,"cost":{"total":0.01}}}}
{"type":"turn_end","message":{"usage":{"totalTokens":200,"cost":{"total":0.02}}}}
`)
	f.Close()

	cost := extractTotalCost(f.Name())
	if cost < 0.029 || cost > 0.031 {
		t.Fatalf("expected ~0.03 cost, got %f", cost)
	}
}
