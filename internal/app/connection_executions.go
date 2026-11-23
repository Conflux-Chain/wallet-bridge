package app

import (
	"context"
	"errors"
	"time"
)

const ConnectionExecutionHeartbeatTimeout = 90 * time.Second

type ConnectionExecutionPhase string

const (
	ExecutionReady              ConnectionExecutionPhase = "ready"
	ExecutionCalling            ConnectionExecutionPhase = "calling"
	ExecutionInterruptedReady   ConnectionExecutionPhase = "interrupted_ready"
	ExecutionInterruptedCalling ConnectionExecutionPhase = "interrupted_calling"
	ExecutionFinished           ConnectionExecutionPhase = "finished"
)

var (
	ErrInvalidConnectionExecution  = errors.New("invalid connection execution")
	ErrConnectionExecutionNotFound = errors.New("connection execution not found")
	ErrConnectionExecutionConflict = errors.New("connection execution conflicts with the current owner or phase")
	ErrWalletBusy                  = errors.New("another request occupies the wallet slot")
)

// Execution IDs distinguish operations, not credentials. BrowserGrantID must
// come from the adapter's authenticated subject, never an untrusted body field.
type ConnectionExecutionRef struct {
	ConnectionID   string
	ID             string
	BrowserGrantID string
}

type ConnectionExecution struct {
	ConnectionExecutionRef
	Phase      ConnectionExecutionPhase
	LastSeenAt time.Time
	CallID     string
}

type WalletRequestKind string

const (
	WalletConnection  WalletRequestKind = "connection"
	WalletTransaction WalletRequestKind = "transaction"
)

type ActiveWalletRequest struct {
	Kind  WalletRequestKind
	ID    string
	Phase ConnectionExecutionPhase
}

type ConnectionCallRef struct {
	ConnectionExecutionRef
	CallID string
}

// Invoke is true only for a newly committed handoff, never a replay. A lost
// response must not be treated as permission to invoke the wallet again.
type ConnectionCallPermit struct {
	Execution ConnectionExecution
	Invoke    bool
}

type ConnectionMaintenanceCounts struct {
	ConnectionExpiryCounts
	Executions int64
}

type ConnectionExecutionStore interface {
	// Reads do not renew liveness, take ownership, or normalize stale phases.
	GetConnectionExecution(context.Context, string) (ConnectionExecution, error)
	GetActiveWalletRequest(context.Context) (*ActiveWalletRequest, error)
	ClaimConnectionExecution(context.Context, ConnectionExecutionRef, time.Time) (ConnectionExecution, error)
	TouchConnectionExecution(context.Context, ConnectionExecutionRef, time.Time) (ConnectionExecution, error)
	InterruptConnectionExecution(context.Context, ConnectionExecutionRef, time.Time) (ConnectionExecution, error)
	// Recovery is an explicit user action. previousID fences the replaced
	// execution; ref.ID must be fresh. A terminal request only releases waiting.
	RecoverConnectionExecution(context.Context, string, ConnectionExecutionRef, time.Time) (ConnectionExecution, error)
	// previousCallID fences delayed handoffs after a subsequent call. Calls use
	// fresh IDs; retries reuse both the ID and its expected predecessor.
	BeginConnectionCall(context.Context, ConnectionCallRef, string, time.Time) (ConnectionCallPermit, error)
	// Return records only that the wallet call ended; it never grants consent.
	ReturnConnectionCall(context.Context, ConnectionCallRef, time.Time) (ConnectionExecution, error)
	// Decisions atomically check the active browser subject and ready execution.
	// Matching terminal replays preserve the original result and Session lifetime.
	CompleteConnectionExecution(context.Context, ConnectionExecutionRef, ConnectionCompletion, time.Time) (ConnectionRequest, error)
	RejectConnectionExecution(context.Context, ConnectionExecutionRef, time.Time) (ConnectionRequest, error)
	MaintainConnectionExecutions(context.Context, time.Time) (int64, error)
	InterruptAllConnectionExecutions(context.Context, time.Time) (int64, error)
	MaintainConnectionState(context.Context, time.Time, bool) (ConnectionMaintenanceCounts, error)
}
