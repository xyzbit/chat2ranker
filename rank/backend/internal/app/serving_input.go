package app

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/xyzbit/chat2ranker/execution/backend/contract"
	"github.com/xyzbit/chat2ranker/rank/backend/internal/domain"
)

// ResponseInputFetcher freezes one remote input without granting access to
// private, loopback, link-local, or non-HTTPS destinations.
type ResponseInputFetcher interface {
	Fetch(context.Context, string, int64) ([]byte, string, error)
}

type secureResponseInputFetcher struct{ client *http.Client }

type normalizedServingInput struct {
	Prompt     string
	Items      []domain.ResponseInputItem
	remoteURLs map[int]string
}

func newSecureResponseInputFetcher() ResponseInputFetcher {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			if len(addresses) == 0 {
				return nil, errors.New("input host has no addresses")
			}
			for _, address := range addresses {
				if !publicResponseInputIP(address.IP) {
					return nil, errors.New("input URL resolves to a non-public address")
				}
			}
			var lastErr error
			for _, address := range addresses {
				connection, err := dialer.DialContext(ctx, network, net.JoinHostPort(address.IP.String(), port))
				if err == nil {
					return connection, nil
				}
				lastErr = err
			}
			return nil, lastErr
		},
	}
	return &secureResponseInputFetcher{client: &http.Client{
		Transport: transport,
		Timeout:   15 * time.Second,
		CheckRedirect: func(request *http.Request, prior []*http.Request) error {
			if len(prior) >= 3 {
				return errors.New("input URL has too many redirects")
			}
			return validateRemoteResponseInputURL(request.URL)
		},
	}}
}

func publicResponseInputIP(ip net.IP) bool {
	return ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast()
}

func validateRemoteResponseInputURL(value *url.URL) error {
	if value == nil || value.Scheme != "https" || value.Hostname() == "" || value.User != nil {
		return errors.New("remote inputs require an HTTPS URL without user credentials")
	}
	return nil
}

func (fetcher *secureResponseInputFetcher) Fetch(ctx context.Context, source string, limit int64) ([]byte, string, error) {
	parsed, err := url.Parse(source)
	if err != nil || validateRemoteResponseInputURL(parsed) != nil {
		return nil, "", errors.New("remote inputs require a valid HTTPS URL")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, "", err
	}
	request.Header.Set("Accept", "image/*, application/pdf, text/*, application/json, application/octet-stream")
	response, err := fetcher.client.Do(request)
	if err != nil {
		return nil, "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, "", fmt.Errorf("input URL returned HTTP %d", response.StatusCode)
	}
	if response.ContentLength > limit {
		return nil, "", errors.New("input URL content exceeds the remaining attachment limit")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(data)) > limit {
		return nil, "", errors.New("input URL content exceeds the remaining attachment limit")
	}
	mediaType, _, _ := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if mediaType == "" {
		mediaType = http.DetectContentType(data)
	}
	return data, mediaType, nil
}

