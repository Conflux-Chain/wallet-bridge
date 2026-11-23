package store

import (
	"context"
	"database/sql"
	"fmt"
)

const applicationID = 0x57425247 // WBRG

var migrations = []string{`
CREATE TABLE browser_grants (
    id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 32),
    token_hash BLOB NOT NULL UNIQUE CHECK (length(token_hash) = 32),
    issued_at INTEGER NOT NULL CHECK (issued_at >= 0),
    expires_at INTEGER NOT NULL CHECK (expires_at - issued_at = 2592000000),
    revoked_at INTEGER CHECK (revoked_at >= issued_at)
);
CREATE INDEX browser_grants_by_issued_at ON browser_grants (issued_at DESC, id);
`, `
CREATE TABLE connection_requests (
    id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 32 AND id NOT GLOB '*[^0-9a-f]*'),
    token_hash BLOB NOT NULL UNIQUE CHECK (typeof(token_hash) = 'blob' AND length(token_hash) = 32),
    client_name TEXT NOT NULL CHECK (length(trim(client_name)) > 0),
    intent TEXT NOT NULL,
    chain_id TEXT NOT NULL CHECK (
        length(chain_id) >= 3 AND substr(chain_id, 1, 2) = '0x' AND
        substr(chain_id, 3) NOT GLOB '*[^0-9a-f]*' AND
        (length(chain_id) = 3 OR substr(chain_id, 3, 1) != '0')),
    chain_name TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('pending', 'succeeded', 'rejected', 'canceled', 'expired')),
    created_at INTEGER NOT NULL CHECK (created_at >= 0),
    expires_at INTEGER NOT NULL CHECK (expires_at - created_at = 300000),
    finished_at INTEGER,
    CHECK ((state = 'pending' AND finished_at IS NULL) OR
           (state != 'pending' AND finished_at IS NOT NULL AND finished_at >= created_at))
);
CREATE INDEX connection_requests_by_expiry ON connection_requests (state, expires_at, id);
CREATE TABLE sessions (
    id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 32 AND id NOT GLOB '*[^0-9a-f]*'),
    connection_id TEXT NOT NULL UNIQUE REFERENCES connection_requests (id),
    token_hash BLOB NOT NULL UNIQUE CHECK (typeof(token_hash) = 'blob' AND length(token_hash) = 32),
    wallet_kind TEXT NOT NULL CHECK (wallet_kind IN ('eip6963', 'legacy')),
    wallet_name TEXT NOT NULL CHECK (length(trim(wallet_name)) > 0),
    wallet_rdns TEXT NOT NULL CHECK (wallet_kind = 'legacy' OR length(trim(wallet_rdns)) > 0),
    account TEXT NOT NULL CHECK (length(account) = 42 AND substr(account, 1, 2) = '0x' AND
                                 substr(account, 3) NOT GLOB '*[^0-9a-f]*'),
    chain_id TEXT NOT NULL CHECK (
        length(chain_id) >= 3 AND substr(chain_id, 1, 2) = '0x' AND
        substr(chain_id, 3) NOT GLOB '*[^0-9a-f]*' AND
        (length(chain_id) = 3 OR substr(chain_id, 3, 1) != '0')),
    created_at INTEGER NOT NULL CHECK (created_at >= 0),
    last_access_at INTEGER NOT NULL CHECK (last_access_at >= created_at),
    closed_at INTEGER CHECK (closed_at >= created_at)
);
CREATE INDEX sessions_by_idle ON sessions (closed_at, last_access_at, id);
`, `
CREATE TABLE connection_requests_v3 (
    id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 32 AND id NOT GLOB '*[^0-9a-f]*'),
    client_name TEXT NOT NULL CHECK (length(trim(client_name)) > 0),
    intent TEXT NOT NULL,
    chain_id TEXT NOT NULL CHECK (
        length(chain_id) >= 3 AND substr(chain_id, 1, 2) = '0x' AND
        substr(chain_id, 3) NOT GLOB '*[^0-9a-f]*' AND
        (length(chain_id) = 3 OR substr(chain_id, 3, 1) != '0')),
    chain_name TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('pending', 'succeeded', 'rejected', 'canceled', 'expired')),
    created_at INTEGER NOT NULL CHECK (created_at >= 0),
    expires_at INTEGER NOT NULL CHECK (expires_at - created_at = 300000),
    finished_at INTEGER,
    CHECK ((state = 'pending' AND finished_at IS NULL) OR
           (state != 'pending' AND finished_at IS NOT NULL AND finished_at >= created_at))
);
INSERT INTO connection_requests_v3
(id, client_name, intent, chain_id, chain_name, state, created_at, expires_at, finished_at)
SELECT id, client_name, intent, chain_id, chain_name, state, created_at, expires_at, finished_at
FROM connection_requests;
CREATE TABLE sessions_v3 (
    id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 32 AND id NOT GLOB '*[^0-9a-f]*'),
    connection_id TEXT NOT NULL UNIQUE REFERENCES connection_requests_v3 (id),
    wallet_kind TEXT NOT NULL CHECK (wallet_kind IN ('eip6963', 'legacy')),
    wallet_name TEXT NOT NULL CHECK (length(trim(wallet_name)) > 0),
    wallet_rdns TEXT NOT NULL CHECK (wallet_kind = 'legacy' OR length(trim(wallet_rdns)) > 0),
    account TEXT NOT NULL CHECK (length(account) = 42 AND substr(account, 1, 2) = '0x' AND
                                 substr(account, 3) NOT GLOB '*[^0-9a-f]*'),
    chain_id TEXT NOT NULL CHECK (
        length(chain_id) >= 3 AND substr(chain_id, 1, 2) = '0x' AND
        substr(chain_id, 3) NOT GLOB '*[^0-9a-f]*' AND
        (length(chain_id) = 3 OR substr(chain_id, 3, 1) != '0')),
    created_at INTEGER NOT NULL CHECK (created_at >= 0),
    last_access_at INTEGER NOT NULL CHECK (last_access_at >= created_at),
    closed_at INTEGER CHECK (closed_at >= created_at)
);
INSERT INTO sessions_v3
(id, connection_id, wallet_kind, wallet_name, wallet_rdns, account, chain_id, created_at, last_access_at, closed_at)
SELECT id, connection_id, wallet_kind, wallet_name, wallet_rdns, account, chain_id, created_at, last_access_at, closed_at
FROM sessions;
DROP TABLE sessions;
DROP TABLE connection_requests;
ALTER TABLE connection_requests_v3 RENAME TO connection_requests;
ALTER TABLE sessions_v3 RENAME TO sessions;
CREATE INDEX connection_requests_by_expiry ON connection_requests (state, expires_at, id);
CREATE INDEX sessions_by_idle ON sessions (closed_at, last_access_at, id);
`, `
CREATE TABLE connection_executions (
    connection_id TEXT PRIMARY KEY NOT NULL REFERENCES connection_requests (id),
    id TEXT NOT NULL UNIQUE CHECK (length(id) = 32 AND id NOT GLOB '*[^0-9a-f]*'),
    browser_grant_id TEXT NOT NULL REFERENCES browser_grants (id),
    phase TEXT NOT NULL CHECK (phase IN ('ready', 'calling', 'interrupted_ready', 'interrupted_calling', 'finished')),
    last_seen_at INTEGER NOT NULL CHECK (last_seen_at >= 0),
    call_id TEXT CHECK (length(call_id) = 32 AND call_id NOT GLOB '*[^0-9a-f]*'),
    CHECK (phase NOT IN ('calling', 'interrupted_calling') OR call_id IS NOT NULL)
);
CREATE UNIQUE INDEX connection_executions_single_slot ON connection_executions ((1))
WHERE phase IN ('ready', 'calling', 'interrupted_calling');
`, `
CREATE TABLE transaction_requests (
    id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 32 AND id NOT GLOB '*[^0-9a-f]*'),
    session_id TEXT NOT NULL REFERENCES sessions (id),
    client_name TEXT NOT NULL CHECK (length(trim(client_name)) > 0),
    intent TEXT NOT NULL,
    from_address TEXT NOT NULL,
    to_address TEXT NOT NULL,
    chain_id TEXT NOT NULL,
    value TEXT NOT NULL,
    data TEXT NOT NULL,
    gas TEXT NOT NULL,
    gas_price TEXT NOT NULL,
    max_fee_per_gas TEXT NOT NULL,
    max_priority_fee_per_gas TEXT NOT NULL,
    created_at INTEGER NOT NULL CHECK (created_at >= 0),
    expires_at INTEGER NOT NULL CHECK (expires_at - created_at = 300000)
);
CREATE INDEX transaction_requests_by_session ON transaction_requests (session_id);
`, `
ALTER TABLE transaction_requests ADD COLUMN state TEXT NOT NULL DEFAULT 'pending'
    CHECK (state IN ('pending', 'canceled', 'expired'));
ALTER TABLE transaction_requests ADD COLUMN finished_at INTEGER
    CHECK ((state = 'pending' AND finished_at IS NULL) OR
           (state = 'expired' AND finished_at IS NOT NULL AND finished_at = expires_at) OR
           (state = 'canceled' AND finished_at IS NOT NULL AND finished_at >= created_at AND finished_at < expires_at));
CREATE INDEX transaction_requests_by_deadline ON transaction_requests (expires_at) WHERE state = 'pending';

UPDATE transaction_requests SET
    state = CASE WHEN expires_at <= s.closed_at THEN 'expired' ELSE 'canceled' END,
    finished_at = min(expires_at, max(transaction_requests.created_at, s.closed_at))
FROM sessions AS s WHERE session_id = s.id AND s.closed_at IS NOT NULL;

CREATE TRIGGER close_session_transactions AFTER UPDATE OF closed_at ON sessions
WHEN OLD.closed_at IS NULL AND NEW.closed_at IS NOT NULL BEGIN
    UPDATE transaction_requests SET
        state = CASE WHEN expires_at <= NEW.closed_at THEN 'expired' ELSE 'canceled' END,
        finished_at = min(expires_at, max(created_at, NEW.closed_at))
    WHERE session_id = NEW.id AND state = 'pending';
END;
`, `
CREATE TABLE wallet_executions (
    id TEXT PRIMARY KEY NOT NULL CHECK (length(id) = 32 AND id NOT GLOB '*[^0-9a-f]*'),
    connection_id TEXT UNIQUE REFERENCES connection_requests (id),
    transaction_id TEXT UNIQUE REFERENCES transaction_requests (id),
    browser_grant_id TEXT NOT NULL REFERENCES browser_grants (id),
    phase TEXT NOT NULL CHECK (phase IN ('ready', 'calling', 'interrupted_ready', 'interrupted_calling', 'finished')),
    last_seen_at INTEGER NOT NULL CHECK (last_seen_at >= 0),
    call_id TEXT CHECK (length(call_id) = 32 AND call_id NOT GLOB '*[^0-9a-f]*'),
    CHECK ((connection_id IS NULL) != (transaction_id IS NULL)),
    CHECK (phase NOT IN ('calling', 'interrupted_calling') OR call_id IS NOT NULL)
);
CREATE UNIQUE INDEX wallet_executions_single_slot ON wallet_executions ((1))
WHERE phase IN ('ready', 'calling', 'interrupted_calling');

INSERT INTO wallet_executions (id, connection_id, browser_grant_id, phase, last_seen_at, call_id)
SELECT id, connection_id, browser_grant_id, phase, last_seen_at, call_id FROM connection_executions;
DROP TABLE connection_executions;
`}

func migrate(ctx context.Context, db *sql.DB, steps []string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var version, identity int
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, "PRAGMA application_id").Scan(&identity); err != nil {
		return err
	}
	if version > len(steps) {
		return ErrSchemaTooNew
	}
	if version < 0 || (version != 0 && identity != applicationID) || (identity != 0 && identity != applicationID) {
		return ErrInvalidDatabase
	}
	if version == 0 {
		var objects int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%'").Scan(&objects); err != nil {
			return err
		}
		if objects != 0 {
			return ErrInvalidDatabase
		}
	}
	for next := version; next < len(steps); next++ {
		if _, err := tx.ExecContext(ctx, steps[next]); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", next+1)); err != nil {
			return err
		}
	}
	if version == 0 {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA application_id = %d", applicationID)); err != nil {
			return err
		}
	}
	return tx.Commit()
}
