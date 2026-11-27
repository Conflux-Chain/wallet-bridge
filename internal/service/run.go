package service

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/api"
	"github.com/Conflux-Chain/wallet-bridge/internal/app"
	"github.com/Conflux-Chain/wallet-bridge/internal/credentials"
	"github.com/Conflux-Chain/wallet-bridge/internal/platform"
	"github.com/Conflux-Chain/wallet-bridge/internal/store"
	"github.com/Conflux-Chain/wallet-bridge/internal/update"
)

type Config struct {
	DataDir          string
	Version          string
	VerifyingUpgrade string
	Assets           fs.FS
	Ready            func() error
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
	checkService, err := update.IsWalletCheckService(config.DataDir)
	if err != nil {
		return &Error{"upgrade_state_error", "Cannot verify the local upgrade state", err}
	}
	if config.VerifyingUpgrade != "" {
		if checkService {
			return &Error{"upgrade_state_error", "Conflicting upgrade startup modes", nil}
		}
		if err := update.CheckCandidate(config.DataDir, config.VerifyingUpgrade, config.Version); err != nil {
			return &Error{"upgrade_state_error", "Cannot verify the candidate program", err}
		}
	} else if !checkService {
		if blocked, err := update.CheckUpgrade(config.DataDir); err != nil {
			return &Error{"upgrade_state_error", "Cannot verify the local upgrade state", err}
		} else if blocked != "" {
			return &Error{string(blocked), blocked.Error(), blocked}
		}
		if err := update.CheckServiceProgram(config.DataDir); err != nil {
			return &Error{"active_program_error", err.Error(), err}
		}
	}
	listener, err := listen("tcp4", api.Address)
	if err != nil {
		if platform.IsPortInUse(err) {
			return &Error{"port_in_use", "The fixed loopback port is occupied or reserved", err}
		}
		return &Error{"listen_failed", "Cannot listen on the fixed loopback address", err}
	}
	defer listener.Close()
	if config.VerifyingUpgrade == "" {
		if err := credentials.Ensure(config.DataDir); err != nil {
			return &Error{"credential_error", "Cannot initialize or validate local credentials", err}
		}
	} else {
		// A missing or empty live database must not be replaced by a new store.
		file, err := platform.OpenPrivateFile(filepath.Join(config.DataDir, store.DatabaseName))
		if err != nil {
			return &Error{"storage_error", "Cannot open the previous database", err}
		}
		info, err := file.Stat()
		if err := errors.Join(err, file.Close()); err != nil || info.Size() == 0 {
			return &Error{"storage_error", "Cannot verify the previous database", err}
		}
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
	if err := maintainConnections(ctx, connections, true); err != nil {
		return err
	}
	browserAuthorization, err := app.NewBrowserAuthorization(database)
	if err != nil {
		return &Error{"configuration_error", "Cannot configure browser authorization", err}
	}
	stop := make(chan struct{})
	var admitted chan struct{}
	if config.VerifyingUpgrade != "" {
		admitted = make(chan struct{})
	}
	var once sync.Once
	handler, err := api.New(api.Config{
		Version: config.Version, VerifyingUpgrade: config.VerifyingUpgrade, Admitted: admitted, ServiceToken: token, Assets: config.Assets,
		StartedAt: time.Now(), Stop: func() { once.Do(func() { close(stop) }) },
		BrowserAuthorization: browserAuthorization,
		OpenBrowser:          platform.OpenBrowser,
		Connections:          connections,
		Transactions:         database,
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
	var verifyOwner <-chan time.Time
	var ticker *time.Ticker
	if config.VerifyingUpgrade != "" {
		ticker = time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		verifyOwner = ticker.C
	}
running:
	for {
		select {
		case err := <-served:
			return &Error{"serve_failed", "The HTTP listener stopped unexpectedly", err}
		case <-ctx.Done():
			break running
		case <-stop:
			break running
		case <-verifyOwner:
			open, err := update.CandidateAdmitted(config.DataDir, config.VerifyingUpgrade)
			if err != nil {
				break running
			}
			if open {
				close(admitted)
				ticker.Stop()
				verifyOwner = nil
			}
		case <-maintenance:
			if maintenanceErr = maintainConnections(ctx, connections, false); maintenanceErr != nil {
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

func maintainConnections(ctx context.Context, connections *app.ConnectionService, restart bool) error {
	maintenance, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var err error
	if restart {
		_, err = connections.Restore(maintenance)
	} else {
		_, err = connections.Maintain(maintenance)
	}
	if err != nil {
		return &Error{"storage_error", "Cannot maintain connection, Session and execution state; existing data was retained", err}
	}
	return nil
}
