package workerprotocol

import "testing"

func TestResponseRequiresIdentityAndConsistentTerminalStatus(t *testing.T) {
	for _, status := range []string{"completed", "failed", "running", "cancelled", ""} {
		for _, message := range []string{"", "failed", " "} {
			response := Response{ProtocolVersion: Version, ExecutionID: "test", Status: status, Error: message}
			valid := status == "completed" && message == "" || status == "failed" && message == "failed"
			if (ValidateResponse(response, "test") == nil) != valid {
				t.Fatalf("unexpected acceptance: %+v", response)
			}
			if ValidateResponse(response, "other") == nil {
				t.Fatal("foreign execution accepted")
			}
			response.ProtocolVersion++
			if ValidateResponse(response, "test") == nil {
				t.Fatal("foreign protocol accepted")
			}
		}
	}
}
