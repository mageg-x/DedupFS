//go:build linux || darwin

package ipc

import (
	"net"
	"os"
	"time"
)

type Duration = time.Duration

func Listen(path string) (net.Listener, error) {
	os.Remove(path)
	return net.Listen("unix", path)
}

func Dial(path string) (net.Conn, error) {
	return net.Dial("unix", path)
}

func DialTimeout(path string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout("unix", path, timeout)
}
