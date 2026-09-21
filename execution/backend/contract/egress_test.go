package contract

import (
	"reflect"
	"testing"
)

func TestEgressPolicyRejectsAmbiguousConfiguration(t *testing.T) {
	for _, value := range []string{
		`null`, `{"defaultAction":"allow"}`, `{} {}`, `{"allow":[{"protocol":"tcp","cidr":"example.com","port":443}]}`,
		`{"allow":[{"protocol":"tcp\nCOMMIT","cidr":"10.0.0.0/8","port":443}]}`,
		`{"allow":[{"protocol":"tcp","cidr":"10.0.0.0/8","port":65536}]}`,
		`{"allow":[{"protocol":"tcp","cidr":"::ffff:10.0.0.1/128","port":443}]}`,
		`{"allow":[{"protocol":"udp","cidr":"10.0.0.0/8","ports":53}]}`,
	} {
		if _, err := ParseEgressPolicy(value); err == nil {
			t.Errorf("invalid egress configuration accepted: %s", value)
		}
	}
	for _, value := range []string{"", `{}`, `{"allow":[]}`} {
		policy, err := ParseEgressPolicy(value)
		if err != nil || len(policy.Allow) != 0 {
			t.Fatalf("empty policy did not select deny-all: %+v %v", policy, err)
		}
	}
}

func TestEgressPolicyCanonicalizesWithoutBorrowingCallerRules(t *testing.T) {
	source := EgressPolicy{Allow: []EgressRule{{"tcp", "2001:db8::123/64", 443}, {"tcp", "10.0.0.9/24", 8800}, {"tcp", "10.0.0.0/24", 8800}}}
	normalized, err := source.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	want := EgressPolicy{Allow: []EgressRule{{"tcp", "10.0.0.0/24", 8800}, {"tcp", "2001:db8::/64", 443}}}
	if !reflect.DeepEqual(normalized, want) || source.Allow[0].CIDR != "2001:db8::123/64" {
		t.Fatalf("policy did not canonicalize independently: %+v", normalized)
	}
	source.Allow[0].CIDR = "::/0"
	if !reflect.DeepEqual(normalized, want) {
		t.Fatal("caller mutation changed normalized policy")
	}
}
