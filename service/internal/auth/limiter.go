package auth

import (
	"context"
	"time"
)

// LimitDecision describes whether a login attempt may proceed and, when
// denied, how long remains in the current fixed window.
type LimitDecision struct {
	Allowed    bool
	RetryAfter time.Duration
}

// LoginLimiter limits failed login attempts by username and client IP.
type LoginLimiter interface {
	Allow(ctx context.Context, username, ip string) (LimitDecision, error)
	RecordFailure(ctx context.Context, username, ip string) error
	ResetUsername(ctx context.Context, username string) error
}
