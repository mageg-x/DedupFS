package ipc

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"sync"

	"github.com/mageg-x/dedupfs/common/log"
)

var logger = log.GetLogger("ipc")

type Server struct {
	path     string
	handlers map[string]Handler
	listener net.Listener
	closed   bool
	mu       sync.RWMutex
}

func NewServer(socketPath string) *Server {
	return &Server{
		path:     socketPath,
		handlers: make(map[string]Handler),
	}
}

func (s *Server) Register(cmd string, handler Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[cmd] = handler
}

func (s *Server) handleConn(ctx context.Context, conn Conn) {
	defer conn.Close()

	dec := json.NewDecoder(conn)
	enc := json.NewEncoder(conn)

	for {
		var req Request
		if err := dec.Decode(&req); err != nil {
			if err != io.EOF {
				logger.Errorf("decode request: %v", err)
			}
			return
		}

		s.mu.RLock()
		handler := s.handlers[req.Cmd]
		s.mu.RUnlock()

		var resp *Response
		if handler == nil {
			resp = &Response{Ok: false, Msg: "unknown command: " + req.Cmd}
		} else {
			resp = handler(ctx, &req)
		}

		if err := enc.Encode(resp); err != nil {
			logger.Errorf("encode response: %v", err)
			return
		}
	}
}

func (s *Server) Start(ctx context.Context) error {
	listener, err := Listen(s.path)
	if err != nil {
		return err
	}
	s.listener = listener

	logger.Infof("server listening on %s", s.path)
	defer func() {
		listener.Close()
		logger.Info("server stopped")
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if s.closed {
				return nil
			}
			return err
		}

		go s.handleConn(ctx, conn)
	}
}

func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.listener != nil {
		s.listener.Close()
	}
	// Unix: remove socket file
	if _, err := Dial(s.path); err == nil {
		// 如果还能连上，说明没删干净（Windows 不需要）
		// 实际上更安全的做法是在 Listen 前删，已做
	}
	return nil
}
