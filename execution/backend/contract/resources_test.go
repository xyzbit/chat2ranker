package contract

import "testing"

func TestResourceQuantitiesPreserveExactAdmissionUnits(t *testing.T) {
	for _, input := range []struct {
		cpu, memory string
		want        RuntimeResources
	}{
		{"0.5", "512Mi", RuntimeResources{500, 512 << 20}},
		{"1500m", "2G", RuntimeResources{1500, 2_000_000_000}},
		{"0.001", "1", RuntimeResources{1, 1}},
	} {
		got, err := ParseRuntimeResources(input.cpu, input.memory)
		if err != nil || got != input.want {
			t.Fatalf("%s/%s: %+v %v", input.cpu, input.memory, got, err)
		}
	}
	for _, input := range [][2]string{{"NaN", "1Gi"}, {"0.0001", "1Gi"}, {"0.5m", "1Gi"}, {"0", "1Gi"}, {"-1", "1Gi"}, {"1", "0"}, {"1", "9Ei"}, {"9223372036854775808m", "1"}, {"1e3", "1"}} {
		if _, err := ParseRuntimeResources(input[0], input[1]); err == nil {
			t.Fatalf("accepted unrepresentable limits: %v", input)
		}
	}
}
