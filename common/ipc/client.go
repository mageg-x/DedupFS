package ipc

import (
	"encoding/json"
	"fmt"
	"io"
)

type Client struct {
	path string
}

func NewClient(socketPath string) *Client {
	return &Client{path: socketPath}
}

// Call sends a command. If noResponse is true, EOF from server is treated as success.
func (c *Client) Call(cmd string, data interface{}, noResponse bool) (*Response, error) {
	conn, err := Dial(c.path)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	req := &Request{
		Cmd:        cmd,
		Data:       data,
		NoResponse: noResponse,
	}

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}

	var resp Response

	if noResponse {
		if err := json.NewDecoder(conn).Decode(&resp); err != nil {
			if err == io.EOF {
				return &Response{Ok: true}, nil
			}
			return nil, fmt.Errorf("read response: %w", err)
		}
		return &resp, nil
	}

	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	return &resp, nil
}
