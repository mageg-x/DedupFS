// Package ipc provides cross-platform JSON-based IPC.
package ipc

import (
	"context"
	"time"
)

type Request struct {
	Cmd        string      `json:"cmd"`
	Data       interface{} `json:"data,omitempty"`
	NoResponse bool        `json:"no_response,omitempty"` // If true, server may close connection without sending a response.
}

type Response struct {
	Ok   bool   `json:"ok"`
	Msg  string `json:"msg,omitempty"`
	Data []byte `json:"data"`
}

type Handler func(ctx context.Context, req *Request) *Response
type HandlerEx func(ctx context.Context, s *Server, req *Request) *Response

// Abstract interfaces for testability
type Conn interface {
	Read([]byte) (int, error)
	Write([]byte) (int, error)
	Close() error
}

type Listener interface {
	Accept() (Conn, error)
	Close() error
}

// IsServerRunning checks if an IPC server is running at the specified path.
// It tries to establish a connection with a short timeout to determine if the server is available.
// Returns true if the server is running, false otherwise.
func IsServerRunning(path string) bool {
	// Use a short timeout to quickly determine if the server is running
	conn, err := DialTimeout(path, 500*time.Millisecond)
	if err != nil {
		return false
	}
	// Close the connection immediately if it was established
	conn.Close()
	return true
}
