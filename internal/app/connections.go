package app

import (
	"context"
	"errors"
	"time"
)

const ConnectionRequestLifetime = 5 * time.Minute

type ConnectionState string

const (
	ConnectionPending   ConnectionState = "pending"
	ConnectionSucceeded ConnectionState = "succeeded"
	ConnectionRejected  ConnectionState = "rejected"
	ConnectionCanceled  ConnectionState = "canceled"
	ConnectionExpired   ConnectionState = "expired"
)

var (
	ErrInvalidConnectionRequest  = errors.New("invalid connection request")
	ErrConnectionRequestExists   = errors.New("connection request ID or token digest already exists")
	ErrConnectionRequestNotFound = errors.New("connection request not found")
	ErrSessionNotFound           = errors.New("session not found")
)

type ConnectionRequest struct {
	ID         string
	ClientName string
	Intent     string
	ChainID    string
	ChainName  string
	State      ConnectionState
	CreatedAt  time.Time
	ExpiresAt  time.Time
	FinishedAt *time.Time
	SessionID  string
}

type WalletKind string

const (
	WalletEIP6963 WalletKind = "eip6963"
	WalletLegacy  WalletKind = "legacy"
)

// WalletDescriptor is self-reported metadata, not a verified extension identity.
// A page's EIP-6963 UUID is deliberately not a persistent wallet identifier.
type WalletDescriptor struct {
	Kind WalletKind
	Name string
	RDNS string
}

type Session struct {
	ID           string
	ConnectionID string
	Wallet       WalletDescriptor
	Account      string
	ChainID      string
	CreatedAt    time.Time
	LastAccessAt time.Time
	ClosedAt     *time.Time
}

// Records returned by this store never contain credentials or their digests.
// Reads preserve historical results; the application enforces active-session use.
type ConnectionStore interface {
	CreateConnectionRequest(context.Context, ConnectionRequest, [32]byte) error
	GetConnectionRequest(context.Context, string) (ConnectionRequest, error)
	FindConnectionRequest(context.Context, string, [32]byte) (ConnectionRequest, error)
	GetSession(context.Context, string) (Session, error)
	FindSession(context.Context, string, [32]byte) (Session, error)
}
