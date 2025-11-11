//go:build windows

package ipc

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/Microsoft/go-winio"
	"github.com/mageg-x/dedupfs/common/utils"
)

func Listen(path string) (net.Listener, error) {
	return winio.ListenPipe(path, &winio.PipeConfig{
		SecurityDescriptor: "D:P(A;;GA;;;WD)", // Everyone full access
	})
}

func Dial(path string) (net.Conn, error) {
	return winio.DialPipe(path, nil)
}

func DialTimeout(path string, timeout time.Duration) (net.Conn, error) {
	// Use a context with timeout instead of PipeDialConfig
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// DialPipe with timeout as deadline
	return winio.DialPipeContext(ctx, path)
}

func GetPath(mountPoint string) string {
	pipeName := fmt.Sprintf("\\\\.\\pipe\\dedupfs_%s", utils.CalcHash([]byte(mountPoint)))
	return pipeName
}
