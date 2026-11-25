package app

import (
	"context"
	"errors"
	"time"
)

var (
	ErrActivityNotFound   = errors.New("activity not found")
	ErrActivityUnfinished = errors.New("activity is unfinished")
)

func ActivityRemovable(state string) bool {
	switch state {
	case "succeeded", "sent", "rejected", "canceled", "expired":
		return true
	default:
		return false
	}
}

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

func (a *ConnectionService) RemoveActivity(ctx context.Context, browser BrowserGrant, kind WalletRequestKind, id string) error {
	return a.store.RemoveActivity(ctx, browser.ID, kind, id)
}
