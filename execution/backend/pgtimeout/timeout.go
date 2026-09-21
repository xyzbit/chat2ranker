// Package pgtimeout bounds PostgreSQL pool acquisition and query I/O without
// imposing a lifetime on the calling HTTP stream, execution, or worker.
package pgtimeout

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Configure installs deadlines on a new, otherwise untraced pool configuration.
// Acquisition and query I/O each receive their own budget, capped by the caller's
// deadline. Query cancellation remains live until rows close. Transactions need
// a separate caller deadline to bound their total duration across statements.
func Configure(config *pgxpool.Config, timeout time.Duration) error {
	if timeout <= 0 {
		return errors.New("PostgreSQL operation timeout must be positive")
	}
	if config.ConnConfig.Tracer != nil {
		return errors.New("PostgreSQL timeout configuration requires an untraced pool")
	}
	config.ConnConfig.ConnectTimeout = timeout
	config.ConnConfig.Tracer = deadlines{timeout: timeout}
	return nil
}

type deadlines struct{ timeout time.Duration }
type cancelKey struct{}

func (d deadlines) start(ctx context.Context) context.Context {
	ctx, cancel := context.WithTimeout(ctx, d.timeout)
	return context.WithValue(ctx, cancelKey{}, cancel)
}

func finish(ctx context.Context) { ctx.Value(cancelKey{}).(context.CancelFunc)() }

func (d deadlines) TraceAcquireStart(ctx context.Context, _ *pgxpool.Pool, _ pgxpool.TraceAcquireStartData) context.Context {
	return d.start(ctx)
}

func (d deadlines) TraceAcquireEnd(ctx context.Context, _ *pgxpool.Pool, _ pgxpool.TraceAcquireEndData) {
	finish(ctx)
}

func (d deadlines) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	return d.start(ctx)
}

func (d deadlines) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	finish(ctx)
}
