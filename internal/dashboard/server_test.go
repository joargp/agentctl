package dashboard

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	neturl "net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/joargp/agentctl/internal/session"
)

func resetDashboardState(t *testing.T) {
	t.Helper()

	previousListTmuxSessions := listTmuxSessionsForDashboard
	previousSessionDataDir := sessionDataDirForDashboard
	previousStat := statSessionIndexFile
	previousNow := dashboardNow
	previousTTL := dashboardIndexTTL
	previousIndexBuilder := buildSessionIndexForDashboard
	previousIndexRefreshHook := onSessionIndexRefreshForDashboard
	cacheMutex.Lock()
	previousSessionCache := sessionCache
	previousLoadedSessions := loadedSessions
	previousLoadedMetadataModTimes := loadedMetadataModTimes
	previousLiveSessionCache := liveSessionCache
	sessionCache = make(map[string]APISession)
	loadedSessions = make(map[string]*session.Session)
	loadedMetadataModTimes = make(map[string]time.Time)
	liveSessionCache = make(map[string]liveAPISession)
	cacheMutex.Unlock()
	indexCacheMutex.Lock()
	previousIndexCache := indexCache
	indexCache = sessionIndexCache{generation: indexCache.generation + 1}
	indexCacheMutex.Unlock()

	t.Cleanup(func() {
		listTmuxSessionsForDashboard = previousListTmuxSessions
		sessionDataDirForDashboard = previousSessionDataDir
		statSessionIndexFile = previousStat
		dashboardNow = previousNow
		dashboardIndexTTL = previousTTL
		buildSessionIndexForDashboard = previousIndexBuilder
		onSessionIndexRefreshForDashboard = previousIndexRefreshHook
		cacheMutex.Lock()
		sessionCache = previousSessionCache
		loadedSessions = previousLoadedSessions
		loadedMetadataModTimes = previousLoadedMetadataModTimes
		liveSessionCache = previousLiveSessionCache
		cacheMutex.Unlock()
		indexCacheMutex.Lock()
		indexCache = previousIndexCache
		indexCacheMutex.Unlock()
	})
}

func saveDashboardSession(t *testing.T, s *session.Session, modTime time.Time) {
	t.Helper()
	if err := session.Save(s); err != nil {
		t.Fatalf("save session %s: %v", s.ID, err)
	}

	dir, err := session.DataDir()
	if err != nil {
		t.Fatalf("get data dir: %v", err)
	}
	metadataFile := filepath.Join(dir, "sessions", s.ID+".json")
	if err := os.Chtimes(metadataFile, modTime, modTime); err != nil {
		t.Fatalf("set session %s modification time: %v", s.ID, err)
	}
}

