package app

import (
	"context"
	"time"
)

// ActivityKey remains stable when a wallet result changes.
type ActivityKey struct {
	CreatedAt time.Time
	ID        string
	Kind      WalletRequestKind
}

type Activity struct {
	ActivityKey
	ClientName string
	ChainID    string
	State      string
	SessionID  string
	TxHash     string
}

func (a *ConnectionService) Activity(ctx context.Context, browser BrowserGrant, after *ActivityKey, limit int) ([]Activity, error) {
	if !connectionID(browser.ID) {
		return nil, ErrInvalidBrowserCredential
	}
	return a.store.ListActivity(ctx, a.now(), after, limit)
}