func normalizeServingInput(raw json.RawMessage) (normalizedServingInput, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return normalizedServingInput{}, problem(400, "input_required", "input is required")
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		text = strings.TrimSpace(text)
		if text == "" {
			return normalizedServingInput{}, problem(400, "input_required", "input must not be empty")
		}
		return normalizedServingInput{Prompt: text, Items: []domain.ResponseInputItem{{Type: "input_text", Text: text}}}, nil
	}
	var rawMessages []json.RawMessage
	if err := json.Unmarshal(raw, &rawMessages); err != nil || len(rawMessages) == 0 {
		return normalizedServingInput{}, problem(400, "unsupported_input", "input must be text or an array of user messages")
	}
	result := normalizedServingInput{remoteURLs: map[int]string{}}
	textParts := []string{}
	for _, rawMessage := range rawMessages {
		var discriminator struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(rawMessage, &discriminator)
		if discriminator.Type == "function_call_output" {
			var output struct {
				Type   string `json:"type"`
				CallID string `json:"call_id"`
				Output string `json:"output"`
			}
			if err := decodeServingJSON(rawMessage, &output); err != nil || strings.TrimSpace(output.CallID) == "" || strings.TrimSpace(output.Output) == "" {
				return normalizedServingInput{}, problem(400, "invalid_function_call_output", "function_call_output requires call_id and non-empty output")
			}
			output.CallID, output.Output = strings.TrimSpace(output.CallID), strings.TrimSpace(output.Output)
			result.Items = append(result.Items, domain.ResponseInputItem{Type: output.Type, CallID: output.CallID, Output: output.Output})
			textParts = append(textParts, "Function result "+output.CallID+":\n"+output.Output)
			continue
		}
		var message struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		}
		if err := decodeServingJSON(rawMessage, &message); err != nil || message.Role != "user" {
			return normalizedServingInput{}, problem(400, "unsupported_input_role", "only user input messages are supported")
		}
		var contentText string
		if json.Unmarshal(message.Content, &contentText) == nil {
			contentText = strings.TrimSpace(contentText)
			if contentText == "" {
				return normalizedServingInput{}, problem(400, "input_required", "message content must not be empty")
			}
			result.Items = append(result.Items, domain.ResponseInputItem{Type: "input_text", Text: contentText})
			textParts = append(textParts, contentText)
			continue
		}
		var rawContent []json.RawMessage
		if err := json.Unmarshal(message.Content, &rawContent); err != nil || len(rawContent) == 0 {
			return normalizedServingInput{}, problem(400, "unsupported_input_content", "message content must contain input items")
		}
		for _, rawItem := range rawContent {
			item, remoteURL, err := normalizeServingInputItem(rawItem, len(result.Items))
			if err != nil {
				return normalizedServingInput{}, err
			}
			result.Items = append(result.Items, item)
			if remoteURL != "" {
				result.remoteURLs[len(result.Items)-1] = remoteURL
			}
			if item.Type == "input_text" {
				textParts = append(textParts, item.Text)
			}
		}
	}
	if len(result.Items) == 0 {
		return normalizedServingInput{}, problem(400, "input_required", "input must not be empty")
	}
	if attachmentCount(result.Items) > contract.MaxInputAttachments {
		return normalizedServingInput{}, problem(400, "too_many_input_attachments", fmt.Sprintf("input accepts at most %d images and files", contract.MaxInputAttachments))
	}
	result.Prompt = strings.TrimSpace(strings.Join(textParts, "\n"))
	if result.Prompt == "" {
		result.Prompt = "Inspect the supplied input files and respond to the user."
	}
	return result, nil
}

