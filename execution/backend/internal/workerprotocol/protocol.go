package workerprotocol

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/xyzbit/chat2ranker/execution/backend/contract"
)

const Version = 1

const EventPrefix = "@@chat2ranker-event "

type Request struct {
	ProtocolVersion int                       `json:"protocolVersion"`
	ExecutionID     string                    `json:"executionId"`
	Spec            contract.Spec             `json:"spec"`
	WorkspaceDir    string                    `json:"workspaceDir"`
	ArtifactDir     string                    `json:"artifactDir"`
	HarnessHome     string                    `json:"harnessHome"`
	Metadata        map[string]string         `json:"metadata,omitempty"`
	ModelConnection *contract.ModelConnection `json:"modelConnection,omitempty"`
	Credential      string                    `json:"credential,omitempty"`
}

type Response struct {
	ProtocolVersion int             `json:"protocolVersion"`
	ExecutionID     string          `json:"executionId"`
	Status          string          `json:"status"`
	Result          contract.Result `json:"result"`
	Error           string          `json:"error,omitempty"`
}

type Event struct {
	Type    string          `json:"type"`
	Message string          `json:"message,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
	At      time.Time       `json:"at"`
}

func EncodeEvent(event Event) ([]byte, error) {
	payload, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}
	return append([]byte(EventPrefix), payload...), nil
}

func DecodeEvent(line string) (Event, bool, error) {
	if !strings.HasPrefix(line, EventPrefix) {
		return Event{}, false, nil
	}
	var event Event
	err := json.Unmarshal([]byte(strings.TrimPrefix(line, EventPrefix)), &event)
	return event, true, err
}
