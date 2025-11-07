// Package ipc provides cross-platform JSON-based IPC.
package ipc

import (
	"context"
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
