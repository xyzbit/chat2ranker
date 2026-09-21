package contract

// LocalModelCandidate describes a reusable local configuration without credentials.
type LocalModelCandidate struct {
	ID           string `json:"id"`
	Source       string `json:"source"`
	Name         string `json:"name"`
	Protocol     string `json:"protocol"`
	BaseURL      string `json:"baseUrl"`
	Model        string `json:"model"`
	ConnectionID string `json:"connectionId,omitempty"`
}

// LocalModelDiscovery keeps source failures separate from usable candidates.
type LocalModelDiscovery struct {
	Candidates []LocalModelCandidate `json:"candidates"`
	Issues     []LocalModelIssue     `json:"issues"`
}

// LocalModelIssue contains a credential-free explanation for an unavailable source.
type LocalModelIssue struct {
	Source  string `json:"source"`
	Message string `json:"message"`
}
