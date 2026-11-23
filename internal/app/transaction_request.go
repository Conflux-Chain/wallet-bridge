package app

import (
	"errors"
	"time"
)

const TransactionRequestLifetime = 5 * time.Minute

var (
	ErrTransactionRequestConflict = errors.New("transaction request ID already identifies different content")
	ErrTransactionRequestNotFound = errors.New("transaction request not found")
	ErrSessionClosed              = errors.New("session is closed")
)

type TransactionRequestInput struct {
	ID         string
	SessionID  string
	ClientName string
	Intent     string
	Fields     map[string]string
}

type TransactionState string

const (
	TransactionPending  TransactionState = "pending"
	TransactionCanceled TransactionState = "canceled"
	TransactionExpired  TransactionState = "expired"
)

// TransactionRequest keeps the original payload and deadline alongside the latest decision.
type TransactionRequest struct {
	ID          string
	SessionID   string
	ClientName  string
	Intent      string
	Transaction Transaction
	CreatedAt   time.Time
	ExpiresAt   time.Time
	State       TransactionState
	FinishedAt  *time.Time
}
