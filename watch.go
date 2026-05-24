package main

import (
	"bytes"
	"fmt"
	"html/template"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/pkg/browser"
)

// Client represents a connected browser client.
type Client struct {
	file string      // file/topic this client is listening to (e.g. "foo.md" or "__dir__")
	ch   chan string // channel to stream the new HTML page content
}

// WatchServer manages active clients and handles directory-based HTTP requests.
type WatchServer struct {
	watchDir       string
	mu             sync.Mutex
	clients        map[*Client]bool
	memFiles       map[string]string
	server         *http.Server
	pendingChanges map[string]string
	debounceTimer  *time.Timer
}

// register adds a client to the server's client list.
func (ws *WatchServer) register(c *Client) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ws.clients[c] = true
}

// unregister removes a client from the server's client list.
func (ws *WatchServer) unregister(c *Client) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	delete(ws.clients, c)
}

// broadcast sends the rendered HTML to all clients watching the specified file/topic.
func (ws *WatchServer) broadcast(file string, html string) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	for c := range ws.clients {
		if c.file == file {
			select {
			case c.ch <- html:
			default:
				// Skip if client is blocked or not reading fast enough
			}
		}
	}
}

// resolveTarget parses a request path to resolve the markdown filename and SSE subscription topic.
func (ws *WatchServer) resolveTarget(path string) (string, string) {
	name := strings.TrimPrefix(path, "/")
	name = strings.TrimSuffix(name, ".html")
	name = strings.TrimSuffix(name, ".md")

	if name == "" {
		indexPage := filepath.Join(ws.watchDir, "index.md")
		if _, err := os.Stat(indexPage); err == nil {
			return "index.md", "index.md"
		}
		return "", ""
	}

	// Clean to prevent directory traversal
	name = filepath.Clean(name)
	if strings.Contains(name, "..") || filepath.IsAbs(name) || strings.ContainsAny(name, `/\`) {
		return "", ""
	}

	filename := name + ".md"
	return filename, filename
}

// mostRecentFile scans the watch directory and returns the filename of the most recently modified markdown file.
func (ws *WatchServer) mostRecentFile() (string, error) {
	files, err := os.ReadDir(ws.watchDir)
	if err != nil {
		return "", err
	}

	var latestFile string
	var latestTime time.Time

	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(strings.ToLower(file.Name()), ".md") {
			continue
		}
		info, err := file.Info()
		if err != nil {
			continue
		}
		if latestFile == "" || info.ModTime().After(latestTime) {
			latestFile = file.Name()
			latestTime = info.ModTime()
		}
	}

	if latestFile == "" {
		return "", os.ErrNotExist
	}
	return latestFile, nil
}

// renderNoFilesPage creates an HTML placeholder page when no notes are found in the watched directory.
func (ws *WatchServer) renderNoFilesPage() string {
	data := TemplateData{
		Title:    "No Notes Found",
		Abstract: "No markdown notes were found in the watched directory.",
		Macros:   template.JS("{}"),
		TOC:      nil,
		Body:     template.HTML("<p>Create a <code>.md</code> file in this directory to start editing.</p>"),
		Preview:  true,
	}
	tmpl, _ := template.New("page").Funcs(template.FuncMap{
		"repeat": func(n int) []struct{} { return nil },
	}).Parse(defaultTemplate)
	var outBuf bytes.Buffer
	_ = tmpl.Execute(&outBuf, data)
	return outBuf.String()
}

// renderDeletedPage creates an HTML page indicating the note has been deleted.
func (ws *WatchServer) renderDeletedPage(filename string) string {
	data := TemplateData{
		Title:    "Note Not Found",
		Abstract: fmt.Sprintf("The note `%s` could not be found or has been deleted.", filename),
		Macros:   template.JS("{}"),
		TOC:      nil,
		Body:     template.HTML("<p><a href=\"/\">Return to home</a></p>"),
		Preview:  true,
	}
	tmpl, _ := template.New("page").Funcs(template.FuncMap{
		"repeat": func(n int) []struct{} { return nil },
	}).Parse(defaultTemplate)
	var outBuf bytes.Buffer
	_ = tmpl.Execute(&outBuf, data)
	return outBuf.String()
}

// ServeHTTP handles requests for previewing and serving SSE.
func (ws *WatchServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/events" {
		ws.serveSSE(w, r)
		return
	}

	if r.URL.Path == "/favicon.ico" {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	targetFile, topic := ws.resolveTarget(r.URL.Path)
	if topic == "" {
		if r.URL.Path == "/" {
			filename, err := ws.mostRecentFile()
			if err != nil {
				html := ws.renderNoFilesPage()
				w.Header().Set("Content-Type", "text/html")
				w.Write([]byte(html))
				return
			}
			targetFile = filename
		} else {
			http.NotFound(w, r)
			return
		}
	}

	// Serve the markdown file preview
	fullPath := filepath.Join(ws.watchDir, targetFile)

	ws.mu.Lock()
	memContent, hasMem := ws.memFiles[targetFile]
	ws.mu.Unlock()

	var html string
	var err error
	if hasMem {
		html, _, err = RenderMarkdownContent([]byte(memContent), true)
	} else {
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			html = ws.renderDeletedPage(targetFile)
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(html))
			return
		}
		html, _, err = RenderMarkdownFile(fullPath, true)
	}

	if err != nil {
		http.Error(w, fmt.Sprintf("Error rendering markdown: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(html))
}

// serveSSE manages client SSE subscription and streams changes.
func (ws *WatchServer) serveSSE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	fileParam := r.URL.Query().Get("file")
	var topic string
	if fileParam == "" || fileParam == "/" {
		topic = ""
	} else {
		_, t := ws.resolveTarget(fileParam)
		if t == "" {
			http.NotFound(w, r)
			return
		}
		topic = t
	}

	client := &Client{
		file: topic,
		ch:   make(chan string, 1),
	}

	ws.register(client)
	defer ws.unregister(client)

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case newHTML := <-client.ch:
			cleanHTML := strings.TrimSuffix(newHTML, "\n")
			ssePayload := strings.ReplaceAll(cleanHTML, "\n", "\ndata: ")
			fmt.Fprintf(w, "data: %s\n\n", ssePayload)
			flusher.Flush()
		}
	}
}

// startWatcher initializes and starts monitoring the watch directory.
func (ws *WatchServer) startWatcher() (*fsnotify.Watcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create fsnotify watcher: %w", err)
	}

	if err := watcher.Add(ws.watchDir); err != nil {
		watcher.Close()
		return nil, fmt.Errorf("failed to watch directory %s: %w", ws.watchDir, err)
	}

	go func() {
		var (
			mu      sync.Mutex
			pending = make(map[string]bool)
			timer   *time.Timer
		)

		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}

				if !strings.HasSuffix(strings.ToLower(event.Name), ".md") {
					continue
				}

				mu.Lock()
				pending[event.Name] = true
				if timer != nil {
					timer.Stop()
				}

				// Debounce events for 50ms
				timer = time.AfterFunc(50*time.Millisecond, func() {
					mu.Lock()
					eventsToProcess := pending
					pending = make(map[string]bool)
					mu.Unlock()
					ws.processChanges(eventsToProcess)
				})
				mu.Unlock()

			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Printf("Watcher error: %v", err)
			}
		}
	}()

	return watcher, nil
}

// processChanges handles triggered fsnotify events by re-rendering files and notifying clients.
func (ws *WatchServer) processChanges(files map[string]bool) {
	for path := range files {
		base := filepath.Base(path)

		if _, err := os.Stat(path); err == nil {
			html, _, err := RenderMarkdownFile(path, true)
			if err == nil {
				ws.broadcast(base, html)
				ws.broadcast("", html)
			} else {
				log.Printf("Error rendering %s: %v", base, err)
			}
		} else if os.IsNotExist(err) {
			html := ws.renderDeletedPage(base)
			ws.broadcast(base, html)

			recentFile, err := ws.mostRecentFile()
			if err == nil {
				recentHTML, _, err := RenderMarkdownFile(filepath.Join(ws.watchDir, recentFile), true)
				if err == nil {
					ws.broadcast("", recentHTML)
				}
			} else {
				ws.broadcast("", ws.renderNoFilesPage())
			}
		}
	}
}

// RunWatch starts a watch server on the given directory and port.
func RunWatch(dir string, port int, openBrowser bool) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("failed to get absolute path of directory: %w", err)
	}
	if info, err := os.Stat(absDir); err != nil {
		return fmt.Errorf("directory does not exist: %w", err)
	} else if !info.IsDir() {
		return fmt.Errorf("path is not a directory: %s", absDir)
	}

	ws := &WatchServer{
		watchDir: absDir,
		clients:  make(map[*Client]bool),
	}

	watcher, err := ws.startWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}
	defer listener.Close()

	actualPort := listener.Addr().(*net.TCPAddr).Port
	url := fmt.Sprintf("http://127.0.0.1:%d", actualPort)

	fmt.Printf("Watching directory: %s\n", absDir)
	fmt.Printf("Serving preview at: %s\n", url)

	if openBrowser {
		go func() {
			_ = browser.OpenURL(url)
		}()
	}

	server := &http.Server{
		Handler: ws,
	}
	ws.server = server

	return server.Serve(listener)
}

// UpdateInMemoryFile updates the in-memory content of a file and broadcasts the change.
func (ws *WatchServer) UpdateInMemoryFile(filename string, content string, immediate bool) {
	ws.mu.Lock()
	if ws.memFiles == nil {
		ws.memFiles = make(map[string]string)
	}

	if !immediate {
		if ws.pendingChanges == nil {
			ws.pendingChanges = make(map[string]string)
		}
		ws.pendingChanges[filename] = content

		if ws.debounceTimer != nil {
			ws.debounceTimer.Stop()
		}

		ws.debounceTimer = time.AfterFunc(100*time.Millisecond, func() {
			ws.mu.Lock()
			changes := ws.pendingChanges
			ws.pendingChanges = make(map[string]string)
			for fname, data := range changes {
				ws.memFiles[fname] = data
			}
			ws.mu.Unlock()

			for fname, data := range changes {
				html, _, err := RenderMarkdownContent([]byte(data), true)
				if err == nil {
					ws.broadcast(fname, html)
					ws.broadcast("", html)
				} else {
					log.Printf("Error rendering in-memory %s: %v", fname, err)
				}
			}
		})
		ws.mu.Unlock()
		return
	}

	if ws.pendingChanges != nil {
		delete(ws.pendingChanges, filename)
	}
	ws.memFiles[filename] = content
	ws.mu.Unlock()

	html, _, err := RenderMarkdownContent([]byte(content), true)
	if err == nil {
		ws.broadcast(filename, html)
		ws.broadcast("", html)
	} else {
		log.Printf("Error rendering in-memory %s: %v", filename, err)
	}
}

// RemoveInMemoryFile clears the document content from memory.
func (ws *WatchServer) RemoveInMemoryFile(filename string) {
	ws.mu.Lock()
	if ws.memFiles != nil {
		delete(ws.memFiles, filename)
	}
	ws.mu.Unlock()
}

// Close stops the HTTP server.
func (ws *WatchServer) Close() error {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	if ws.debounceTimer != nil {
		ws.debounceTimer.Stop()
	}
	if ws.server != nil {
		return ws.server.Close()
	}
	return nil
}

// StartLSPWatchServer starts a preview server on a random port for LSP integration, and opens the browser.
func StartLSPWatchServer(dir string) (*WatchServer, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path of directory: %w", err)
	}
	if info, err := os.Stat(absDir); err != nil {
		return nil, fmt.Errorf("directory does not exist: %w", err)
	} else if !info.IsDir() {
		return nil, fmt.Errorf("path is not a directory: %s", absDir)
	}

	ws := &WatchServer{
		watchDir: absDir,
		clients:  make(map[*Client]bool),
		memFiles: make(map[string]string),
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("failed to listen on random port: %w", err)
	}

	actualPort := listener.Addr().(*net.TCPAddr).Port
	url := fmt.Sprintf("http://127.0.0.1:%d", actualPort)

	// Since LSP runs on stdout/stdin, print startup logs to stderr
	log.Printf("LSP watch server starting on: %s", url)

	go func() {
		_ = browser.OpenURL(url)
	}()

	server := &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", actualPort),
		Handler: ws,
	}
	ws.server = server

	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Printf("LSP watch server error: %v", err)
		}
	}()

	return ws, nil
}
