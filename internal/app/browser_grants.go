package app

import (
	"context"
	"errors"
	"time"
)

const BrowserGrantLifetime = 30 * 24 * time.Hour

var (
	ErrInvalidBrowserGrant  = errors.New("invalid browser grant")
	ErrBrowserGrantExists   = errors.New("browser grant ID or token digest already exists")
	ErrBrowserGrantNotFound = errors.New("browser grant not found")
)

// BrowserGrant contains management metadata, never a cookie or token digest.
type BrowserGrant struct {
	ID        string
	IssuedAt  time.Time
	ExpiresAt time.Time
	RevokedAt *time.Time
}

type BrowserGrantStore interface {
	CreateBrowserGrant(context.Context, BrowserGrant, [32]byte) error
	FindActiveBrowserGrant(context.Context, [32]byte, time.Time) (BrowserGrant, error)
	ListBrowserGrants(context.Context) ([]BrowserGrant, error)
	RevokeBrowserGrant(context.Context, string, time.Time) error
	RevokeAllBrowserGrants(context.Context, time.Time) (int64, error)
}
