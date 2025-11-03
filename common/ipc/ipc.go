// Package ipc provides a simple command-based ipc mechanism over Unix domain sockets.
package ipc

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/mageg-x/dedupfs/common/log"
	"io"
	"net"
	"os"
	"time"
)

var (
	// 获取logger实例用于输出日志，与mount包保持一致的名称
	logger = log.GetLogger("dedupfs")
)

// Request is sent by the client.
type Request struct {
	Cmd  string      `json:"cmd"`
	Data interface{} `json:"data,omitempty"`
}

// Response is sent by the server.
type Response struct {
	Ok   bool        `json:"ok"`
	Msg  string      `json:"msg,omitempty"`
	Data interface{} `json:"data,omitempty"`
}

// Handler processes a command.
type Handler func(ctx context.Context, req *Request) *Response

// Server represents an ipc server.
type Server struct {
	socketPath string
	handlers   map[string]Handler
	listener   net.Listener
	closed     bool
}

// NewServer creates a new ipc server bound to a Unix socket.
// The socket path is typically like "/tmp/myapp.sock" or inside a runtime dir.
func NewServer(socketPath string) *Server {
	return &Server{
		socketPath: socketPath,
		handlers:   make(map[string]Handler),
	}
}

// IsServerRunning checks if a server is already running on the given socket path.
// It attempts to connect to the socket and returns true if successful (server is running),
// or false if the connection fails (server is not running).
func IsServerRunning(socketPath string) bool {
	// Try to connect to the socket
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		// Connection failed, server is not running
		return false
	}
	// Connection succeeded, server is running
	_ = conn.Close()
	return true
}

// Register binds a command name to a handler function.
func (s *Server) Register(cmd string, handler Handler) {
	s.handlers[cmd] = handler
}

// handleConnection handles a single client connection.
func (s *Server) handleConnection(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	dec := json.NewDecoder(conn)
	enc := json.NewEncoder(conn)

	for {
		var req Request
		if err := dec.Decode(&req); err != nil {
			// 区分EOF错误和其他错误
			if err == io.EOF {
				// Client disconnected gracefully, not an error
				logger.Debug("[ipc] client disconnected")
			} else {
				// Real error in decoding
				logger.Errorf("[ipc] failed to decode request: %v", err)
			}
			return
		}

		handler, exists := s.handlers[req.Cmd]
		var resp *Response
		if !exists {
			logger.Warnf("[ipc] unknown command: %s", req.Cmd)
			resp = &Response{Ok: false, Msg: "unknown command: " + req.Cmd}
		} else {
			resp = handler(ctx, &req)
		}

		if err := enc.Encode(resp); err != nil {
			logger.Errorf("[ipc] failed to encode response: %v", err)
			return
		}
	}
}

// Start listens on the Unix socket and serves requests.
// This method blocks.
func (s *Server) Start(ctx context.Context) error {
	// Remove stale socket file
	if err := os.Remove(s.socketPath); err != nil && !os.IsNotExist(err) {
		logger.Warnf("[ipc] failed to remove stale socket file: %v", err)
	}

	listener, err := net.Listen("unix", s.socketPath)
	if err != nil {
		logger.Errorf("[ipc] failed to listen on unix socket %s: %v", s.socketPath, err)
		return fmt.Errorf("failed to listen on unix socket %s: %w", s.socketPath, err)
	}
	s.listener = listener

	logger.Infof("[ipc] server listening on unix://%s", s.socketPath)

	// Cast listener to *net.UnixListener to set deadline
	unixListener, ok := listener.(*net.UnixListener)
	if !ok {
		logger.Errorf("[ipc] listener is not a UnixListener")
		return fmt.Errorf("listener is not a UnixListener")
	}

	for {
		// Check if context was cancelled
		select {
		case <-ctx.Done():
			logger.Info("[ipc] server shutting down")
			return nil
		default:
		}

		// Set a short deadline for accept so we can check ctx.Done() regularly
		unixListener.SetDeadline(time.Now().Add(time.Second))
		conn, err := listener.Accept()
		if err != nil {
			if opErr, ok := err.(*net.OpError); ok && opErr.Timeout() {
				// This was a timeout, continue loop to check ctx.Done()
				continue
			}
			// Usually happens on shutdown
			logger.Debugf("[ipc] accept failed: %v", err)
			return fmt.Errorf("accept failed: %w", err)
		}
		go s.handleConnection(ctx, conn)
	}
}

// Close shuts down the server and removes the socket file.
func (s *Server) Close() error {
	if s != nil && s.listener != nil {
		if err := s.listener.Close(); err != nil {
			logger.Errorf("[ipc] failed to close listener: %v", err)
		}
		if err := os.Remove(s.socketPath); err != nil && !os.IsNotExist(err) {
			logger.Warnf("[ipc] failed to remove socket file: %v", err)
		}
		logger.Infof("[ipc] server closed and socket removed")
	}
	return nil
}

// Client represents an ipc client.
type Client struct {
	socketPath string
}

// NewClient creates a client that connects to the given Unix socket.
func NewClient(socketPath string) *Client {
	return &Client{socketPath: socketPath}
}

// Call sends a command and waits for the response.
func (c *Client) Call(cmd string, data interface{}) (*Response, error) {
	conn, err := net.Dial("unix", c.socketPath)
	if err != nil {
		logger.Errorf("[ipc] failed to connect to %s: %v", c.socketPath, err)
		return nil, fmt.Errorf("failed to connect to %s: %w", c.socketPath, err)
	}
	defer conn.Close()

	req := &Request{Cmd: cmd, Data: data}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		logger.Errorf("[ipc] failed to send request: %v", err)
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	// For stop command, the server might close immediately after receiving the command
	// without sending a response, which would result in EOF error
	if cmd == "stop" {
		// Wait a short time to give the server a chance to respond if it can
		timer := time.NewTimer(100 * time.Millisecond)
		defer timer.Stop()

		// Try to read response with timeout
		responseChan := make(chan *Response, 1)
		errChan := make(chan error, 1)

		go func() {
			var resp Response
			if err := json.NewDecoder(conn).Decode(&resp); err != nil {
				errChan <- err
				return
			}
			responseChan <- &resp
		}()

		select {
		case resp := <-responseChan:
			return resp, nil
		case err := <-errChan:
			// If we get an EOF error for stop command, it's likely that the server
			// is shutting down as expected, so consider it a success
			if err == io.EOF {
				logger.Debugf("[ipc] received EOF after sending stop command, server is probably shutting down")
				return &Response{Ok: true}, nil
			}
			logger.Errorf("[ipc] failed to read response: %v", err)
			return nil, fmt.Errorf("failed to read response: %w", err)
		case <-timer.C:
			// If we timeout, assume the server is shutting down
			logger.Debugf("[ipc] timeout waiting for stop response, server is probably shutting down")
			return &Response{Ok: true}, nil
		}
	}

	// For other commands, proceed normally
	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		logger.Errorf("[ipc] failed to read response: %v", err)
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if !resp.Ok {
		logger.Warnf("[ipc] command %s failed: %s", cmd, resp.Msg)
	}

	return &resp, nil
}
