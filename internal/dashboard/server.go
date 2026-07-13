package dashboard

import (
	"bufio"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/joargp/agentctl/internal/session"
	"github.com/joargp/agentctl/internal/tmux"
	"github.com/nxadm/tail"
)

//go:embed static/*
var staticFiles embed.FS

var (
	cacheMutex             sync.RWMutex
	sessionCache           = make(map[string]APISession)
	loadedSessions         = make(map[string]*session.Session)
	loadedMetadataModTimes = make(map[string]time.Time)
	liveSessionCache       = make(map[string]liveAPISession)

	// Kept as a variable so dashboard behavior can be tested without tmux.
	listTmuxSessionsForDashboard = tmux.ListSessions

	// These hooks keep filesystem-index behavior deterministic in tests.
	sessionDataDirForDashboard    = session.DataDir
	statSessionIndexFile          = os.Stat
	dashboardNow                  = time.Now
	dashboardIndexTTL             = 5 * time.Second
	buildSessionIndexForDashboard = buildSessionIndex
	// Test hook invoked after an index build has installed (or failed).
	onSessionIndexRefreshForDashboard = func() {}
)

// sessionIndexCache holds an immutable, sorted directory index. Metadata is
// intentionally loaded separately, and only for pages requested by a client.
// A stale index is usable while one goroutine refreshes it in the background.
type sessionIndexCache struct {
	files       []sessionFile
	builtAt     time.Time
	initialized bool
	refreshing  bool
	ready       chan struct{}
	generation  uint64
}

var (
	indexCacheMutex sync.Mutex
	indexCache      sessionIndexCache
)

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

type liveAPISession struct {
	api        APISession
	logModTime time.Time
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

func deriveLastActivity(logFile string, running bool) (string, string) {
	// A compact tail is enough to identify the current activity while keeping
	// the first page responsive when several agents have multi-megabyte logs.
	data := readTail(logFile, 128*1024)
	if len(data) == 0 {
		if running {
			return "starting", ""
		}
		return "unknown", ""
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)

	type activeTool struct {
		name   string
		detail string
	}

	activeTools := make(map[string]activeTool)
	turnCount := 0
	lastState := "unknown"
	lastDetail := ""

	for scanner.Scan() {
		line := scanner.Text()
		activity := session.ParseActivityLine(line, &turnCount)
		if activity.State != "" {
			lastState = activity.State
			lastDetail = activity.Detail
		}

		var event map[string]interface{}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}

		eventType, _ := event["type"].(string)
		toolCallID, _ := event["toolCallId"].(string)
		if toolCallID == "" {
			continue
		}

		switch eventType {
		case "tool_execution_start":
			toolName, _ := event["toolName"].(string)
			detail := ""
			if args, ok := event["args"].(map[string]interface{}); ok {
				if cmd, ok := args["command"].(string); ok {
					detail = cmd
				} else if path, ok := args["path"].(string); ok {
					detail = path
				}
			}
			activeTools[toolCallID] = activeTool{name: toolName, detail: detail}
		case "tool_execution_end":
			delete(activeTools, toolCallID)
		}
	}

	if running && len(activeTools) > 0 {
		for _, tool := range activeTools {
			return "running " + tool.name, tool.detail
		}
	}

	return lastState, lastDetail
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
	// StatsCached explicitly records a completed log scan, including valid
	// zero-turn sessions. Turns > 0 remains the compatibility signal for
	// sessions created before StatsCached was added. For live sessions this
	// avoids rescanning a growing multi-megabyte log when a recent cached value
	// is already available.
	if s.StatsCached || s.Turns > 0 {
		return logStats{Turns: s.Turns, TotalCost: s.TotalCost}
	}
	if running {
		// The selected session's log stream remains the live source of truth.
		// Avoid a full historical scan merely to populate sidebar aggregates.
		return logStats{}
	}
	return scanLogStats(s.LogFile)
}

func isRunning(s *session.Session, runningSessions map[string]bool) bool {
	return runningSessions[s.TmuxSession]
}

