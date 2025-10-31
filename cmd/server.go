package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/mageg-x/dedupfs/internal/ipc"
	"github.com/mageg-x/dedupfs/internal/log"
	"github.com/mageg-x/dedupfs/internal/mount"
	"github.com/spf13/cobra"
)

const (
	// SocketPath is the default Unix socket path for dedupfs
	SocketPath = "/tmp/dedupfs.sock"
)

var (
	server *ipc.Server
)

var serverCmd = func() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Manage the dedupfs server",
		Long:  "Start or stop the dedupfs server that handles mount, unmount, and stats commands.",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Help()
		},
	}

	// 添加start子命令
	startCmd := &cobra.Command{
		Use:     "start",
		Short:   "Start the dedupfs server",
		RunE:    startServerAction,
		Example: `  dedupfs server start`,
	}

	// 添加stop子命令
	stopCmd := &cobra.Command{
		Use:     "stop",
		Short:   "Stop the dedupfs server",
		RunE:    stopServerAction,
		Example: `  dedupfs server stop`,
	}

	// 添加子命令到主命令
	cmd.AddCommand(startCmd)
	cmd.AddCommand(stopCmd)

	return cmd
}()

// initServer 初始化server命令（保持空函数以兼容现有调用）
func initServer() {}

// cleanup 设置程序退出时的清理工作
func cleanup() {
	// 设置信号处理，捕获中断信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)

	// 在单独的goroutine中处理信号
	go func() {
		sig := <-sigChan
		logger := log.GetLogger("dedupfs")
		logger.Infof("received signal: %s, initiating cleanup", sig)
		// 显式调用清理函数
		logger.Info("cleaning up mounted directories")
		if server != nil {
			server.Close()
		}
		mount.CleanupMounts()
		logger.Info("dedupfs program exited")
	}()
}

// startServerAction starts the dedupfs server
func startServerAction(cmd *cobra.Command, args []string) error {
	// Ensure socket directory exists
	socketDir := filepath.Dir(SocketPath)
	if err := os.MkdirAll(socketDir, 0755); err != nil {
		logger.Errorf("failed to create socket directory %s: %v", socketDir, err)
		return fmt.Errorf("failed to create socket directory: %w", err)
	}

	// Check if server is already running
	if ipc.IsServerRunning(SocketPath) {
		logger.Errorf("server is already running at %s", SocketPath)
		return fmt.Errorf("server is already running")
	}

	// Create and configure server
	server = ipc.NewServer(SocketPath)

	// Register handlers
	server.Register("mount", HandleMountCommand)
	server.Register("unmount", HandleUnmountCommand)
	server.Register("stats", HandleStatsCommand)
	server.Register("stop", handleStopCommand)

	logger.Infof("starting dedupfs server on %s", SocketPath)

	cleanup()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := server.Start(ctx); err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "use of closed network connection") {
			logger.Infof("failed to run server: %v", err)
		} else {
			logger.Errorf("failed to run server: %v", err)
		}
	}

	mount.CleanupMounts()
	logger.Info("server stopped gracefully")
	return nil
}

// stopServerAction sends a stop command to the running server
func stopServerAction(cmd *cobra.Command, args []string) error {
	// Check if server is running
	if !ipc.IsServerRunning(SocketPath) {
		logger.Errorf("server is not running at %s", SocketPath)
		return nil
	}

	logger.Infof("sending stop command to server at %s", SocketPath)

	// Create client and send stop command
	client := ipc.NewClient(SocketPath)
	resp, err := client.Call("stop", nil)
	if err != nil {
		logger.Errorf("failed to send stop command to %s: %v", SocketPath, err)
		return nil
	}

	if !resp.Ok {
		logger.Errorf("stop command failed: %s", resp.Msg)
		return nil
	}

	logger.Info("stop command sent successfully")
	return nil
}

// handleStopCommand handles stop requests to shut down the server
func handleStopCommand(ctx context.Context, req *ipc.Request) *ipc.Response {
	logger.Info("received stop command, shutting down server...")

	// Create a context with timeout for server shutdown
	_ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Close the server in a goroutine to avoid deadlock
	go func() {
		if err := server.Close(); err != nil {
			logger.Errorf("error closing server: %v", err)
		}
	}()

	// Wait for server to close or timeout
	<-_ctx.Done()
	return &ipc.Response{Ok: true, Msg: "server shutting down"}
}
