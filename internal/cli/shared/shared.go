// Package shared provides helpers shared across CLI sub-packages.
package shared

import (
	"context"
	"time"

	"github.com/shhac/agent-sql/internal/driver"
	"github.com/shhac/agent-sql/internal/resolve"
)

// GlobalFlags holds global CLI flag values shared across sub-packages.
type GlobalFlags struct {
	Connection string
	Format     string
	Expand     string
	Full       bool
	TimeoutMS  int
	Compact    bool
}

// MakeContext returns a context with an optional timeout.
// The caller must defer cancel() to avoid resource leaks.
func MakeContext(timeoutMs int) (context.Context, context.CancelFunc) {
	ctx := context.Background()
	if timeoutMs > 0 {
		return context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	}
	return ctx, func() {}
}

// WithConnection resolves a connection, runs fn, and handles cleanup. Errors
// from resolution or fn are propagated to the caller (cobra); libcli.Run renders
// them as the family's structured JSON on stderr exactly once. This helper does
// NOT pre-render — doing so under libcli.Run would double-print.
func WithConnection(conn string, timeout int, fn func(ctx context.Context, drv driver.Connection) error) error {
	ctx, cancel := MakeContext(timeout)
	defer cancel()

	drv, err := resolve.Resolve(ctx, resolve.Opts{Connection: conn, Timeout: timeout})
	if err != nil {
		return err
	}
	defer func() { _ = drv.Close() }()

	return fn(ctx, drv)
}
