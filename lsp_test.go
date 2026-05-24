package main

import (
	"context"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sourcegraph/jsonrpc2"
)

type dummyHandler struct{}

func (dummyHandler) Handle(ctx context.Context, conn *jsonrpc2.Conn, req *jsonrpc2.Request) {}

func TestLSPInitialize(t *testing.T) {
	// Create pipes for communication.
	// clientWrite -> serverRead
	serverRead, clientWrite := io.Pipe()
	// serverWrite -> clientRead
	clientRead, serverWrite := io.Pipe()

	// Start the server in a goroutine.
	serverErrChan := make(chan error, 1)
	go func() {
		err := RunLSP(serverRead, serverWrite, false)
		// Close serverWrite so the client receives EOF.
		_ = serverWrite.Close()
		serverErrChan <- err
	}()

	// Start the client in this goroutine using jsonrpc2.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	clientStream := jsonrpc2.NewBufferedStream(readWriteCloser{Reader: clientRead, Writer: clientWrite}, jsonrpc2.VSCodeObjectCodec{})
	clientConn := jsonrpc2.NewConn(ctx, clientStream, dummyHandler{})
	defer clientConn.Close()

	// Send initialize request.
	var result struct {
		Capabilities map[string]interface{} `json:"capabilities"`
	}
	err := clientConn.Call(ctx, "initialize", nil, &result)
	if err != nil {
		t.Fatalf("failed to call initialize: %v", err)
	}

	if result.Capabilities == nil {
		t.Error("expected non-nil capabilities in initialize result")
	}

	// Try sending a random unknown request.
	var dummy interface{}
	err = clientConn.Call(ctx, "textDocument/hover", nil, &dummy)
	if err == nil {
		t.Error("expected error for unknown request, got nil")
	} else {
		rpcErr, ok := err.(*jsonrpc2.Error)
		if !ok {
			t.Errorf("expected *jsonrpc2.Error, got %T: %v", err, err)
		} else if rpcErr.Code != jsonrpc2.CodeMethodNotFound {
			t.Errorf("expected error code CodeMethodNotFound (-32601), got %d", rpcErr.Code)
		}
	}

	// Close clientWrite to signal EOF to server.
	_ = clientWrite.Close()

	// Wait for server to finish.
	select {
	case err := <-serverErrChan:
		if err != nil && err != io.EOF {
			t.Logf("server exited with: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Error("timeout waiting for server to exit")
	}
}

func TestLSPWatchServer(t *testing.T) {
	// Create a temp directory for workspace
	tmpDir, err := os.MkdirTemp("", "zeka-lsp-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create pipes for communication.
	serverRead, clientWrite := io.Pipe()
	clientRead, serverWrite := io.Pipe()

	serverErrChan := make(chan error, 1)
	go func() {
		err := RunLSP(serverRead, serverWrite, true)
		_ = serverWrite.Close()
		serverErrChan <- err
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	clientStream := jsonrpc2.NewBufferedStream(readWriteCloser{Reader: clientRead, Writer: clientWrite}, jsonrpc2.VSCodeObjectCodec{})
	clientConn := jsonrpc2.NewConn(ctx, clientStream, dummyHandler{})
	defer clientConn.Close()

	// Convert temp directory path to file URI
	tmpURI := "file://" + filepath.ToSlash(tmpDir)

	// Call initialize
	var initResult struct {
		Capabilities struct {
			TextDocumentSync struct {
				OpenClose bool `json:"openClose"`
				Change    int  `json:"change"`
			} `json:"textDocumentSync"`
		} `json:"capabilities"`
	}

	initParams := struct {
		RootURI string `json:"rootUri"`
	}{
		RootURI: tmpURI,
	}

	err = clientConn.Call(ctx, "initialize", initParams, &initResult)
	if err != nil {
		t.Fatalf("initialize failed: %v", err)
	}

	if !initResult.Capabilities.TextDocumentSync.OpenClose || initResult.Capabilities.TextDocumentSync.Change != 1 {
		t.Errorf("unexpected textDocumentSync capabilities: %+v", initResult.Capabilities.TextDocumentSync)
	}

	// Wait a moment for server to start
	time.Sleep(100 * time.Millisecond)

	// Ensure lastLSPWatchServer is set
	if lastLSPWatchServer == nil {
		t.Fatal("expected lastLSPWatchServer to be initialized, got nil")
	}

	// Read the actual port/address the server bound to
	lastLSPWatchServer.mu.Lock()
	srv := lastLSPWatchServer.server
	addr := srv.Addr
	lastLSPWatchServer.mu.Unlock()

	if addr == "" {
		t.Fatal("expected watch server address to be populated")
	}

	// 1. Send didOpen notification for a file named "testfile.md"
	fileURI := tmpURI + "/testfile.md"
	didOpenNotification := struct {
		TextDocument struct {
			URI        string `json:"uri"`
			LanguageID string `json:"languageId"`
			Version    int    `json:"version"`
			Text       string `json:"text"`
		} `json:"textDocument"`
	}{}
	didOpenNotification.TextDocument.URI = fileURI
	didOpenNotification.TextDocument.LanguageID = "markdown"
	didOpenNotification.TextDocument.Version = 1
	didOpenNotification.TextDocument.Text = "# Title\nHello LSP World"

	err = clientConn.Notify(ctx, "textDocument/didOpen", didOpenNotification)
	if err != nil {
		t.Fatalf("failed to notify didOpen: %v", err)
	}

	// Wait for processing (immediate render on didOpen)
	time.Sleep(100 * time.Millisecond)

	// Query the preview server
	resp, err := http.Get("http://" + addr + "/testfile.html")
	if err != nil {
		t.Fatalf("failed to GET /testfile.html: %v", err)
	}
	body, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	if !strings.Contains(string(body), "Hello LSP World") {
		t.Errorf("expected body to contain 'Hello LSP World', got: %s", string(body))
	}

	// 2. Send didChange notification
	didChangeNotification := struct {
		TextDocument struct {
			URI     string `json:"uri"`
			Version int    `json:"version"`
		} `json:"textDocument"`
		ContentChanges []struct {
			Text string `json:"text"`
		} `json:"contentChanges"`
	}{}
	didChangeNotification.TextDocument.URI = fileURI
	didChangeNotification.TextDocument.Version = 2
	didChangeNotification.ContentChanges = []struct {
		Text string `json:"text"`
	}{
		{Text: "# Title\nModified LSP World"},
	}

	err = clientConn.Notify(ctx, "textDocument/didChange", didChangeNotification)
	if err != nil {
		t.Fatalf("failed to notify didChange: %v", err)
	}

	// Verify that the change is NOT immediate due to debouncing
	time.Sleep(20 * time.Millisecond)
	resp, err = http.Get("http://" + addr + "/testfile.html")
	if err == nil {
		body, _ = ioutil.ReadAll(resp.Body)
		resp.Body.Close()
		if strings.Contains(string(body), "Modified LSP World") {
			t.Errorf("didChange rendered immediately without debouncing")
		}
	}

	// Wait for debounce cooldown (100ms + some buffer)
	time.Sleep(150 * time.Millisecond)

	// Now it should be updated
	resp, err = http.Get("http://" + addr + "/testfile.html")
	if err != nil {
		t.Fatalf("failed to GET /testfile.html after debounce: %v", err)
	}
	body, err = ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}

	if !strings.Contains(string(body), "Modified LSP World") {
		t.Errorf("expected body to contain 'Modified LSP World' after debounce, got: %s", string(body))
	}

	// 3. Send didClose
	didCloseNotification := struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
	}{}
	didCloseNotification.TextDocument.URI = fileURI

	err = clientConn.Notify(ctx, "textDocument/didClose", didCloseNotification)
	if err != nil {
		t.Fatalf("failed to notify didClose: %v", err)
	}

	// 4. Close connection and wait for server to exit
	_ = clientWrite.Close()

	select {
	case err := <-serverErrChan:
		if err != nil && err != io.EOF {
			t.Logf("server exited with: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Error("timeout waiting for server to exit")
	}

	// Try querying the preview server again to verify it is closed
	_, err = http.Get("http://" + addr + "/testfile.html")
	if err == nil {
		t.Error("expected watch server to be closed, but GET succeeded")
	}
}
