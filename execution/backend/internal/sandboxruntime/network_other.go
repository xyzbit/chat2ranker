//go:build !linux

package sandboxruntime

import (
	"context"
	"errors"
	"time"
)

// NetworkInit requires Linux network namespaces and netfilter enforcement.
func NetworkInit(context.Context, time.Time, time.Duration) error {
	return errors.New("sandbox network isolation requires Linux")
}
