package daemon

import (
	"context"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"github.com/vibium/clicker/internal/log"
	"github.com/vibium/clicker/internal/mcp"
	"github.com/vibium/clicker/internal/paths"
)

// Daemon manages a long-lived browser session accessible via a Unix socket.
type Daemon struct {
	listener     net.Listener
	handlers     *mcp.Handlers
	mu           sync.Mutex // serializes handler access
	version      string
	startTime    time.Time
	lastActivity time.Time
	idleTimeout  time.Duration
	socketPath   string
	shutdownOnce sync.Once
	done         chan struct{}
}

// Options configures a new Daemon.
type Options struct {
	Version         string
	ScreenshotDir   string
	Headless        bool
	IdleTimeout     time.Duration
	ChromedriverURL string
}

// New creates a new Daemon instance.
func New(opts Options) *Daemon {
	return &Daemon{
		handlers:     mcp.NewHandlers(opts.ScreenshotDir, opts.Headless, opts.ChromedriverURL),
		version:      opts.Version,
		idleTimeout:  opts.IdleTimeout,
		startTime:    time.Now(),
		lastActivity: time.Now(),
		done:         make(chan struct{}),
	}
}

// Run starts the daemon, listening for connections until the context is cancelled.
func (d *Daemon) Run(ctx context.Context) error {
	socketPath, err := paths.GetSocketPath()
	if err != nil {
		return fmt.Errorf("get socket path: %w", err)
	}
	d.socketPath = socketPath

	// Ensure parent directory exists
	dir, err := paths.GetDaemonDir()
	if err != nil {
		return fmt.Errorf("get daemon dir: %w", err)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create daemon dir: %w", err)
	}

	// Remove stale socket file
	os.Remove(socketPath)

	listener, err := listen(socketPath)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	d.listener = listener

	// Write PID file
	if err := WritePID(); err != nil {
		listener.Close()
		return fmt.Errorf("write PID: %w", err)
	}

	log.Debug("daemon started", "socket", socketPath, "pid", os.Getpid())

	// Start idle timeout watcher if configured
	if d.idleTimeout > 0 {
		go d.watchIdle(ctx)
	}

	// Accept connections until context cancelled
	go func() {
		<-ctx.Done()
		d.Shutdown()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-d.done:
				return nil // Clean shutdown
			default:
				log.Debug("accept error", "error", err)
				continue
			}
		}
		go d.handleConnection(conn)
	}
}

// Shutdown performs a clean daemon shutdown.
func (d *Daemon) Shutdown() {
	d.shutdownOnce.Do(func() {
		log.Debug("daemon shutting down")
		close(d.done)

		// Close browser session
		d.mu.Lock()
		d.handlers.Close()
		d.mu.Unlock()

		// Close listener
		if d.listener != nil {
			d.listener.Close()
		}

		// Clean up socket file
		if d.socketPath != "" {
			os.Remove(d.socketPath)
		}

		// Remove PID file
		RemovePID()
	})
}

// touchActivity updates the last activity timestamp.
func (d *Daemon) touchActivity() {
	d.mu.Lock()
	d.lastActivity = time.Now()
	d.mu.Unlock()
}

// watchIdle monitors for idle timeout and triggers shutdown.
func (d *Daemon) watchIdle(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			d.mu.Lock()
			idle := time.Since(d.lastActivity)
			d.mu.Unlock()

			if idle >= d.idleTimeout {
				log.Debug("idle timeout reached, shutting down", "idle", idle, "timeout", d.idleTimeout)
				d.Shutdown()
				return
			}
		case <-d.done:
			return
		case <-ctx.Done():
			return
		}
	}
}
