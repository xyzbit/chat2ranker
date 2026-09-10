package contract

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func attachmentForTest(name, mediaType string, data []byte) InputAttachment {
	digest := sha256.Sum256(data)
	return InputAttachment{ID: "sha256:" + hex.EncodeToString(digest[:]), Type: "input_file", Name: name, MediaType: mediaType, Data: data}
}

func TestValidateInputAttachments(t *testing.T) {
	file := attachmentForTest("brief.pdf", "application/pdf", []byte("pdf"))
	image := attachmentForTest("photo.png", "image/png", []byte("png"))
	image.Type = "input_image"
	image.Detail = "auto"
	if err := ValidateInputAttachments([]InputAttachment{file, image}); err != nil {
		t.Fatal(err)
	}
	invalid := image
	invalid.ID = "sha256:" + strings.Repeat("0", 64)
	if err := ValidateInputAttachments([]InputAttachment{invalid}); err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("digest mismatch returned %v", err)
	}
	invalid = image
	invalid.MediaType = "application/pdf"
	if err := ValidateInputAttachments([]InputAttachment{invalid}); err == nil || !strings.Contains(err.Error(), "not an image") {
		t.Fatalf("invalid image media type returned %v", err)
	}
}

func TestInputAttachmentRelativePathSanitizesName(t *testing.T) {
	attachment := attachmentForTest("../季度 报告?.pdf", "application/pdf", []byte("pdf"))
	if got := InputAttachmentRelativePath(1, attachment); got != ".rank-inputs/002-______.pdf" {
		t.Fatalf("unexpected attachment path %q", got)
	}
}
