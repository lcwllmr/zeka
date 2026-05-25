package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"strings"
	"sync"

	"github.com/pkg/browser"
	"github.com/sourcegraph/jsonrpc2"
)

// readWriteCloser joins an io.Reader and io.Writer into an io.ReadWriteCloser.
type readWriteCloser struct {
	io.Reader
	io.Writer
}

func (rwc readWriteCloser) Close() error {
	return nil
}

// lastLSPWatchServer tracks the watch server started by the last active LSP server instance (for testing purposes).
var lastLSPWatchServer *WatchServer

// lspHandler handles JSON-RPC 2.0 requests for the Language Server.
type lspHandler struct {
	xFlag         bool
	mu            sync.Mutex
	ws            *WatchServer
	browserOpened bool
}

type didOpenParams struct {
	TextDocument struct {
		URI  string `json:"uri"`
		Text string `json:"text"`
	} `json:"textDocument"`
}

type didChangeParams struct {
	TextDocument struct {
		URI string `json:"uri"`
	} `json:"textDocument"`
	ContentChanges []struct {
		Text string `json:"text"`
	} `json:"contentChanges"`
}

type didCloseParams struct {
	TextDocument struct {
		URI string `json:"uri"`
	} `json:"textDocument"`
}

func parseFilePath(uri string) string {
	if !strings.HasPrefix(uri, "file://") {
		return ""
	}
	path := strings.TrimPrefix(uri, "file://")
	if len(path) > 2 && path[0] == '/' && path[2] == ':' {
		path = path[1:]
	}
	return filepath.Clean(path)
}

// Handle processes incoming JSON-RPC requests.
func (h *lspHandler) Handle(ctx context.Context, conn *jsonrpc2.Conn, req *jsonrpc2.Request) {
	switch req.Method {
	case "initialize":
		var params struct {
			RootPath string `json:"rootPath"`
			RootURI  string `json:"rootUri"`
		}
		if req.Params != nil {
			_ = json.Unmarshal(*req.Params, &params)
		}

		workspaceDir := ""
		if params.RootURI != "" {
			workspaceDir = parseFilePath(params.RootURI)
		}
		if workspaceDir == "" && params.RootPath != "" {
			workspaceDir = params.RootPath
		}
		if workspaceDir == "" {
			workspaceDir = "."
		}

		if h.xFlag {
			h.mu.Lock()
			if h.ws == nil {
				ws, err := StartLSPWatchServer(workspaceDir)
				if err != nil {
					log.Printf("Failed to start LSP watch server: %v", err)
				} else {
					h.ws = ws
					lastLSPWatchServer = ws
				}
			}
			h.mu.Unlock()
		}

		type TextDocumentSyncOptions struct {
			OpenClose bool `json:"openClose"`
			Change    int  `json:"change"`
		}
		type ServerCapabilities struct {
			TextDocumentSync TextDocumentSyncOptions `json:"textDocumentSync"`
		}
		type InitializeResult struct {
			Capabilities ServerCapabilities `json:"capabilities"`
		}
		res := InitializeResult{
			Capabilities: ServerCapabilities{
				TextDocumentSync: TextDocumentSyncOptions{
					OpenClose: true,
					Change:    1, // Full document sync
				},
			},
		}
		if err := conn.Reply(ctx, req.ID, res); err != nil {
			return
		}

	case "textDocument/didOpen":
		if req.Params == nil {
			return
		}
		var params didOpenParams
		if err := json.Unmarshal(*req.Params, &params); err != nil {
			return
		}
		filename := filepath.Base(parseFilePath(params.TextDocument.URI))
		if !isZettelkastenFile(filename) {
			return
		}
		h.mu.Lock()
		ws := h.ws
		shouldOpen := h.xFlag && !h.browserOpened && ws != nil
		if shouldOpen {
			h.browserOpened = true
		}
		h.mu.Unlock()
		if ws != nil {
			ws.UpdateInMemoryFile(filename, params.TextDocument.Text, true)
			if shouldOpen {
				go func() {
					_ = browser.OpenURL(ws.url + "/" + filename)
				}()
			}
		}

	case "textDocument/didChange":
		if req.Params == nil {
			return
		}
		var params didChangeParams
		if err := json.Unmarshal(*req.Params, &params); err != nil {
			return
		}
		if len(params.ContentChanges) > 0 {
			filename := filepath.Base(parseFilePath(params.TextDocument.URI))
			if !isZettelkastenFile(filename) {
				return
			}
			h.mu.Lock()
			ws := h.ws
			h.mu.Unlock()
			if ws != nil {
				ws.UpdateInMemoryFile(filename, params.ContentChanges[0].Text, false)
			}
		}

	case "textDocument/didClose":
		if req.Params == nil {
			return
		}
		var params didCloseParams
		if err := json.Unmarshal(*req.Params, &params); err != nil {
			return
		}
		filename := filepath.Base(parseFilePath(params.TextDocument.URI))
		if !isZettelkastenFile(filename) {
			return
		}
		h.mu.Lock()
		ws := h.ws
		h.mu.Unlock()
		if ws != nil {
			ws.RemoveInMemoryFile(filename)
		}

	default:
		if !req.Notif {
			errErr := &jsonrpc2.Error{
				Code:    jsonrpc2.CodeMethodNotFound,
				Message: fmt.Sprintf("method not found: %s", req.Method),
			}
			_ = conn.ReplyWithError(ctx, req.ID, errErr)
		}
	}
}

// RunLSP starts the Language Server Protocol session on the given reader/writer.
func RunLSP(in io.Reader, out io.Writer, xFlag bool) error {
	handler := &lspHandler{
		xFlag: xFlag,
	}
	stream := jsonrpc2.NewBufferedStream(readWriteCloser{Reader: in, Writer: out}, jsonrpc2.VSCodeObjectCodec{})
	conn := jsonrpc2.NewConn(context.Background(), stream, handler)
	<-conn.DisconnectNotify()

	handler.mu.Lock()
	if handler.ws != nil {
		_ = handler.ws.Close()
	}
	handler.mu.Unlock()

	return nil
}
