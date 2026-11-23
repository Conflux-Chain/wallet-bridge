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

// TransactionRequest is the immutable record created before approval.
type TransactionRequest struct {
	ID          string
	SessionID   string
	ClientName  string
	Intent      string
	Transaction Transaction
	CreatedAt   time.Time
	ExpiresAt   time.Time
}
