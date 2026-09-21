package app

import (
	"bytes"
	"encoding/json"
	"math/big"
	"reflect"
	"strings"

	"github.com/xyzbit/agentkiln/execution/backend/contract"
)

// replay compares JSON values, including raw schemas and tool arguments whose
// formatting PostgreSQL jsonb can change. Arrays remain ordered and numbers exact.
func replay(existing contract.Execution, requested contract.Spec) (contract.Execution, error) {
	left, err := specIdentity(existing.Spec)
	if err != nil {
		return contract.Execution{}, err
	}
	right, err := specIdentity(requested)
	if err != nil {
		return contract.Execution{}, err
	}
	if !reflect.DeepEqual(left, right) {
		return contract.Execution{}, problem(409, "idempotency_conflict", "idempotencyKey was already used with another spec")
	}
	return existing, nil
}

func specIdentity(spec contract.Spec) (any, error) {
	encoded, err := json.Marshal(spec)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return normalizeJSONNumbers(value), nil
}

func normalizeJSONNumbers(value any) any {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			v[key] = normalizeJSONNumbers(child)
		}
	case []any:
		for index, child := range v {
			v[index] = normalizeJSONNumbers(child)
		}
	case json.Number:
		coefficient, exponent, _ := strings.Cut(strings.ToLower(string(v)), "e")
		sign := ""
		if strings.HasPrefix(coefficient, "-") {
			sign, coefficient = "-", coefficient[1:]
		}
		integer, fraction, _ := strings.Cut(coefficient, ".")
		digits := strings.TrimLeft(integer+fraction, "0")
		if digits == "" {
			return json.Number("0")
		}
		trimmed := strings.TrimRight(digits, "0")
		// Adjust only the exponent; expanding 1e100000000 would allocate memory
		// proportional to its value rather than the bounded JSON input size.
		power := new(big.Int)
		if exponent != "" {
			power.SetString(exponent, 10)
		}
		power.Add(power, big.NewInt(int64(len(digits)-len(trimmed)-len(fraction))))
		return json.Number(sign + trimmed + "e" + power.String())
	}
	return value
}
