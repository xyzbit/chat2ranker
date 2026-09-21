package contract

import (
	"cmp"
	"encoding/json"
	"errors"
	"io"
	"net/netip"
	"slices"
	"strings"
)

// EgressRule permits a transport destination. Port zero explicitly permits all
// ports of Protocol at CIDR; domain matching and DNS resolution are not implied.
type EgressRule struct {
	Protocol string `json:"protocol"`
	CIDR     string `json:"cidr"`
	Port     uint16 `json:"port"`
}

// EgressPolicy denies new outbound connections except Allow and local loopback.
// Replies to accepted connections remain allowed. Empty Allow denies all egress.
type EgressPolicy struct {
	Allow []EgressRule `json:"allow"`
}

// Normalize validates wire/config policy and returns an owned, canonical copy.
func (p EgressPolicy) Normalize() (EgressPolicy, error) {
	rules := slices.Clone(p.Allow)
	for i, rule := range rules {
		prefix, err := netip.ParsePrefix(rule.CIDR)
		if err != nil || prefix.Addr().Is4In6() || (rule.Protocol != "tcp" && rule.Protocol != "udp") {
			return EgressPolicy{}, errors.New("egress requires an IPv4/IPv6 CIDR and tcp or udp protocol")
		}
		rules[i].CIDR = prefix.Masked().String()
	}
	slices.SortFunc(rules, func(a, b EgressRule) int {
		if n := cmp.Compare(a.CIDR, b.CIDR); n != 0 {
			return n
		}
		if n := cmp.Compare(a.Protocol, b.Protocol); n != 0 {
			return n
		}
		return cmp.Compare(a.Port, b.Port)
	})
	return EgressPolicy{Allow: slices.Compact(rules)}, nil
}

// ParseEgressPolicy rejects malformed or unknown JSON fields. Empty configuration
// selects deny-all; a runtime must receive the resulting explicit JSON object.
func ParseEgressPolicy(value string) (EgressPolicy, error) {
	if value == "" {
		return EgressPolicy{}, nil
	}
	decoder := json.NewDecoder(strings.NewReader(value))
	decoder.DisallowUnknownFields()
	var policy *EgressPolicy
	if err := decoder.Decode(&policy); err != nil || policy == nil {
		return EgressPolicy{}, errors.New("egress policy must be a JSON object with an allow list")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return EgressPolicy{}, errors.New("egress policy contains trailing JSON")
	}
	return policy.Normalize()
}
