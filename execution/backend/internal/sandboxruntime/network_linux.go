//go:build linux

package sandboxruntime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/xyzbit/agentkiln/execution/backend/contract"
)

// NetworkInit installs default-deny IPv4/IPv6 rules before exposing execd, then
// reexecutes PID 1 without network-administration capabilities. The server must
// give this container its own network namespace and initial CAP_NET_ADMIN.
func NetworkInit(parent context.Context, expires time.Time, maximum time.Duration) error {
	if os.Getpid() != 1 || os.Geteuid() != 0 || time.Until(expires) <= 0 || time.Until(expires) > maximum {
		return errors.New("network init requires root PID 1 and a valid bounded deadline")
	}
	value := os.Getenv("RANK_RUNTIME_EGRESS_POLICY")
	if value == "" {
		return errors.New("sandbox requires an explicit egress policy")
	}
	policy, err := contract.ParseEgressPolicy(value)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithDeadline(parent, expires)
	defer cancel()
	for _, family := range []struct {
		binary string
		ipv6   bool
	}{{"/usr/sbin/iptables-restore", false}, {"/usr/sbin/ip6tables-restore", true}} {
		command := exec.CommandContext(ctx, family.binary, "--wait", "2")
		command.Stdin = strings.NewReader(networkRules(policy, family.ipv6))
		if output, err := command.CombinedOutput(); err != nil {
			return fmt.Errorf("sandbox network policy installation failed: %w: %s", err, output)
		}
	}
	// Exec replaces every Go runtime thread; all descendants inherit the reduced
	// bounding set, including the root daemon's future command-handler threads.
	env := []string{"PATH=/usr/local/bin:/usr/bin:/bin"}
	for _, name := range []string{"EXECD_ACCESS_TOKEN", "RANK_RUNTIME_EXPIRES_AT", "RANK_RUNTIME_MAX_LIFETIME"} {
		env = append(env, name+"="+os.Getenv(name))
	}
	return syscall.Exec("/usr/bin/setpriv", []string{"setpriv", "--bounding-set=-net_admin,-net_raw", "--inh-caps=-all", "--ambient-caps=-all", "/usr/local/bin/execution-sandbox", "-mode", "init"}, env)
}
