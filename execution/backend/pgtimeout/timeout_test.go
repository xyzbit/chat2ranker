package pgtimeout_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/xyzbit/agentkiln/execution/backend/pgtimeout"
)

func pool(t *testing.T, mutate func(*pgxpool.Config)) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("EXECUTION_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("EXECUTION_TEST_DATABASE_URL is required")
	}
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	config.MaxConns = 1
	if err := pgtimeout.Configure(config, 250*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if mutate != nil {
		mutate(config)
	}
	p, err := pgxpool.NewWithConfig(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := p.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	return p
}

func bounded(t *testing.T, operation func(context.Context) error) {
	t.Helper()
	// This outer limit is only a watchdog: a caller without a short deadline must
	// receive a driver timeout substantially before it expires.
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	start := time.Now()
	err := operation(ctx)
	if !errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil || time.Since(start) > time.Second {
		t.Fatalf("unbounded database operation: elapsed=%s err=%v parent=%v", time.Since(start), err, ctx.Err())
	}
}

func TestPoolWaitAndQueryTimeoutLeaveCallerUsable(t *testing.T) {
	p := pool(t, nil)
	conn, err := p.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	bounded(t, func(ctx context.Context) error {
		_, err := p.Exec(ctx, "SELECT 1")
		return err
	})
	conn.Release()
	bounded(t, func(ctx context.Context) error {
		_, err := p.Exec(ctx, "SELECT pg_sleep(10)")
		return err
	})
	var value int
	if err := p.QueryRow(t.Context(), "SELECT 42").Scan(&value); err != nil || value != 42 {
		t.Fatalf("pool did not recover: %d %v", value, err)
	}
}

func TestShorterCallerDeadlineIsPreserved(t *testing.T) {
	p := pool(t, nil)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	_, err := p.Exec(ctx, "SELECT pg_sleep(10)")
	if !errors.Is(err, context.DeadlineExceeded) || ctx.Err() == nil {
		t.Fatalf("caller cancellation lost: %v", err)
	}
}

func TestEstablishedConnectionBlackholeReturnsBeforeCallerDeadline(t *testing.T) {
	var stall atomic.Bool
	p := pool(t, func(config *pgxpool.Config) {
		// Keep real PostgreSQL authentication and protocol traffic. Once enabled,
		// consume bytes without forwarding them, leaving both TCP sockets open.
		config.ConnConfig.DialFunc = func(ctx context.Context, network, address string) (net.Conn, error) {
			upstream, err := (&net.Dialer{}).DialContext(ctx, network, address)
			if err != nil {
				return nil, err
			}
			client, proxy := net.Pipe()
			var workers sync.WaitGroup
			workers.Add(2)
			pump := func(to, from net.Conn) {
				defer workers.Done()
				defer to.Close()
				defer from.Close()
				buf := make([]byte, 32768)
				for {
					n, err := from.Read(buf)
					if n > 0 && !stall.Load() {
						if _, err := io.CopyN(to, bytes.NewReader(buf[:n]), int64(n)); err != nil {
							return
						}
					}
					if err != nil {
						return
					}
				}
			}
			go pump(upstream, proxy)
			go pump(proxy, upstream)
			t.Cleanup(func() { client.Close(); proxy.Close(); upstream.Close(); workers.Wait() })
			return client, nil
		}
	})
	stall.Store(true)
	bounded(t, func(ctx context.Context) error {
		_, err := p.Exec(ctx, "SELECT 1")
		return err
	})
}