func makeAPISession(s *session.Session, running bool) APISession {
	status := "done"
	if running {
		status = "running"
	}

	stats := getSessionLogStats(s, running)
	apiSess := APISession{
		ID:        s.ID,
		Name:      s.Name,
		Model:     s.Model,
		Task:      s.Task,
		Cwd:       s.Cwd,
		StartedAt: s.StartedAt,
		Status:    status,
		Turns:     stats.Turns,
		TotalCost: stats.TotalCost,
	}

	// The dashboard only displays activity for live sessions. Avoid reading and
	// parsing completed session log tails while indexing history.
	if running {
		apiSess.LastState, apiSess.LastDetail = deriveLastActivity(s.LogFile, true)
	}

	return apiSess
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

func browserCommand(goos, url string) (string, []string) {
	switch goos {
	case "darwin":
		return "open", []string{url}
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", url}
	default:
		return "xdg-open", []string{url}
	}
}

func openBrowser(url string) error {
	name, args := browserCommand(runtime.GOOS, url)
	return exec.Command(name, args...).Run()
}

func Run(port int, autoOpen bool) error {
	return runDashboard(port, autoOpen, openBrowser, http.Serve)
}

func runDashboard(port int, autoOpen bool, opener func(string) error, serve func(net.Listener, http.Handler) error) error {
	mux := http.NewServeMux()

	// 1. Root and Static Files Routes
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
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

	mux.HandleFunc("GET /app.js", func(w http.ResponseWriter, r *http.Request) {
		data, err := staticFiles.ReadFile("static/app.js")
		if err != nil {
			http.Error(w, "app.js not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Write(data)
	})

	// 2. API Routes
	mux.HandleFunc("GET /api/sessions", handleSessions)
	mux.HandleFunc("GET /api/sessions/{id}/logs", handleSessionLogs)
	mux.HandleFunc("POST /api/sessions/{id}/kill", handleSessionKill)

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return err
	}
	actualPort := port
	if tcpAddr, ok := listener.Addr().(*net.TCPAddr); ok {
		actualPort = tcpAddr.Port
	}
	url := fmt.Sprintf("http://localhost:%d", actualPort)
	fmt.Printf("agentctl dashboard running on %s\n", url)
	if autoOpen {
		go func() {
			if err := opener(url); err != nil {
				log.Printf("could not open dashboard in browser: %v", err)
			}
		}()
	}
	return serve(listener, mux)
}

type sessionFile struct {
	id              string
	metadataModTime time.Time
	logModTime      time.Time
	modTime         time.Time // Effective sort time: the newer of metadata and log.
}

// buildSessionIndex scans names and modification times only; it never reads
// metadata or log contents. Both metadata and logs are statted so a completed
// session remains ordered by its most recent log activity after it exits tmux.
func buildSessionIndex(_ map[string]bool) ([]sessionFile, error) {
	dir, err := sessionDataDirForDashboard()
	if err != nil {
		return nil, err
	}

	sessDir := filepath.Join(dir, "sessions")
	entries, err := os.ReadDir(sessDir)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	filesMap := make(map[string]sessionFile)

	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		info, err := statSessionIndexFile(filepath.Join(sessDir, e.Name()))
		if err == nil {
			filesMap[id] = sessionFile{
				id:              id,
				metadataModTime: info.ModTime(),
				modTime:         info.ModTime(),
			}
		}
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
		info, err := statSessionIndexFile(filepath.Join(logDir, e.Name()))
		if err == nil {
			sf := filesMap[id]
			sf.id = id
			sf.logModTime = info.ModTime()
			if sf.modTime.IsZero() || info.ModTime().After(sf.modTime) {
				sf.modTime = info.ModTime()
			}
			filesMap[id] = sf
		}
	}

	var list []sessionFile
	for _, sf := range filesMap {
		list = append(list, sf)
	}

	sort.Slice(list, func(i, j int) bool {
		if list[i].modTime.Equal(list[j].modTime) {
			return list[i].id < list[j].id
		}
		return list[i].modTime.After(list[j].modTime)
	})

	return list, nil
}

func cleanDeletedCachedSessions(files []sessionFile) {
	activeIDs := make(map[string]struct{}, len(files))
	for _, sf := range files {
		activeIDs[sf.id] = struct{}{}
	}

	cacheMutex.Lock()
	defer cacheMutex.Unlock()
	for id := range loadedSessions {
		if _, exists := activeIDs[id]; !exists {
			delete(loadedSessions, id)
			delete(loadedMetadataModTimes, id)
			delete(sessionCache, id)
			delete(liveSessionCache, id)
		}
	}
}

func finishSessionIndexRefresh(generation uint64, ready chan struct{}, files []sessionFile, err error) {
	indexCacheMutex.Lock()
	shouldClean := false
	if generation == indexCache.generation {
		if err == nil {
			indexCache.files = files
			indexCache.builtAt = dashboardNow()
			indexCache.initialized = true
			shouldClean = true
		}
		indexCache.refreshing = false
		indexCache.ready = nil
	}
	indexCacheMutex.Unlock()
	close(ready)

	if shouldClean {
		// Cleanup is done off the request path for background refreshes.
		cleanDeletedCachedSessions(files)
	}
	onSessionIndexRefreshForDashboard()
}

// getCachedSessionIndex returns a fresh index synchronously on first use. On
// later stale reads it returns the old immutable index immediately and starts
// at most one asynchronous rebuild.
func getCachedSessionIndex(runningSessions map[string]bool) ([]sessionFile, error) {
	for {
		indexCacheMutex.Lock()
		now := dashboardNow()
		if indexCache.initialized {
			files := indexCache.files
			if now.Sub(indexCache.builtAt) <= dashboardIndexTTL {
				indexCacheMutex.Unlock()
				return files, nil
			}
			if !indexCache.refreshing {
				indexCache.refreshing = true
				indexCache.generation++
				generation := indexCache.generation
				ready := make(chan struct{})
				indexCache.ready = ready
				indexCacheMutex.Unlock()
				go func() {
					files, err := buildSessionIndexForDashboard(runningSessions)
					finishSessionIndexRefresh(generation, ready, files, err)
				}()
				return files, nil
			}
			indexCacheMutex.Unlock()
			return files, nil
		}

		if indexCache.refreshing {
			ready := indexCache.ready
			indexCacheMutex.Unlock()
			<-ready
			continue
		}

		indexCache.refreshing = true
		indexCache.generation++
		generation := indexCache.generation
		ready := make(chan struct{})
		indexCache.ready = ready
		indexCacheMutex.Unlock()

		files, err := buildSessionIndexForDashboard(runningSessions)
		finishSessionIndexRefresh(generation, ready, files, err)
		return files, err
	}
}

// runningCountForIndex deliberately counts only agentctl tmux names that have
// a corresponding session index entry. This keeps the sidebar aggregate and
// the returned population on the same snapshot, excluding unrelated tmux
// sessions and stale tmux names.
func runningCountForIndex(files []sessionFile, runningSessions map[string]bool) int {
	indexed := make(map[string]struct{}, len(files))
	for _, sf := range files {
		indexed[sf.id] = struct{}{}
	}
	count := 0
	for tmuxName := range runningSessions {
		if id, ok := strings.CutPrefix(tmuxName, "agentctl-"); ok {
			if _, exists := indexed[id]; exists {
				count++
			}
		}
	}
	return count
}

func handleSessions(w http.ResponseWriter, r *http.Request) {
	offset, limit, err := sessionPageFromRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Take one tmux snapshot per request for both row status and the aggregate;
	// never make a per-row tmux call.
	runningSessions := listTmuxSessionsForDashboard()
	sortedFiles, err := getCachedSessionIndex(runningSessions)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	total := len(sortedFiles)
	// If rows were deleted while a client was on the final page, return the
	// new final page rather than an empty, stranded page. The returned offset
	// tells the client exactly where that fresh snapshot begins.
	if total == 0 {
		offset = 0
	} else if offset >= total {
		offset = ((total - 1) / limit) * limit
	}
	end := offset + limit
	if end > total {
		end = total
	}
	page := sortedFiles[offset:end]

	apiSessions := make([]APISession, 0, len(page))
	for _, sf := range page {
		cacheMutex.RLock()
		s, loaded := loadedSessions[sf.id]
		loadedMetadataModTime := loadedMetadataModTimes[sf.id]
		cacheMutex.RUnlock()

		// A metadata rewrite can change visible fields, cached completion stats,
		// and even the tmux/log paths. Reload it before creating this page row.
		if !loaded || !loadedMetadataModTime.Equal(sf.metadataModTime) {
			loadedSession, loadErr := session.Load(sf.id)
			if loadErr != nil {
				continue
			}
			s = loadedSession
			cacheMutex.Lock()
			// Replacing a metadata version invalidates every derived row. The
			// cache is populated only for requested pages.
			loadedSessions[sf.id] = s
			loadedMetadataModTimes[sf.id] = sf.metadataModTime
			delete(sessionCache, sf.id)
			delete(liveSessionCache, sf.id)
			cacheMutex.Unlock()
		}

		running := isRunning(s, runningSessions)
		if running {
			cacheMutex.RLock()
			cachedLive, hasCachedLive := liveSessionCache[sf.id]
			cacheMutex.RUnlock()
			if hasCachedLive && cachedLive.logModTime.Equal(sf.logModTime) {
				apiSessions = append(apiSessions, cachedLive.api)
				continue
			}

			apiSess := makeAPISession(s, true)
			cacheMutex.Lock()
			liveSessionCache[s.ID] = liveAPISession{api: apiSess, logModTime: sf.logModTime}
			delete(sessionCache, s.ID)
			cacheMutex.Unlock()
			apiSessions = append(apiSessions, apiSess)
			continue
		}

		cacheMutex.RLock()
		cachedAPI, hasCachedAPI := sessionCache[sf.id]
		cacheMutex.RUnlock()
		if hasCachedAPI && cachedAPI.Status == "done" {
			apiSessions = append(apiSessions, cachedAPI)
			continue
		}

		apiSess := makeAPISession(s, false)
		cacheMutex.Lock()
		sessionCache[s.ID] = apiSess
		delete(liveSessionCache, s.ID)
		cacheMutex.Unlock()
		apiSessions = append(apiSessions, apiSess)
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Total-Count", strconv.Itoa(total))
	w.Header().Set("X-Running-Count", strconv.Itoa(runningCountForIndex(sortedFiles, runningSessions)))
	w.Header().Set("X-Has-More", strconv.FormatBool(end < total))
	w.Header().Set("X-Offset", strconv.Itoa(offset))
	w.Header().Set("X-Limit", strconv.Itoa(limit))
	json.NewEncoder(w).Encode(apiSessions)
}

const (
	defaultSessionPageLimit = 100
	maxSessionPageLimit     = defaultSessionPageLimit
)

func sessionPageFromRequest(r *http.Request) (offset, limit int, err error) {
	offset = 0
	limit = defaultSessionPageLimit
	query := r.URL.Query()
	if value, present := query["offset"]; present {
		if len(value) != 1 || value[0] == "" {
			return 0, 0, fmt.Errorf("invalid offset")
		}
		offset, err = strconv.Atoi(value[0])
		if err != nil || offset < 0 {
			return 0, 0, fmt.Errorf("invalid offset")
		}
	}
	if value, present := query["limit"]; present {
		if len(value) != 1 || value[0] == "" {
			return 0, 0, fmt.Errorf("invalid limit")
		}
		limit, err = strconv.Atoi(value[0])
		if err != nil || limit < 1 {
			return 0, 0, fmt.Errorf("invalid limit")
		}
	}
	if limit > maxSessionPageLimit {
		limit = maxSessionPageLimit
	}
	return offset, limit, nil
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
