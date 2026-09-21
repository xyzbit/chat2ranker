package sandboxruntime

import (
	"fmt"
	"strings"

	"github.com/xyzbit/agentkiln/execution/backend/contract"
)

// networkRules only accepts a normalized policy; targets and ports are never
// shell text. This container owns the filter table in its private namespace.
func networkRules(policy contract.EgressPolicy, ipv6 bool) string {
	loopback := "127.0.0.1/32"
	if ipv6 {
		loopback = "::1/128"
	}
	var rules strings.Builder
	rules.WriteString("*filter\n:INPUT ACCEPT [0:0]\n:FORWARD DROP [0:0]\n:OUTPUT DROP [0:0]\n-A OUTPUT -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT\n")
	fmt.Fprintf(&rules, "-A OUTPUT -d %s -j ACCEPT\n", loopback)
	for _, rule := range policy.Allow {
		if strings.Contains(rule.CIDR, ":") != ipv6 {
			continue
		}
		fmt.Fprintf(&rules, "-A OUTPUT -p %s -d %s", rule.Protocol, rule.CIDR)
		if rule.Port != 0 {
			fmt.Fprintf(&rules, " --dport %d", rule.Port)
		}
		rules.WriteString(" -j ACCEPT\n")
	}
	rules.WriteString("COMMIT\n")
	return rules.String()
}
