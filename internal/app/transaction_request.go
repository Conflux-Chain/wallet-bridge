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
	ErrTransactionReviewConflict  = errors.New("transaction review owner or phase changed")
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

// The browser adapter supplies BrowserGrantID after authenticating the page.
type TransactionReviewRef struct {
	TransactionID  string
	ID             string
	BrowserGrantID string
}

type TransactionReview struct {
	TransactionReviewRef
	Phase      ConnectionExecutionPhase
	LastSeenAt time.Time
	CallID     string
}

type ReviewAction string

const (
	ClaimReview           ReviewAction = "claim"
	HeartbeatReview       ReviewAction = "heartbeat"
	InterruptReview       ReviewAction = "interrupt"
	RecoverReview         ReviewAction = "recover"
	BeginPreparationCall  ReviewAction = "begin_preparation"
	ReturnPreparationCall ReviewAction = "return_preparation"
)

type ReviewCommand struct {
	Action     ReviewAction
	PreviousID string // Replaced execution ID for recovery, preceding call ID for begin.
	CallID     string // Only for wallet preparation (account access or chain switch).
}

type ReviewResult struct {
	Review TransactionReview
	Invoke bool // Granted once for a preparation call.
}
