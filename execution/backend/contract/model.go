package contract

import (
	"errors"
	"strings"
)

// ValidateModelSelection rejects unresolved connection models and whitespace
// aliases before queue admission. Connection defaults belong to configuration
// creation; an accepted execution never consults them.
func ValidateModelSelection(spec Spec) error {
	if spec.ModelConnectionID != strings.TrimSpace(spec.ModelConnectionID) || spec.Model != strings.TrimSpace(spec.Model) {
		return errors.New("modelConnectionId and model must not contain surrounding whitespace")
	}
	if spec.ModelConnectionID != "" && spec.Model == "" {
		return errors.New("model is required with modelConnectionId; resolve the connection default before submission")
	}
	return nil
}
