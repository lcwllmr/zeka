package main

import (
	"context"
	"io"
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
		err := RunLSP(serverRead, serverWrite)
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
