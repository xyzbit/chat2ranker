package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDependencyDeadlineIsAnExplicitServiceError(t *testing.T) {
	for _, write := range []func(http.ResponseWriter, error){writeError, writeServingError} {

		response := httptest.NewRecorder()
		write(response, fmt.Errorf("database operation: %w", context.DeadlineExceeded))
		if response.Code != 503 || !strings.Contains(response.Body.String(), `"code":"dependency_timeout"`) {
			t.Fatalf("unexpected deadline response: %d %s", response.Code, response.Body.String())
		}
	}
}
