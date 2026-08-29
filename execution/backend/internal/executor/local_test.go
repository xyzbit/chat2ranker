package executor

import (
	"slices"
	"testing"
)

func TestWorkerEnvironmentCarriesPackagedDSHPath(t *testing.T) {
	t.Setenv("RANK_DSH_BIN", "/runtime/dsh.js")
	if environment := workerEnvironment("/runtime"); !slices.Contains(environment, "RANK_DSH_BIN=/runtime/dsh.js") {
		t.Fatalf("worker environment omitted packaged DSH path: %#v", environment)
	}
}
