package domain

import "time"

type WebhookEndpointID string
type WebhookDeliveryID string

const (
	WebhookEventResponseCompleted      = "response.completed"
	WebhookEventResponseFailed         = "response.failed"
	WebhookEventResponseCancelled      = "response.cancelled"
	WebhookEventResponseRequiresAction = "response.requires_action"

	WebhookDeliveryPending    = "pending"
	WebhookDeliveryDelivering = "delivering"
	WebhookDeliveryDelivered  = "delivered"
	WebhookDeliveryFailed     = "failed"
)

// WebhookEndpoint is one tenant-owned HTTPS destination with a unique signing
// secret. SecretRef identifies private storage and is never serialized.
type WebhookEndpoint struct {
	ID            WebhookEndpointID `json:"id"`
	TenantID      string            `json:"tenantId"`
	Name          string            `json:"name"`
	URL           string            `json:"url"`
	SecretRef     string            `json:"-"`
	Events        []string          `json:"events"`
	DeploymentIDs []DeploymentID    `json:"deploymentIds"`
	Enabled       bool              `json:"enabled"`
	CreatedAt     time.Time         `json:"createdAt"`
	UpdatedAt     time.Time         `json:"updatedAt"`
}

// WebhookDelivery is one durable, idempotently identified event delivery.
// Payload is frozen before commit and the delivery ID remains stable on retry.
type WebhookDelivery struct {
	ID            WebhookDeliveryID `json:"id"`
	EndpointID    WebhookEndpointID `json:"endpointId"`
	ResponseID    ResponseID        `json:"responseId"`
	EventType     string            `json:"eventType"`
	Payload       []byte            `json:"-"`
	Status        string            `json:"status"`
	Attempts      int               `json:"attempts"`
	NextAttemptAt time.Time         `json:"nextAttemptAt"`
	LeaseUntil    *time.Time        `json:"leaseUntil,omitempty"`
	LeaseToken    string            `json:"-"`
	LastError     string            `json:"lastError,omitempty"`
	CreatedAt     time.Time         `json:"createdAt"`
	DeliveredAt   *time.Time        `json:"deliveredAt,omitempty"`
}
