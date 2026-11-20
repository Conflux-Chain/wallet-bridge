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
	ErrConnectionExecutionBusy     = errors.New("another connection execution occupies the wallet slot")
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

type ConnectionExecutionStore interface {
	// Reads do not renew liveness, take ownership, or normalize stale phases.
	GetConnectionExecution(context.Context, string) (ConnectionExecution, error)
	ClaimConnectionExecution(context.Context, ConnectionExecutionRef, time.Time) (ConnectionExecution, error)
	TouchConnectionExecution(context.Context, ConnectionExecutionRef, time.Time) (ConnectionExecution, error)
	InterruptConnectionExecution(context.Context, ConnectionExecutionRef, time.Time) (ConnectionExecution, error)
	// Recovery is an explicit user action. previousID fences the replaced
	// execution; ref.ID must be fresh. A terminal request only releases waiting.
	RecoverConnectionExecution(context.Context, string, ConnectionExecutionRef, time.Time) (ConnectionExecution, error)
	MaintainConnectionExecutions(context.Context, time.Time) (int64, error)
	InterruptAllConnectionExecutions(context.Context, time.Time) (int64, error)
}
