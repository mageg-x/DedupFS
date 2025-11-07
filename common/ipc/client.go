package ipc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

type Client struct {
	path string
}

func NewClient(socketPath string) *Client {
	return &Client{path: socketPath}
}

// Call sends a command. If noResponse is true, EOF from server is treated as success.
func (c *Client) Call(ctx context.Context, cmd string, data interface{}, noResponse bool) (*Response, error) {
	// 1. 从 ctx 获取超时时间（或设默认值）
	var timeout time.Duration
	if deadline, ok := ctx.Deadline(); ok {
		timeout = time.Until(deadline)
		if timeout <= 0 {
			logger.Errorf("timeout exceeded")
			return nil, context.DeadlineExceeded
		}
	} else {
		// 默认超时：5秒（可根据需要调整）
		timeout = 5 * time.Second
	}

	// 2. 调用你的 DialTimeout
	conn, err := DialTimeout(c.path, timeout)
	if err != nil {
		logger.Errorf("connect: %v", err)
		return nil, fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	// 3. 设置连接的读写 deadline（可选但推荐）
	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline)
	}

	// 4. 发送请求...
	req := &Request{Cmd: cmd, Data: data, NoResponse: noResponse}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		logger.Errorf("faield send request: %v", err)
		return nil, fmt.Errorf("send request: %w", err)
	}

	// 5. 读取响应...
	var resp Response
	if noResponse {
		if err := json.NewDecoder(conn).Decode(&resp); err != nil {
			if err == io.EOF {
				logger.Debugf("server closed connection")
				return &Response{Ok: true}, nil
			}
			logger.Errorf("failed read response: %v", err)
			return nil, fmt.Errorf("read response: %w", err)
		}
	} else {
		if err := json.NewDecoder(conn).Decode(&resp); err != nil {
			logger.Errorf("failed decode response: %v", err)
			return nil, fmt.Errorf("read response: %w", err)
		}
	}

	return &resp, nil
}
