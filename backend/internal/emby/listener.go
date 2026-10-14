package emby

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const listenerShutdownTimeout = 10 * time.Second

// Listener owns the optional HTTP server used exclusively by Emby clients.
type Listener struct {
	mu      sync.Mutex
	handler http.Handler
	server  *http.Server
	port    int
}

func NewListener(handler http.Handler) *Listener {
	return &Listener{handler: handler}
}

func (l *Listener) Reconfigure(_ context.Context, enabled bool, port int) error {
	if l == nil || l.handler == nil {
		return errors.New("Emby server is unavailable")
	}

	l.mu.Lock()
	if enabled && l.server != nil && l.port == port {
		l.mu.Unlock()
		return nil
	}

	var nextListener net.Listener
	var nextServer *http.Server
	if enabled {
		bound, err := net.Listen("tcp4", net.JoinHostPort("0.0.0.0", strconv.Itoa(port)))
		if err != nil {
			l.mu.Unlock()
			return fmt.Errorf("emby port %d is unavailable", port)
		}
		nextListener = bound
		nextServer = &http.Server{
			Handler:           l.handler,
			ReadHeaderTimeout: 10 * time.Second,
		}
	}

	previousServer := l.server
	l.server = nextServer
	l.port = 0
	if enabled {
		l.port = port
	}
	l.mu.Unlock()

	if nextServer != nil {
		go func() {
			if err := nextServer.Serve(nextListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("Emby server stopped unexpectedly: %v", err)
			}
		}()
		log.Printf("Emby compatibility server listening on :%d", port)
	}
	if previousServer != nil {
		shutdownListenerServer(previousServer)
	}
	return nil
}

func (l *Listener) Shutdown(ctx context.Context) error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	server := l.server
	l.server = nil
	l.port = 0
	l.mu.Unlock()
	if server == nil {
		return nil
	}
	return server.Shutdown(ctx)
}

func shutdownListenerServer(server *http.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), listenerShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		_ = server.Close()
	}
}
