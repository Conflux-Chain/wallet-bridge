package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/Conflux-Chain/wallet-bridge/internal/app"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

func (s *Store) CompleteConnectionRequest(ctx context.Context, id string, completion app.ConnectionCompletion, now time.Time) (app.ConnectionRequest, error) {
	if !validCompletion(completion) {
		return app.ConnectionRequest{}, app.ErrInvalidSession
	}
	return s.transitionConnection(ctx, id, app.ConnectionSucceeded, completion, now)
}

func (s *Store) FinishConnectionRequest(ctx context.Context, id string, state app.ConnectionState, now time.Time) (app.ConnectionRequest, error) {
	if state != app.ConnectionCanceled && state != app.ConnectionRejected && state != app.ConnectionExpired {
		return app.ConnectionRequest{}, app.ErrInvalidConnectionRequest
	}
	return s.transitionConnection(ctx, id, state, app.ConnectionCompletion{}, now)
}

func (s *Store) transitionConnection(ctx context.Context, id string, desired app.ConnectionState, completion app.ConnectionCompletion, now time.Time) (app.ConnectionRequest, error) {
	if !validID(id) || !validTime(now) {
		return app.ConnectionRequest{}, app.ErrInvalidConnectionRequest
	}
	now = time.UnixMilli(now.UnixMilli()).UTC()
	// The configured immediate transaction acquires the write lock before reading
	// state, so concurrent completion and cancellation cannot both win.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return app.ConnectionRequest{}, err
	}
	defer tx.Rollback()
	request, err := scanConnection(tx.QueryRowContext(ctx, connectionSelect+" WHERE r.id = ?", id))
	if err != nil {
		return app.ConnectionRequest{}, err
	}
	if request.State != app.ConnectionPending {
		if request.State != desired {
			return request, app.ErrConnectionConflict
		}
		if desired == app.ConnectionSucceeded {
			matches, err := matchesCompletion(ctx, tx, id, completion)
			if err != nil {
				return app.ConnectionRequest{}, err
			}
			if !matches {
				return request, app.ErrConnectionConflict
			}
		}
		return request, nil
	}
	if now.Before(request.CreatedAt) {
		return app.ConnectionRequest{}, app.ErrInvalidConnectionRequest
	}
	state, finished := desired, now
	if !now.Before(request.ExpiresAt) {
		state, finished = app.ConnectionExpired, request.ExpiresAt
	} else if desired == app.ConnectionExpired {
		return app.ConnectionRequest{}, app.ErrInvalidConnectionRequest
	}
	if state == app.ConnectionSucceeded && completion.ChainID != request.ChainID {
		return app.ConnectionRequest{}, app.ErrInvalidSession
	}
	if _, err := tx.ExecContext(ctx, "UPDATE connection_requests SET state = ?, finished_at = ? WHERE id = ?", state, finished.UnixMilli(), id); err != nil {
		return app.ConnectionRequest{}, err
	}
	if state == app.ConnectionSucceeded {
		_, err := tx.ExecContext(ctx, `INSERT INTO sessions
(id, connection_id, wallet_kind, wallet_name, wallet_rdns, account, chain_id, created_at, last_access_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, completion.SessionID, id, completion.Wallet.Kind,
			completion.Wallet.Name, completion.Wallet.RDNS, completion.Account, completion.ChainID, now.UnixMilli(), now.UnixMilli())
		var failure *sqlite.Error
		if errors.As(err, &failure) && (failure.Code() == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY || failure.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE) {
			return app.ConnectionRequest{}, app.ErrSessionExists
		}
		if err != nil {
			return app.ConnectionRequest{}, err
		}
		request.SessionID = completion.SessionID
	}
	if err := tx.Commit(); err != nil {
		return app.ConnectionRequest{}, err
	}
	request.State, request.FinishedAt = state, &finished
	if state != desired {
		// Expiry is durable even though the requested outcome was refused.
		return request, app.ErrConnectionConflict
	}
	return request, nil
}

func matchesCompletion(ctx context.Context, tx *sql.Tx, id string, completion app.ConnectionCompletion) (bool, error) {
	var matches bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM sessions
WHERE connection_id = ? AND id = ? AND wallet_kind = ? AND wallet_name = ?
AND wallet_rdns = ? AND account = ? AND chain_id = ?)`, id, completion.SessionID,
		completion.Wallet.Kind, completion.Wallet.Name, completion.Wallet.RDNS, completion.Account, completion.ChainID).Scan(&matches)
	return matches, err
}

func validCompletion(completion app.ConnectionCompletion) bool {
	if !validID(completion.SessionID) || !validChainID(completion.ChainID) || strings.TrimSpace(completion.Wallet.Name) == "" {
		return false
	}
	if completion.Wallet.Kind != app.WalletEIP6963 && completion.Wallet.Kind != app.WalletLegacy {
		return false
	}
	if completion.Wallet.Kind == app.WalletEIP6963 && strings.TrimSpace(completion.Wallet.RDNS) == "" {
		return false
	}
	if len(completion.Account) != 42 || !strings.HasPrefix(completion.Account, "0x") {
		return false
	}
	for _, c := range completion.Account[2:] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
