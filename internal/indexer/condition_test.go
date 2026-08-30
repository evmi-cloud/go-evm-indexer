package indexer

import (
	"testing"

	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
)

func TestEvalCondition(t *testing.T) {
	cases := []struct {
		actual, op, expected string
		want                 bool
	}{
		// numeric
		{"100", "gt", "50", true},
		{"100", "gt", "100", false},
		{"100", "gte", "100", true},
		{"10", "lt", "100", true},
		{"100", "eq", "100", true},
		{"100", "neq", "100", false},
		// numeric beats lexical ("100" < "20" as strings, but 100 > 20 numerically)
		{"100", "gt", "20", true},
		// string (addresses), case-insensitive
		{"0xABC", "eq", "0xabc", true},
		{"0xABC", "neq", "0xdef", true},
		{"hello world", "contains", "world", true},
		{"hello", "contains", "xyz", false},
		// unknown operator
		{"1", "??", "1", false},
	}
	for _, c := range cases {
		if got := evalCondition(c.actual, c.op, c.expected); got != c.want {
			t.Errorf("evalCondition(%q,%q,%q) = %v, want %v", c.actual, c.op, c.expected, got, c.want)
		}
	}
}

func TestRuleConditionsPass(t *testing.T) {
	rule := evmi_database.EvmFactoryRule{
		Conditions: []evmi_database.EvmFactoryRuleCondition{
			{Arg: "amount", Operator: "gte", Value: "1000"},
			{Arg: "kind", Operator: "eq", Value: "pool"},
		},
	}
	// All pass.
	if !ruleConditionsPass(rule, map[string]string{"amount": "2000", "kind": "pool"}) {
		t.Error("expected all conditions to pass")
	}
	// One fails (amount too low).
	if ruleConditionsPass(rule, map[string]string{"amount": "500", "kind": "pool"}) {
		t.Error("expected fail on amount")
	}
	// Missing arg → treated as empty → fails eq.
	if ruleConditionsPass(rule, map[string]string{"amount": "2000"}) {
		t.Error("expected fail on missing kind")
	}
	// No conditions → always pass.
	if !ruleConditionsPass(evmi_database.EvmFactoryRule{}, map[string]string{}) {
		t.Error("empty conditions must pass")
	}
}
