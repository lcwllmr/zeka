package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestResolveTarget(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "zeka-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ws := &WatchServer{
		watchDir: tmpDir,
	}

	// 1. empty / index.md not existing -> empty target
	f, topic := ws.resolveTarget("/")
	if f != "" || topic != "" {
		t.Errorf("expected empty targets, got %q and %q", f, topic)
	}

	// 2. index.md exists -> still rejected because it is not Zettelkasten
	idxPath := filepath.Join(tmpDir, "index.md")
	if err := os.WriteFile(idxPath, []byte("index content"), 0644); err != nil {
		t.Fatalf("failed to write index: %v", err)
	}
	f, topic = ws.resolveTarget("/")
	if f != "" || topic != "" {
		t.Errorf("expected empty targets for index.md, got %q and %q", f, topic)
	}

	// 3. clean name resolution (invalid vs valid)
	f, topic = ws.resolveTarget("/about.html")
	if f != "" || topic != "" {
		t.Errorf("expected rejected target for non-hex name, got %q and %q", f, topic)
	}

	f, topic = ws.resolveTarget("/0123456789abcdef.html")
	if f != "0123456789abcdef.md" || topic != "0123456789abcdef.md" {
		t.Errorf("expected 0123456789abcdef.md, got %q and %q", f, topic)
	}

	f, topic = ws.resolveTarget("/sub/0123456789abcdef.html")
	if f != "" || topic != "" {
		t.Errorf("expected blocked traversal, got %q and %q", f, topic)
	}
}

func TestServeHTTP(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "zeka-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ws := &WatchServer{
		watchDir: tmpDir,
		clients:  make(map[*Client]bool),
	}

	// 1. Test empty directory serves a blank page
	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	ws.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 for empty index, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "<title></title>") || strings.Contains(rr.Body.String(), "No Notes Found") {
		t.Errorf("expected body to be blank template, got %s", rr.Body.String())
	}

	// 2. Create note A
	noteAPath := filepath.Join(tmpDir, "1111111111111111.md")
	if err := os.WriteFile(noteAPath, []byte("---\ntitle: Note A\n---\nContent A"), 0644); err != nil {
		t.Fatalf("failed to write note A: %v", err)
	}

	// Create note B
	noteBPath := filepath.Join(tmpDir, "2222222222222222.md")
	if err := os.WriteFile(noteBPath, []byte("---\ntitle: Note B\n---\nContent B"), 0644); err != nil {
		t.Fatalf("failed to write note B: %v", err)
	}

	// Explicitly set ModTimes: Note B modified after Note A
	now := time.Now()
	_ = os.Chtimes(noteAPath, now.Add(-10*time.Minute), now.Add(-10*time.Minute))
	_ = os.Chtimes(noteBPath, now, now)

	// Test GET / serves the most recently modified note B
	req = httptest.NewRequest("GET", "/", nil)
	rr = httptest.NewRecorder()
	ws.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "Note B") {
		t.Errorf("expected page to show Note B, got %s", rr.Body.String())
	}

	// Test GET /1111111111111111.html serves Note A
	req = httptest.NewRequest("GET", "/1111111111111111.html", nil)
	rr = httptest.NewRecorder()
	ws.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "Note A") {
		t.Errorf("expected page to show Note A, got %s", rr.Body.String())
	}

	// Test GET /nonhex.html returns 404
	req = httptest.NewRequest("GET", "/nonhex.html", nil)
	rr = httptest.NewRecorder()
	ws.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected status 404 for non-hex request, got %d", rr.Code)
	}
}

func TestSSE(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "zeka-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ws := &WatchServer{
		watchDir: tmpDir,
		clients:  make(map[*Client]bool),
	}

	server := httptest.NewServer(ws)
	defer server.Close()

	// Connect to SSE for root "/"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", server.URL+"/events?file=/", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	respChan := make(chan *http.Response, 1)
	errChan := make(chan error, 1)

	go func() {
		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			errChan <- err
			return
		}
		respChan <- resp
	}()

	// Wait a moment for connection
	time.Sleep(100 * time.Millisecond)

	// Verify client is registered under root "" topic
	ws.mu.Lock()
	var registeredClient *Client
	for c := range ws.clients {
		if c.file == "" {
			registeredClient = c
			break
		}
	}
	ws.mu.Unlock()

	if registeredClient == nil {
		t.Fatal("expected client to be registered under root '' topic")
	}

	// Broadcast an update to the root topic
	testHTML := "<div>root snap update</div>"
	ws.broadcast("", testHTML)

	// Cancel context to close SSE connection
	cancel()

	// Wait for goroutine to exit
	select {
	case <-respChan:
		// success
	case err := <-errChan:
		t.Logf("connection finished with error (expected due to cancel): %v", err)
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for connection to close")
	}
}

func TestRootSnappingAddDelete(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "zeka-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	ws := &WatchServer{
		watchDir: tmpDir,
		clients:  make(map[*Client]bool),
	}

	// Register a client on root
	rootClient := &Client{
		file: "",
		ch:   make(chan string, 10),
	}
	ws.register(rootClient)
	defer ws.unregister(rootClient)

	// 1. Add note A
	noteAPath := filepath.Join(tmpDir, "1111111111111111.md")
	if err := os.WriteFile(noteAPath, []byte("---\ntitle: Note A\n---\nContent A"), 0644); err != nil {
		t.Fatalf("failed to write Note A: %v", err)
	}
	ws.processChanges(map[string]bool{noteAPath: true})

	// Root client should receive Note A HTML
	select {
	case html := <-rootClient.ch:
		if !strings.Contains(html, "Note A") {
			t.Errorf("expected root to receive Note A, got %s", html)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timed out waiting for Note A on root")
	}

	// 2. Add note B (more recent)
	noteBPath := filepath.Join(tmpDir, "2222222222222222.md")
	if err := os.WriteFile(noteBPath, []byte("---\ntitle: Note B\n---\nContent B"), 0644); err != nil {
		t.Fatalf("failed to write Note B: %v", err)
	}
	// Explicitly set ModTimes: Note B modified after Note A
	now := time.Now()
	_ = os.Chtimes(noteAPath, now.Add(-10*time.Minute), now.Add(-10*time.Minute))
	_ = os.Chtimes(noteBPath, now, now)

	ws.processChanges(map[string]bool{noteBPath: true})

	// Root client should snap to Note B
	select {
	case html := <-rootClient.ch:
		if !strings.Contains(html, "Note B") {
			t.Errorf("expected root to receive Note B, got %s", html)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timed out waiting for Note B on root")
	}

	// 3. Delete note B -> root client should snap back to Note A (since it's the only one left)
	if err := os.Remove(noteBPath); err != nil {
		t.Fatalf("failed to delete Note B: %v", err)
	}
	ws.processChanges(map[string]bool{noteBPath: true})

	select {
	case html := <-rootClient.ch:
		if !strings.Contains(html, "Note A") {
			t.Errorf("expected root to snap back to Note A, got %s", html)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timed out waiting for Note A snapback on root")
	}

	// 4. Delete note A -> root client should get a blank page
	if err := os.Remove(noteAPath); err != nil {
		t.Fatalf("failed to delete Note A: %v", err)
	}
	ws.processChanges(map[string]bool{noteAPath: true})

	select {
	case html := <-rootClient.ch:
		if !strings.Contains(html, "<title></title>") || strings.Contains(html, "No Notes Found") {
			t.Errorf("expected root to receive blank template page, got %s", html)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timed out waiting for blank page")
	}
}
