package contract

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"mime"
	"path"
	"strings"
	"unicode"
)

// Input attachment limits bound Execution request and worker disk usage.
const (
	MaxInputAttachments     = 8
	MaxInputAttachmentBytes = 20 << 20
)

// ValidateInputAttachments verifies content identity and the bounded wire
// payload before an Execution is persisted or started.
func ValidateInputAttachments(attachments []InputAttachment) error {
	if len(attachments) > MaxInputAttachments {
		return fmt.Errorf("input attachments exceed the %d item limit", MaxInputAttachments)
	}
	total := 0
	for index, attachment := range attachments {
		if attachment.Type != "input_image" && attachment.Type != "input_file" {
			return fmt.Errorf("input attachment %d has unsupported type %q", index, attachment.Type)
		}
		if strings.TrimSpace(attachment.Name) == "" || len(attachment.Name) > 128 {
			return fmt.Errorf("input attachment %d requires a name of at most 128 bytes", index)
		}
		mediaType, _, err := mime.ParseMediaType(attachment.MediaType)
		if err != nil || mediaType == "" {
			return fmt.Errorf("input attachment %d has invalid media type", index)
		}
		if attachment.Type == "input_image" && !strings.HasPrefix(mediaType, "image/") {
			return fmt.Errorf("input attachment %d is not an image", index)
		}
		if attachment.Detail != "" && attachment.Detail != "auto" {
			return fmt.Errorf("input attachment %d supports only detail=auto", index)
		}
		if len(attachment.Data) == 0 {
			return fmt.Errorf("input attachment %d is empty", index)
		}
		total += len(attachment.Data)
		if total > MaxInputAttachmentBytes {
			return fmt.Errorf("input attachments exceed %d bytes", MaxInputAttachmentBytes)
		}
		digest := sha256.Sum256(attachment.Data)
		if attachment.ID != "sha256:"+hex.EncodeToString(digest[:]) {
			return fmt.Errorf("input attachment %d content digest does not match", index)
		}
	}
	return nil
}

// InputAttachmentRelativePath returns the worker-local path shared by the
// materializer and harness prompt renderer.
func InputAttachmentRelativePath(index int, attachment InputAttachment) string {
	name := path.Base(strings.ReplaceAll(attachment.Name, "\\", "/"))
	name = strings.Map(func(value rune) rune {
		if value > unicode.MaxASCII || !(unicode.IsLetter(value) || unicode.IsDigit(value) || strings.ContainsRune("._-", value)) {
			return '_'
		}
		return value
	}, name)
	name = strings.Trim(name, ".")
	if name == "" {
		name = "input"
	}
	if len(name) > 80 {
		name = name[:80]
	}
	return path.Join(".rank-inputs", fmt.Sprintf("%03d-%s", index+1, name))
}
