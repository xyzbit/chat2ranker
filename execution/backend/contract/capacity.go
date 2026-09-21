package contract

import "time"

// Capacity describes durable work for one homogeneous worker configuration.
// It does not measure live workers, CPU usage, or provider rate limits.
type Capacity struct {
	ObservedAt time.Time     `json:"observedAt"`
	Executor   string        `json:"executor"`
	Online     QueueCapacity `json:"online"`
	Evaluation QueueCapacity `json:"evaluation"`
}

// QueueCapacity separates eligible demand from work that more replicas cannot
// unblock. Active and CleanupPending are durable reservations, not heartbeats.
type QueueCapacity struct {
	SlotsPerWorker         int     `json:"slotsPerWorker"`
	Active                 int     `json:"active"`
	Ready                  int     `json:"ready"`
	ResourceBlocked        int     `json:"resourceBlocked"`
	QuotaBlocked           int     `json:"quotaBlocked"`
	CapabilityBlocked      int     `json:"capabilityBlocked"`
	Expired                int     `json:"expired"`
	DeliveryBlocked        int     `json:"deliveryBlocked"`
	CleanupPending         int     `json:"cleanupPending"`
	Demand                 int     `json:"demand"`
	OldestReadyWaitSeconds float64 `json:"oldestReadyWaitSeconds"`
}