func normalizeServingInputItem(raw json.RawMessage, ordinal int) (domain.ResponseInputItem, string, error) {
	var input struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		ImageURL string `json:"image_url"`
		FileURL  string `json:"file_url"`
		FileData string `json:"file_data"`
		FileID   string `json:"file_id"`
		Filename string `json:"filename"`
		Detail   string `json:"detail"`
	}
	if err := decodeServingJSON(raw, &input); err != nil {
		return domain.ResponseInputItem{}, "", problem(400, "unsupported_input_content", "input content contains invalid or unsupported fields")
	}
	input.Type, input.Text, input.ImageURL = strings.TrimSpace(input.Type), strings.TrimSpace(input.Text), strings.TrimSpace(input.ImageURL)
	input.FileURL, input.FileData, input.FileID = strings.TrimSpace(input.FileURL), strings.TrimSpace(input.FileData), strings.TrimSpace(input.FileID)
	input.Filename, input.Detail = strings.TrimSpace(input.Filename), strings.TrimSpace(input.Detail)
	switch input.Type {
	case "input_text":
		if input.Text == "" || input.ImageURL != "" || input.FileURL != "" || input.FileData != "" || input.FileID != "" || input.Filename != "" || input.Detail != "" {
			return domain.ResponseInputItem{}, "", problem(400, "unsupported_input_content", "input_text accepts only non-empty text")
		}
		return domain.ResponseInputItem{Type: input.Type, Text: input.Text}, "", nil
	case "input_image":
		if input.ImageURL == "" || input.FileURL != "" || input.FileData != "" || input.FileID != "" || input.Filename != "" || input.Text != "" {
			return domain.ResponseInputItem{}, "", problem(400, "unsupported_input_image", "input_image requires image_url; file_id is not supported")
		}
		if input.Detail == "" {
			input.Detail = "auto"
		}
		if input.Detail != "auto" {
			return domain.ResponseInputItem{}, "", problem(400, "unsupported_image_detail", "input_image supports only detail=auto")
		}
		return normalizeInputSource(input.Type, input.ImageURL, fmt.Sprintf("image-%d", ordinal+1), input.Detail)
	case "input_file":
		if input.FileID != "" {
			return domain.ResponseInputItem{}, "", problem(400, "unsupported_file_id", "input_file file_id requires a Files API and is not supported")
		}
		if input.Text != "" || input.ImageURL != "" || (input.FileURL == "") == (input.FileData == "") || input.Detail != "" {
			return domain.ResponseInputItem{}, "", problem(400, "unsupported_input_file", "input_file requires exactly one of file_url or file_data")
		}
		if input.FileData != "" {
			if input.Filename == "" {
				return domain.ResponseInputItem{}, "", problem(400, "filename_required", "input_file file_data requires filename")
			}
			return normalizeInlineInput(input.Type, input.FileData, input.Filename, "")
		}
		name := input.Filename
		if name == "" {
			name = remoteInputName(input.FileURL, fmt.Sprintf("file-%d", ordinal+1))
		}
		return normalizeRemoteInput(input.Type, input.FileURL, name, "")
	default:
		return domain.ResponseInputItem{}, "", problem(400, "unsupported_input_content", "input content type must be input_text, input_image, or input_file")
	}
}

func normalizeInputSource(kind, source, fallbackName, detail string) (domain.ResponseInputItem, string, error) {
	if strings.HasPrefix(source, "data:") {
		return normalizeInlineInput(kind, source, fallbackName, detail)
	}
	name := remoteInputName(source, fallbackName)
	return normalizeRemoteInput(kind, source, name, detail)
}

func normalizeInlineInput(kind, source, name, detail string) (domain.ResponseInputItem, string, error) {
	data, mediaType, err := decodeInlineResponseInput(source, name)
	if err != nil {
		return domain.ResponseInputItem{}, "", problem(400, "invalid_input_data", err.Error())
	}
	if kind == "input_image" && !strings.HasPrefix(mediaType, "image/") {
		return domain.ResponseInputItem{}, "", problem(400, "invalid_input_image", "input_image data must use an image media type")
	}
	if len(data) > contract.MaxInputAttachmentBytes {
		return domain.ResponseInputItem{}, "", problem(400, "input_too_large", "input attachments exceed the supported size")
	}
	digest := contentDigest(data)
	if path.Ext(name) == "" {
		name += extensionForMediaType(mediaType)
	}
	return domain.ResponseInputItem{Type: kind, Name: safeInputName(name), MediaType: mediaType, Detail: detail, SourceDigest: digest, ContentDigest: digest, Data: data}, "", nil
}

func normalizeRemoteInput(kind, source, name, detail string) (domain.ResponseInputItem, string, error) {
	parsed, err := url.Parse(source)
	if err != nil || validateRemoteResponseInputURL(parsed) != nil {
		return domain.ResponseInputItem{}, "", problem(400, "invalid_input_url", "remote inputs require a valid HTTPS URL")
	}
	return domain.ResponseInputItem{Type: kind, Name: safeInputName(name), Detail: detail, SourceDigest: stringDigest(parsed.String())}, parsed.String(), nil
}

