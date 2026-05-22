package main

import (
	"bufio"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/joargp/agentctl/internal/session"
	"github.com/joargp/agentctl/internal/tmux"
	"github.com/nxadm/tail"
)

//go:embed static/*
var staticFiles embed.FS

type APISession struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Model      string    `json:"model"`
	Task       string    `json:"task"`
	Cwd        string    `json:"cwd"`
	StartedAt  time.Time `json:"started_at"`
	Status     string    `json:"status"` // "running" or "done"
	Turns      int       `json:"turns"`
	TotalCost  float64   `json:"total_cost"`
	LastState  string    `json:"last_state"`
	LastDetail string    `json:"last_detail"`
}

type logStats struct {
	Turns     int
	TotalCost float64
}

// readTail reads the last n bytes of a file. Returns nil on error.
func readTail(path string, n int64) []byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil
	}

	size := info.Size()
	if size <= n {
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return nil
		}
		data, _ := io.ReadAll(f)
		return data
	}

	buf := make([]byte, n)
	_, err = f.ReadAt(buf, size-n)
	if err != nil && err != io.EOF {
		return nil
	}
	return buf
}

func scanLogStats(logFile string) logStats {
	f, err := os.Open(logFile)
	if err != nil {
		return logStats{}
	}
	defer f.Close()

	stats := logStats{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, `"turn_end"`) {
			continue
		}

		var event map[string]interface{}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		if eventType, _ := event["type"].(string); eventType != "turn_end" {
			continue
		}
		stats.Turns++
		msg, _ := event["message"].(map[string]interface{})
		if msg == nil {
			continue
		}
		usage, _ := msg["usage"].(map[string]interface{})
		if usage == nil {
			continue
		}
		costInfo, _ := usage["cost"].(map[string]interface{})
		if costInfo == nil {
			continue
		}
		if cost, ok := costInfo["total"].(float64); ok {
			stats.TotalCost += cost
		}
	}

	return stats
}

func getSessionLogStats(s *session.Session, running bool) logStats {
	if running {
		return scanLogStats(s.LogFile)
	}
	if s.Turns > 0 {
		return logStats{Turns: s.Turns, TotalCost: s.TotalCost}
	}
	return scanLogStats(s.LogFile)
}

func getAgentctlPath() string {
	if path, err := exec.LookPath("agentctl"); err == nil {
		return path
	}
	if _, err := os.Stat("../agentctl"); err == nil {
		return "../agentctl"
	}
	if _, err := os.Stat("./agentctl"); err == nil {
		return "./agentctl"
	}
	return "agentctl"
}

func main() {
	port := flag.Int("port", 8080, "port to run the server on")
	flag.Parse()

	// 1. Root and Static Files Routes
	http.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		data, err := staticFiles.ReadFile("static/index.html")
		if err != nil {
			http.Error(w, "index.html not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(data)
	})

	http.HandleFunc("GET /app.js", func(w http.ResponseWriter, r *http.Request) {
		data, err := staticFiles.ReadFile("static/app.js")
		if err != nil {
			http.Error(w, "app.js not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Write(data)
	})

	// 2. API Routes
	http.HandleFunc("GET /api/sessions", handleSessions)
	http.HandleFunc("GET /api/sessions/{id}/logs", handleSessionLogs)
	http.HandleFunc("POST /api/sessions/{id}/kill", handleSessionKill)

	addr := fmt.Sprintf(":%d", *port)
	fmt.Printf("agentctl dashboard running on http://localhost:%d\n", *port)
	log.Fatal(http.ListenAndServe(addr, nil))
}

func handleSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := session.List()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].StartedAt.After(sessions[j].StartedAt)
	})

	apiSessions := make([]APISession, 0, len(sessions))
	for _, s := range sessions {
		running := tmux.SessionExists(s.TmuxSession)
		status := "done"
		if running {
			status = "running"
		}

		stats := getSessionLogStats(s, running)
		
		state := "unknown"
		detail := ""
		data := readTail(s.LogFile, 64*1024)
		if len(data) > 0 {
			state, detail = session.ParseLastActivity(data)
		} else if running {
			state = "starting"
		}

		apiSessions = append(apiSessions, APISession{
			ID:         s.ID,
			Name:       s.Name,
			Model:      s.Model,
			Task:       s.Task,
			Cwd:        s.Cwd,
			StartedAt:  s.StartedAt,
			Status:     status,
			Turns:      stats.Turns,
			TotalCost:  stats.TotalCost,
			LastState:  state,
			LastDetail: detail,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(apiSessions)
}

func handleSessionLogs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s, err := session.Load(id)
	if err != nil {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// First stream existing logs
	file, err := os.Open(s.LogFile)
	if err == nil {
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 0, 128*1024), 10*1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.TrimSpace(line) != "" {
				fmt.Fprintf(w, "data: %s\n\n", line)
				flusher.Flush()
			}
		}
		file.Close()
	}

	// Send caught_up event
	fmt.Fprintf(w, "event: caught_up\ndata: {}\n\n")
	flusher.Flush()

	running := tmux.SessionExists(s.TmuxSession)
	if !running {
		fmt.Fprintf(w, "event: end\ndata: {}\n\n")
		flusher.Flush()
		return
	}

	// Start tailing
	t, err := tail.TailFile(s.LogFile, tail.Config{
		Follow: true,
		ReOpen: true,
		Poll:   true,
	})
	if err != nil {
		log.Printf("Error tailing file %s: %v", s.LogFile, err)
		return
	}
	defer t.Cleanup()
	defer t.Stop()

	doneChan := r.Context().Done()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-doneChan:
			return
		case <-ticker.C:
			fmt.Fprintf(w, ": ping\n\n")
			flusher.Flush()
			
			if !tmux.SessionExists(s.TmuxSession) {
				time.Sleep(500 * time.Millisecond)
				for {
					select {
					case line, ok := <-t.Lines:
						if ok && line != nil && strings.TrimSpace(line.Text) != "" {
							fmt.Fprintf(w, "data: %s\n\n", line.Text)
							flusher.Flush()
						} else {
							goto finished
						}
					default:
						goto finished
					}
				}
			finished:
				fmt.Fprintf(w, "event: end\ndata: {}\n\n")
				flusher.Flush()
				return
			}
		case line, ok := <-t.Lines:
			if !ok {
				return
			}
			if line.Err != nil {
				log.Printf("Tail line error: %v", line.Err)
				return
			}
			if strings.TrimSpace(line.Text) != "" {
				fmt.Fprintf(w, "data: %s\n\n", line.Text)
				flusher.Flush()
			}
		}
	}
}

func handleSessionKill(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	log.Printf("Killing session %s", id)

	binPath := getAgentctlPath()
	cmd := exec.Command(binPath, "kill", id)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("Failed to kill session %s: %v, output: %s", id, err, string(output))
		http.Error(w, fmt.Sprintf("Failed to kill session: %s", string(output)), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"success":true}`))
}
