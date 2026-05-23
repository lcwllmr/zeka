package main

import (
	"context"
	"fmt"
	"io"

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

// lspHandler handles JSON-RPC 2.0 requests for the Language Server.
type lspHandler struct{}

// Handle processes incoming JSON-RPC requests.
func (h *lspHandler) Handle(ctx context.Context, conn *jsonrpc2.Conn, req *jsonrpc2.Request) {
	switch req.Method {
	case "initialize":
		type ServerCapabilities struct{}
		type InitializeResult struct {
			Capabilities ServerCapabilities `json:"capabilities"`
		}
		res := InitializeResult{
			Capabilities: ServerCapabilities{},
		}
		if err := conn.Reply(ctx, req.ID, res); err != nil {
			// LSP stream is stdout, any debugging/logging should go to stderr.
			return
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
func RunLSP(in io.Reader, out io.Writer) error {
	handler := &lspHandler{}
	stream := jsonrpc2.NewBufferedStream(readWriteCloser{Reader: in, Writer: out}, jsonrpc2.VSCodeObjectCodec{})
	conn := jsonrpc2.NewConn(context.Background(), stream, handler)
	<-conn.DisconnectNotify()
	return nil
}