func TestGetSessionLogStatsUsesExplicitZeroCache(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "session.log")
	if err := os.WriteFile(logFile, []byte(`{"type":"turn_end","message":{"usage":{"cost":{"total":0.01}}}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stats := getSessionLogStats(&session.Session{
		LogFile:     logFile,
		StatsCached: true,
	}, false)

	if stats.Turns != 0 || stats.TotalCost != 0 {
		t.Fatalf("expected explicit zero-value cache without a rescan, got %#v", stats)
	}
}

func TestBrowserCommand(t *testing.T) {
	url := "http://localhost:8080"
	tests := []struct {
		goos     string
		wantName string
		wantArgs []string
	}{
		{goos: "darwin", wantName: "open", wantArgs: []string{url}},
		{goos: "windows", wantName: "rundll32", wantArgs: []string{"url.dll,FileProtocolHandler", url}},
		{goos: "linux", wantName: "xdg-open", wantArgs: []string{url}},
	}

	for _, tt := range tests {
		t.Run(tt.goos, func(t *testing.T) {
			name, args := browserCommand(tt.goos, url)
			if name != tt.wantName {
				t.Fatalf("command = %q, want %q", name, tt.wantName)
			}
			if len(args) != len(tt.wantArgs) {
				t.Fatalf("args = %#v, want %#v", args, tt.wantArgs)
			}
			for i := range args {
				if args[i] != tt.wantArgs[i] {
					t.Fatalf("args = %#v, want %#v", args, tt.wantArgs)
				}
			}
		})
	}
}

func TestRunDashboardOpensActualBoundURL(t *testing.T) {
	serveDone := errors.New("serve done")
	opened := make(chan string, 1)
	err := runDashboard(0, true, func(url string) error {
		opened <- url
		return nil
	}, func(listener net.Listener, _ http.Handler) error {
		if err := listener.Close(); err != nil {
			t.Fatalf("close listener: %v", err)
		}
		return serveDone
	})
	if !errors.Is(err, serveDone) {
		t.Fatalf("runDashboard error = %v, want %v", err, serveDone)
	}

	select {
	case openedURL := <-opened:
		parsed, err := neturl.Parse(openedURL)
		if err != nil {
			t.Fatalf("parse opened URL %q: %v", openedURL, err)
		}
		_, port, err := net.SplitHostPort(parsed.Host)
		if err != nil {
			t.Fatalf("opened URL host = %q: %v", parsed.Host, err)
		}
		if port == "0" || port == "" {
			t.Fatalf("opened URL did not use actual bound port: %q", openedURL)
		}
	case <-time.After(time.Second):
		t.Fatal("dashboard did not invoke browser opener")
	}
}

func TestRunDashboardDoesNotOpenWhenDisabled(t *testing.T) {
	serveDone := errors.New("serve done")
	var opened atomic.Bool
	err := runDashboard(0, false, func(string) error {
		opened.Store(true)
		return nil
	}, func(listener net.Listener, _ http.Handler) error {
		_ = listener.Close()
		return serveDone
	})
	if !errors.Is(err, serveDone) {
		t.Fatalf("runDashboard error = %v, want %v", err, serveDone)
	}
	if opened.Load() {
		t.Fatal("dashboard invoked browser opener when auto-open was disabled")
	}
}

func TestRunDashboardBindFailureDoesNotOpen(t *testing.T) {
	occupied, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	defer occupied.Close()
	port := occupied.Addr().(*net.TCPAddr).Port

	var opened atomic.Bool
	err = runDashboard(port, true, func(string) error {
		opened.Store(true)
		return nil
	}, func(net.Listener, http.Handler) error {
		t.Fatal("serve called after bind failure")
		return nil
	})
	if err == nil {
		t.Fatal("expected bind failure")
	}
	if opened.Load() {
		t.Fatal("dashboard invoked browser opener after bind failure")
	}
}

func TestMakeAPISessionSkipsCompletedActivityParsing(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "completed.log")
	if err := os.WriteFile(logFile, []byte(`{"type":"text_delta","delta":"completed work"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	apiSession := makeAPISession(&session.Session{
		ID:          "completed",
		Model:       "gpt-test",
		LogFile:     logFile,
		StatsCached: true,
	}, false)

	if apiSession.Status != "done" {
		t.Fatalf("expected done status, got %q", apiSession.Status)
	}
	if apiSession.LastState != "" || apiSession.LastDetail != "" {
		t.Fatalf("completed session unexpectedly parsed activity: %#v", apiSession)
	}
}

func TestHandleSessionsUsesOneRunningSnapshotAndPreservesStatus(t *testing.T) {
	resetDashboardState(t)
	t.Setenv("HOME", t.TempDir())

	logDir := filepath.Join(t.TempDir(), "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	runningLog := filepath.Join(logDir, "running.log")
	completedLog := filepath.Join(logDir, "completed.log")
	if err := os.WriteFile(runningLog, []byte(`{"type":"text_delta","delta":"working"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(completedLog, []byte(`{"type":"text_delta","delta":"already done"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	base := time.Now().Add(-time.Hour)
	saveDashboardSession(t, &session.Session{
		ID:          "running",
		Model:       "gpt-test",
		Task:        "running task",
		Cwd:         "/tmp/running",
		TmuxSession: "agentctl-running",
		LogFile:     runningLog,
		StartedAt:   base,
		StatsCached: true,
	}, base.Add(2*time.Second))
	saveDashboardSession(t, &session.Session{
		ID:          "completed",
		Model:       "gpt-test",
		Task:        "completed task",
		Cwd:         "/tmp/completed",
		TmuxSession: "agentctl-completed",
		LogFile:     completedLog,
		StartedAt:   base,
		StatsCached: true,
	}, base.Add(time.Second))

	snapshotCalls := 0
	listTmuxSessionsForDashboard = func() map[string]bool {
		snapshotCalls++
		return map[string]bool{"agentctl-running": true}
	}

	response := httptest.NewRecorder()
	handleSessions(response, httptest.NewRequest(http.MethodGet, "/api/sessions", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
	}
	if snapshotCalls != 1 {
		t.Fatalf("expected one tmux snapshot, got %d", snapshotCalls)
	}

	var sessions []APISession
	if err := json.Unmarshal(response.Body.Bytes(), &sessions); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected two sessions, got %d", len(sessions))
	}
	if sessions[0].ID != "running" || sessions[0].Status != "running" {
		t.Fatalf("expected first session to be running, got %#v", sessions[0])
	}
	if sessions[0].LastState != "writing" || sessions[0].LastDetail != "working" {
		t.Fatalf("expected running activity from its log, got %#v", sessions[0])
	}
	if sessions[1].ID != "completed" || sessions[1].Status != "done" {
		t.Fatalf("expected second session to be completed, got %#v", sessions[1])
	}
	if sessions[1].LastState != "" || sessions[1].LastDetail != "" {
		t.Fatalf("completed activity should not be parsed, got %#v", sessions[1])
	}
}

func TestHandleSessionsKeepsNewestHundredInOrder(t *testing.T) {
	resetDashboardState(t)
	t.Setenv("HOME", t.TempDir())

	base := time.Now().Add(-2 * time.Hour)
	for i := 0; i < 101; i++ {
		id := fmt.Sprintf("session-%03d", i)
		saveDashboardSession(t, &session.Session{
			ID:          id,
			Model:       "gpt-test",
			Task:        id,
			Cwd:         "/tmp",
			TmuxSession: "agentctl-" + id,
			StartedAt:   base.Add(time.Duration(-i) * time.Minute),
			StatsCached: true,
		}, base.Add(time.Duration(101-i)*time.Second))
	}

	snapshotCalls := 0
	listTmuxSessionsForDashboard = func() map[string]bool {
		snapshotCalls++
		return map[string]bool{}
	}

	response := httptest.NewRecorder()
	handleSessions(response, httptest.NewRequest(http.MethodGet, "/api/sessions", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
	}
	if snapshotCalls != 1 {
		t.Fatalf("expected one tmux snapshot, got %d", snapshotCalls)
	}

	var sessions []APISession
	if err := json.Unmarshal(response.Body.Bytes(), &sessions); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(sessions) != 100 {
		t.Fatalf("expected newest 100 sessions, got %d", len(sessions))
	}
	for i, apiSession := range sessions {
		wantID := fmt.Sprintf("session-%03d", i)
		if apiSession.ID != wantID {
			t.Fatalf("session %d: expected %q, got %q", i, wantID, apiSession.ID)
		}
		if apiSession.Status != "done" {
			t.Fatalf("session %s: expected done, got %q", apiSession.ID, apiSession.Status)
		}
	}
}

func TestHandleSessionsPaginationHeadersAndValidation(t *testing.T) {
	resetDashboardState(t)
	t.Setenv("HOME", t.TempDir())

	base := time.Now().Add(-2 * time.Hour)
	for i := 0; i < 205; i++ {
		id := fmt.Sprintf("session-%03d", i)
		saveDashboardSession(t, &session.Session{
			ID:          id,
			Model:       "gpt-test",
			Task:        id,
			TmuxSession: "agentctl-" + id,
			StartedAt:   base,
			StatsCached: true,
		}, base.Add(time.Duration(205-i)*time.Second))
	}
	listTmuxSessionsForDashboard = func() map[string]bool { return map[string]bool{} }

	response := httptest.NewRecorder()
	handleSessions(response, httptest.NewRequest(http.MethodGet, "/api/sessions?offset=100&limit=999", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("X-Total-Count"); got != "205" {
		t.Fatalf("X-Total-Count = %q, want 205", got)
	}
	if got := response.Header().Get("X-Offset"); got != "100" {
		t.Fatalf("X-Offset = %q, want 100", got)
	}
	if got := response.Header().Get("X-Limit"); got != "100" {
		t.Fatalf("X-Limit = %q, want 100", got)
	}
	if got := response.Header().Get("X-Has-More"); got != "true" {
		t.Fatalf("X-Has-More = %q, want true", got)
	}
	var sessions []APISession
	if err := json.Unmarshal(response.Body.Bytes(), &sessions); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(sessions) != 100 {
		t.Fatalf("expected bounded 100-session page, got %d", len(sessions))
	}
	if sessions[0].ID != "session-100" || sessions[len(sessions)-1].ID != "session-199" {
		t.Fatalf("unexpected page order: first=%q last=%q", sessions[0].ID, sessions[len(sessions)-1].ID)
	}

	cacheMutex.RLock()
	loaded := len(loadedSessions)
	cacheMutex.RUnlock()
	if loaded != 100 {
		t.Fatalf("handler loaded %d sessions, want only requested page (100)", loaded)
	}

	for _, query := range []string{"?offset=-1", "?offset=wat", "?limit=0", "?limit=-1", "?limit=wat"} {
		response := httptest.NewRecorder()
		handleSessions(response, httptest.NewRequest(http.MethodGet, "/api/sessions"+query, nil))
		if response.Code != http.StatusBadRequest {
			t.Errorf("%s: got status %d, want 400", query, response.Code)
		}
	}
}

func TestSessionIndexStaleWhileRefreshStartsOnlyOnce(t *testing.T) {
	resetDashboardState(t)

	now := time.Now()
	dashboardNow = func() time.Time { return now }
	dashboardIndexTTL = time.Second
	var calls atomic.Int32
	refreshStarted := make(chan struct{})
	allowRefresh := make(chan struct{})
	refreshCompleted := make(chan struct{}, 2)
	onSessionIndexRefreshForDashboard = func() { refreshCompleted <- struct{}{} }
	buildSessionIndexForDashboard = func(map[string]bool) ([]sessionFile, error) {
		switch calls.Add(1) {
		case 1:
			return []sessionFile{{id: "old", modTime: now}}, nil
		case 2:
			close(refreshStarted)
			<-allowRefresh
			return []sessionFile{{id: "new", modTime: now}}, nil
		default:
			t.Fatalf("unexpected duplicate index refresh")
			return nil, nil
		}
	}

	files, err := getCachedSessionIndex(map[string]bool{})
	if err != nil || len(files) != 1 || files[0].id != "old" {
		t.Fatalf("initial index = %#v, %v", files, err)
	}
	<-refreshCompleted
	now = now.Add(2 * time.Second)
	files, err = getCachedSessionIndex(map[string]bool{})
	if err != nil || files[0].id != "old" {
		t.Fatalf("stale index was not returned immediately: %#v, %v", files, err)
	}
	<-refreshStarted
	files, err = getCachedSessionIndex(map[string]bool{})
	if err != nil || files[0].id != "old" {
		t.Fatalf("concurrent stale read = %#v, %v", files, err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("index builder called %d times, want initial build plus one refresh", got)
	}
	close(allowRefresh)
	<-refreshCompleted
	files, err = getCachedSessionIndex(map[string]bool{})
	if err != nil || files[0].id != "new" {
		t.Fatalf("refreshed index = %#v, %v", files, err)
	}
}

func TestBuildSessionIndexStatsAllLogsForCorrectCompletedOrdering(t *testing.T) {
	resetDashboardState(t)
	t.Setenv("HOME", t.TempDir())

	dir, err := session.DataDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "sessions"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"completed", "running"} {
		if err := os.WriteFile(filepath.Join(dir, "sessions", id+".json"), []byte(`{"id":"`+id+`"}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"completed", "running", "recovered"} {
		if err := os.WriteFile(filepath.Join(dir, "logs", id+".log"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	statCalls := make(map[string]int)
	statSessionIndexFile = func(path string) (os.FileInfo, error) {
		statCalls[filepath.Base(path)]++
		return os.Stat(path)
	}
	files, err := buildSessionIndex(map[string]bool{"agentctl-running": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 {
		t.Fatalf("expected metadata and recovered sessions, got %#v", files)
	}
	for _, file := range []string{"completed.json", "running.json", "completed.log", "running.log", "recovered.log"} {
		if statCalls[file] != 1 {
			t.Errorf("%s stat count = %d, want 1", file, statCalls[file])
		}
	}
}

func TestBuildSessionIndexUsesCompletedLogMtimeForOrdering(t *testing.T) {
	resetDashboardState(t)
	t.Setenv("HOME", t.TempDir())

	dir, err := session.DataDir()
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().Add(-time.Hour)
	for _, id := range []string{"completed", "metadata-newer"} {
		if err := os.MkdirAll(filepath.Join(dir, "sessions"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "sessions", id+".json"), []byte(`{"id":"`+id+`"}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"completed", "metadata-newer"} {
		if err := os.WriteFile(filepath.Join(dir, "logs", id+".log"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chtimes(filepath.Join(dir, "sessions", "completed.json"), base, base); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(dir, "logs", "completed.log"), base.Add(3*time.Minute), base.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(dir, "sessions", "metadata-newer.json"), base.Add(2*time.Minute), base.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(dir, "logs", "metadata-newer.log"), base, base); err != nil {
		t.Fatal(err)
	}

	files, err := buildSessionIndex(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0].id != "completed" || !files[0].logModTime.Equal(base.Add(3*time.Minute)) {
		t.Fatalf("completed log mtime did not determine ordering: %#v", files)
	}
}

func TestHandleSessionsHeadersAndReloadsChangedMetadata(t *testing.T) {
	resetDashboardState(t)
	t.Setenv("HOME", t.TempDir())

	base := time.Now().Add(-time.Hour)
	s := &session.Session{
		ID: "changed", Name: "before", Model: "gpt-test", Task: "old task", TmuxSession: "agentctl-changed",
		StartedAt: base, StatsCached: true,
	}
	saveDashboardSession(t, s, base)
	listTmuxSessionsForDashboard = func() map[string]bool {
		return map[string]bool{"agentctl-changed": true, "agentctl-missing": true, "unrelated": true}
	}

	response := httptest.NewRecorder()
	handleSessions(response, httptest.NewRequest(http.MethodGet, "/api/sessions", nil))
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	if got := response.Header().Get("X-Running-Count"); got != "1" {
		t.Fatalf("X-Running-Count = %q, want 1", got)
	}

	// Advance the index clock so the stale-while-refresh build observes this
	// metadata rewrite without restarting the dashboard.
	now := time.Now().Add(time.Hour)
	dashboardNow = func() time.Time { return now }
	dashboardIndexTTL = 0
	s.Name = "after"
	s.Task = "new task"
	if err := session.Save(s); err != nil {
		t.Fatal(err)
	}
	dir, err := session.DataDir()
	if err != nil {
		t.Fatal(err)
	}
	metadataFile := filepath.Join(dir, "sessions", s.ID+".json")
	if err := os.Chtimes(metadataFile, now, now); err != nil {
		t.Fatal(err)
	}

	refreshed := make(chan struct{}, 1)
	onSessionIndexRefreshForDashboard = func() { refreshed <- struct{}{} }
	response = httptest.NewRecorder()
	handleSessions(response, httptest.NewRequest(http.MethodGet, "/api/sessions", nil))
	<-refreshed
	dashboardIndexTTL = time.Hour
	response = httptest.NewRecorder()
	handleSessions(response, httptest.NewRequest(http.MethodGet, "/api/sessions", nil))
	var sessions []APISession
	if err := json.Unmarshal(response.Body.Bytes(), &sessions); err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].Name != "after" || sessions[0].Task != "new task" {
		t.Fatalf("metadata update was not reloaded: %#v", sessions)
	}
}

func TestHandleSessionsReturnsLastPageWhenRequestedPageWasDeleted(t *testing.T) {
	resetDashboardState(t)
	t.Setenv("HOME", t.TempDir())

	base := time.Now().Add(-time.Hour)
	for i := 0; i < 201; i++ {
		id := fmt.Sprintf("session-%03d", i)
		saveDashboardSession(t, &session.Session{ID: id, Model: "gpt-test", Task: id, StatsCached: true}, base.Add(time.Duration(201-i)*time.Second))
	}
	listTmuxSessionsForDashboard = func() map[string]bool { return map[string]bool{} }
	response := httptest.NewRecorder()
	handleSessions(response, httptest.NewRequest(http.MethodGet, "/api/sessions?offset=200", nil))
	if got := response.Header().Get("X-Offset"); got != "200" {
		t.Fatalf("initial final page offset = %q, want 200", got)
	}

	dir, err := session.DataDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "sessions", "session-200.json")); err != nil {
		t.Fatal(err)
	}
	indexCacheMutex.Lock()
	indexCache = sessionIndexCache{}
	indexCacheMutex.Unlock()
	response = httptest.NewRecorder()
	handleSessions(response, httptest.NewRequest(http.MethodGet, "/api/sessions?offset=200", nil))
	if got := response.Header().Get("X-Offset"); got != "100" {
		t.Fatalf("deleted final page offset = %q, want 100", got)
	}
	var sessions []APISession
	if err := json.Unmarshal(response.Body.Bytes(), &sessions); err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 100 {
		t.Fatalf("deleted final page returned %d rows, want 100", len(sessions))
	}
}

func BenchmarkMakeAPISessionCompletedWithLargeLog(b *testing.B) {
	logFile := filepath.Join(b.TempDir(), "completed.log")
	line := []byte(`{"type":"text_delta","delta":"completed work"}` + "\n")
	const logSize = 1024 * 1024
	data := bytes.Repeat(line, (logSize+len(line)-1)/len(line))[:logSize]
	if err := os.WriteFile(logFile, data, 0o644); err != nil {
		b.Fatal(err)
	}

	s := &session.Session{
		ID:          "completed",
		Model:       "gpt-test",
		LogFile:     logFile,
		StatsCached: true,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = makeAPISession(s, false)
	}
}
