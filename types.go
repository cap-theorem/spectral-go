package spectral

import "time"

type Registration struct {
	Service    string
	InstanceID string
	Endpoint   string
	TTL        time.Duration
}

type Lease struct {
	Token     string
	TTL       time.Duration
	ExpiresAt time.Time
}

type Provider struct {
	Service    string
	InstanceID string
	Endpoint   string
}

type State string

const (
	StateJoining State = "joining"
	StateActive  State = "active"
	StateLeaving State = "leaving"
	StateLeft    State = "left"
)

type Status struct {
	Ready bool
	State State
}

type registerRequest struct {
	Service    string `json:"service"`
	InstanceID string `json:"instance_id"`
	Endpoint   string `json:"endpoint"`
	TTL        string `json:"ttl"`
}

type leaseResponse struct {
	Token     string    `json:"token"`
	TTL       string    `json:"ttl"`
	ExpiresAt time.Time `json:"expires_at"`
}

type renewRequest struct {
	Token string `json:"token"`
}

type providerResponse struct {
	Service    string `json:"service"`
	InstanceID string `json:"instance_id"`
	Endpoint   string `json:"endpoint"`
}

type statusResponse struct {
	Ready bool   `json:"ready"`
	State string `json:"state"`
}
