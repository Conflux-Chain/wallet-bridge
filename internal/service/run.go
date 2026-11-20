package service

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/api"
	"github.com/Conflux-Chain/wallet-bridge/internal/app"
	"github.com/Conflux-Chain/wallet-bridge/internal/credentials"
	"github.com/Conflux-Chain/wallet-bridge/internal/platform"
	"github.com/Conflux-Chain/wallet-bridge/internal/store"
)

type Config struct {
	DataDir string
	Version string
	Assets  fs.FS
	Ready   func() error
}

type Error struct {
	Code    string
	Message string
	Cause   error
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }
func (e *Error) Unwrap() error { return e.Cause }

func Run(ctx context.Context, config Config) error {
	maintenance := time.NewTicker(30 * time.Second)
	defer maintenance.Stop()
	return run(ctx, config, net.Listen, maintenance.C)
}

func run(ctx context.Context, config Config, listen func(string, string) (net.Listener, error), maintenance <-chan time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if config.DataDir == "" {
		dir, err := platform.DataDir()
		if err != nil {
			return &Error{"data_unavailable", "Cannot resolve the user data directory", err}
		}
		config.DataDir = dir
	}
	lock, err := platform.AcquireServiceLock(config.DataDir)
	if err != nil {
		if errors.Is(err, platform.ErrLocked) {
			return &Error{"already_running", "Another service holds this data directory", err}
		}
		return &Error{"data_unavailable", "Cannot establish a private data directory and process lock", err}
	}
	defer lock.Close()
	listener, err := listen("tcp4", api.Address)
	if err != nil {
		if platform.IsPortInUse(err) {
			return &Error{"port_in_use", "The fixed loopback port is occupied or reserved", err}
		}
		return &Error{"listen_failed", "Cannot listen on the fixed loopback address", err}
	}
	defer listener.Close()
	if err := credentials.Ensure(config.DataDir); err != nil {
		return &Error{"credential_error", "Cannot initialize or validate local credentials", err}
	}
	token, err := credentials.Read(config.DataDir)
	if err != nil {
		return &Error{"credential_error", "Cannot read the service credential", err}
	}
	initializing, cancelInitialization := context.WithTimeout(ctx, 5*time.Second)
	database, err := store.Open(initializing, config.DataDir)
	cancelInitialization()
	if err != nil {
		return &Error{"storage_error", "Cannot initialize or migrate the private database; existing data was retained", err}
	}
	defer database.Close()
	connections, err := app.NewConnectionService(database)
	if err != nil {
		return &Error{"configuration_error", "Cannot configure connection lifecycle", err}
	}
	if err := expireConnections(ctx, connections); err != nil {
		return err
	}
	browserAuthorization, err := app.NewBrowserAuthorization(database)
	if err != nil {
		return &Error{"configuration_error", "Cannot configure browser authorization", err}
	}
	stop := make(chan struct{})
	var once sync.Once
	handler, err := api.New(api.Config{
		Version: config.Version, ServiceToken: token, Assets: config.Assets,
		StartedAt: time.Now(), Stop: func() { once.Do(func() { close(stop) }) },
		BrowserAuthorization: browserAuthorization,
	})
	if err != nil {
		return &Error{"configuration_error", "Cannot configure the local API", err}
	}
	server := &http.Server{
		Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second,
		WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8 << 10,
		ErrorLog: log.New(io.Discard, "", 0),
	}
	defer server.Close()
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	if config.Ready != nil {
		if err := config.Ready(); err != nil {
			return &Error{"output_error", "Cannot report service readiness", err}
		}
	}
	var maintenanceErr error
running:
	for {
		select {
		case err := <-served:
			return &Error{"serve_failed", "The HTTP listener stopped unexpectedly", err}
		case <-ctx.Done():
			break running
		case <-stop:
			break running
		case <-maintenance:
			if maintenanceErr = expireConnections(ctx, connections); maintenanceErr != nil {
				if ctx.Err() != nil {
					maintenanceErr = nil
				}
				break running
			}
		}
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdown); err != nil {
		return &Error{"shutdown_failed", "The HTTP service did not finish graceful shutdown within five seconds", err}
	}
	if err := <-served; !errors.Is(err, http.ErrServerClosed) {
		return &Error{"serve_failed", "The HTTP listener stopped unexpectedly", err}
	}
	return maintenanceErr
}

func expireConnections(ctx context.Context, connections *app.ConnectionService) error {
	maintenance, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := connections.Expire(maintenance); err != nil {
		return &Error{"storage_error", "Cannot maintain connection and Session deadlines; existing data was retained", err}
	}
	return nil
}
