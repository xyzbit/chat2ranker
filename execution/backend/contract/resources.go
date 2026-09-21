package contract

import (
	"errors"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
)

// RuntimeResources are hard per-container limits, also used for durable admission.
type RuntimeResources struct {
	CPUMillis   int64 `json:"cpuMillis"`
	MemoryBytes int64 `json:"memoryBytes"`
}

// RuntimePlan freezes the service, image and resource allocation before queuing.
// Pool is an operator-owned capacity budget shared by every worker using it.
type RuntimePlan struct {
	Pool      string           `json:"pool"`
	Endpoint  string           `json:"endpoint"`
	Scope     string           `json:"scope"`
	Image     string           `json:"image"`
	Resources RuntimeResources `json:"resources"`
	Egress    EgressPolicy     `json:"egress"`
}

// ParseRuntimeResources accepts positive integer millicores and byte quantities.
// Fractional cores must be representable in millicores; overflow is rejected.
func ParseRuntimeResources(cpu, memory string) (RuntimeResources, error) {
	var result RuntimeResources
	if !regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?m?$`).MatchString(cpu) {
		return result, errors.New("CPU must be positive cores or millicores")
	}
	value, ok := new(big.Rat).SetString(strings.TrimSuffix(cpu, "m"))
	if !ok {
		return result, errors.New("invalid CPU quantity")
	}
	if !strings.HasSuffix(cpu, "m") {
		value.Mul(value, big.NewRat(1000, 1))
	}
	if value.Sign() <= 0 || !value.IsInt() || !value.Num().IsInt64() {
		return result, errors.New("CPU must resolve to positive integer millicores")
	}
	result.CPUMillis = value.Num().Int64()
	parts := regexp.MustCompile(`^([1-9][0-9]*)([KMGTPE]i|[kMGTPE])?$`).FindStringSubmatch(memory)
	if parts == nil {
		return result, errors.New("memory must be positive bytes or a Ki/Mi/Gi-style quantity")
	}
	count, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return result, errors.New("memory quantity overflows int64")
	}
	factor := int64(1)
	if parts[2] != "" {
		base := int64(1000)
		unit := parts[2]
		if strings.HasSuffix(unit, "i") {
			base = 1024
			unit = strings.TrimSuffix(unit, "i")
		}
		power := strings.Index("KMGTPE", strings.ToUpper(unit)) + 1
		for range power {
			if factor > math.MaxInt64/base {
				return result, errors.New("memory quantity overflows int64")
			}
			factor *= base
		}
	}
	if count > math.MaxInt64/factor {
		return result, errors.New("memory quantity overflows int64")
	}
	result.MemoryBytes = count * factor
	return result, nil
}