func decodeInlineResponseInput(source, name string) ([]byte, string, error) {
	mediaType := mime.TypeByExtension(strings.ToLower(path.Ext(name)))
	payload := source
	if strings.HasPrefix(source, "data:") {
		header, encoded, ok := strings.Cut(strings.TrimPrefix(source, "data:"), ",")
		if !ok || !strings.HasSuffix(strings.ToLower(header), ";base64") {
			return nil, "", errors.New("data URLs must use base64 encoding")
		}
		mediaType = strings.TrimSuffix(header, ";base64")
		if parsed, _, err := mime.ParseMediaType(mediaType); err == nil {
			mediaType = parsed
		} else {
			return nil, "", errors.New("data URL has an invalid media type")
		}
		payload = encoded
	}
	payload = strings.Map(func(value rune) rune {
		if value == ' ' || value == '\n' || value == '\r' || value == '\t' {
			return -1
		}
		return value
	}, payload)
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		data, err = base64.RawStdEncoding.DecodeString(payload)
	}
	if err != nil || len(data) == 0 {
		return nil, "", errors.New("input data must be non-empty base64")
	}
	if mediaType == "" {
		mediaType = http.DetectContentType(data)
	}
	return data, mediaType, nil
}

func (s *Service) freezeServingInput(ctx context.Context, input normalizedServingInput) (normalizedServingInput, error) {
	total := 0
	for index := range input.Items {
		item := &input.Items[index]
		if item.Type == "input_text" {
			continue
		}
		if source := input.remoteURLs[index]; source != "" {
			remaining := int64(contract.MaxInputAttachmentBytes - total)
			data, mediaType, err := s.inputFetcher.Fetch(ctx, source, remaining)
			if err != nil {
				return normalizedServingInput{}, problem(400, "input_fetch_failed", err.Error())
			}
			if item.Type == "input_image" && !strings.HasPrefix(mediaType, "image/") {
				return normalizedServingInput{}, problem(400, "invalid_input_image", "input_image URL did not return an image media type")
			}
			item.Data, item.MediaType, item.ContentDigest = data, mediaType, contentDigest(data)
			if path.Ext(item.Name) == "" {
				item.Name = safeInputName(item.Name + extensionForMediaType(mediaType))
			}
		}
		total += len(item.Data)
	}
	attachments := servingInputAttachments(input.Items)
	if err := contract.ValidateInputAttachments(attachments); err != nil {
		return normalizedServingInput{}, problem(400, "invalid_input_attachments", err.Error())
	}
	return input, nil
}

func servingInputAttachments(items []domain.ResponseInputItem) []contract.InputAttachment {
	attachments := []contract.InputAttachment{}
	for _, item := range items {
		if item.Type != "input_image" && item.Type != "input_file" {
			continue
		}
		attachments = append(attachments, contract.InputAttachment{ID: item.ContentDigest, Type: item.Type, Name: item.Name, MediaType: item.MediaType, Detail: item.Detail, Data: item.Data})
	}
	return attachments
}

func servingInputDigestItems(items []domain.ResponseInputItem) []domain.ResponseInputItem {
	result := make([]domain.ResponseInputItem, len(items))
	for index, item := range items {
		item.Data, item.ContentDigest = nil, ""
		result[index] = item
	}
	return result
}

func attachmentCount(items []domain.ResponseInputItem) int {
	count := 0
	for _, item := range items {
		if item.Type == "input_image" || item.Type == "input_file" {
			count++
		}
	}
	return count
}

func contentDigest(data []byte) string {
	digest := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func stringDigest(value string) string { return contentDigest([]byte(value)) }

func remoteInputName(source, fallback string) string {
	parsed, err := url.Parse(source)
	if err == nil && path.Base(parsed.Path) != "." && path.Base(parsed.Path) != "/" && path.Base(parsed.Path) != "" {
		return path.Base(parsed.Path)
	}
	return fallback
}

func safeInputName(value string) string {
	value = path.Base(strings.ReplaceAll(value, "\\", "/"))
	if value == "." || value == "/" || value == "" {
		return "input"
	}
	runes := []rune(value)
	for len(runes) > 0 && len(string(runes)) > 128 {
		runes = runes[:len(runes)-1]
	}
	return string(runes)
}

func extensionForMediaType(mediaType string) string {
	extensions, _ := mime.ExtensionsByType(mediaType)
	if len(extensions) > 0 {
		return extensions[0]
	}
	return ""
}

var _ ResponseInputFetcher = (*secureResponseInputFetcher)(nil)
