package domain

import (
	"strings"
	"time"
)

// Webhook auth modes for inbound project channels.
const (
	WebhookAuthHMAC        = "hmac"
	WebhookAuthToken       = "token"
	WebhookAuthHMACOrToken = "hmac_or_token"
)

// WebhookEvent statuses recorded for operators (no secrets).
const (
	WebhookEventReceived  = "received"
	WebhookEventAccepted  = "accepted"
	WebhookEventRejected  = "rejected"
	WebhookEventDispatched = "dispatched"
	WebhookEventDispatchFailed = "dispatch_failed"
)

// DefaultWebhookBodyLimit is the default max inbound JSON body size (256 KiB).
const DefaultWebhookBodyLimit = 256 << 10

// MaxWebhookBodyLimit is the hard ceiling for configurable body size (1 MiB).
const MaxWebhookBodyLimit = 1 << 20

// WebhookSecretGrace is how long a rotated-out secret remains valid.
const WebhookSecretGrace = 15 * time.Minute

// WebhookTimestampSkew is the allowed clock skew for anti-replay timestamps.
const WebhookTimestampSkew = 5 * time.Minute

// WebhookNonceTTL is how long a used nonce is remembered.
const WebhookNonceTTL = 10 * time.Minute

// WebhookIdempotencyTTL is how long an idempotency key maps to the same issue.
const WebhookIdempotencyTTL = 24 * time.Hour

// ProjectWebhookChannel is one inbound webhook endpoint bound to a project.
type ProjectWebhookChannel struct {
	ID                   string    `json:"id"`
	ProjectID            string    `json:"projectId"`
	Name                 string    `json:"name"`
	Description          string    `json:"description,omitempty"`
	Enabled              bool      `json:"enabled"`
	DeliveryTokenPrefix  string    `json:"deliveryTokenPrefix"`
	AuthMode             string    `json:"authMode"` // hmac (default), token, hmac_or_token
	DefaultWorkflowID    string    `json:"defaultWorkflowId,omitempty"`
	AssigneeUserID       int64     `json:"assigneeUserId,omitempty"`
	TitleTemplate        string    `json:"titleTemplate,omitempty"`
	DescriptionTemplate  string    `json:"descriptionTemplate,omitempty"`
	AutoDispatch         bool      `json:"autoDispatch"`
	MaxBodyBytes         int       `json:"maxBodyBytes,omitempty"`
	SecretPrefix         string    `json:"secretPrefix,omitempty"`
	SecretVersion        int       `json:"secretVersion,omitempty"`
	HasPreviousSecret    bool      `json:"hasPreviousSecret,omitempty"`
	PreviousSecretUntil  *time.Time `json:"previousSecretUntil,omitempty"`
	CreatedAt            time.Time `json:"createdAt"`
	UpdatedAt            time.Time `json:"updatedAt"`
	// InboundPath is the public path (no host); filled by API for UI copy.
	InboundPath string `json:"inboundPath,omitempty"`
	// InboundURL is absolute when a public base URL is known.
	InboundURL string `json:"inboundURL,omitempty"`
}

// NormalizeAuthMode returns a supported auth mode (default hmac).
func NormalizeWebhookAuthMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case WebhookAuthToken:
		return WebhookAuthToken
	case WebhookAuthHMACOrToken, "either", "any":
		return WebhookAuthHMACOrToken
	default:
		return WebhookAuthHMAC
	}
}

// EffectiveMaxBodyBytes returns the channel body limit with safe defaults/ceiling.
func (c ProjectWebhookChannel) EffectiveMaxBodyBytes() int {
	n := c.MaxBodyBytes
	if n <= 0 {
		return DefaultWebhookBodyLimit
	}
	if n > MaxWebhookBodyLimit {
		return MaxWebhookBodyLimit
	}
	return n
}

// ProjectWebhookSecretVersion is one hashed secret revision for a channel.
type ProjectWebhookSecretVersion struct {
	ID           string     `json:"id"`
	ChannelID    string     `json:"channelId"`
	Version      int        `json:"version"`
	SecretPrefix string     `json:"secretPrefix"`
	CreatedAt    time.Time  `json:"createdAt"`
	GraceUntil   *time.Time `json:"graceUntil,omitempty"`
}

// ProjectWebhookEvent is an audit row for one inbound attempt.
type ProjectWebhookEvent struct {
	ID               string    `json:"id"`
	ChannelID        string    `json:"channelId"`
	ProjectID        string    `json:"projectId"`
	Status           string    `json:"status"`
	Reason           string    `json:"reason,omitempty"`
	IssueID          string    `json:"issueId,omitempty"`
	IssueNumber      int       `json:"issueNumber,omitempty"`
	TaskID           string    `json:"taskId,omitempty"`
	RunID            string    `json:"runId,omitempty"`
	IdempotencyKeyID string    `json:"idempotencyKeyId,omitempty"` // truncated/hash id only
	ClientIP         string    `json:"clientIp,omitempty"`
	PayloadBytes     int       `json:"payloadBytes,omitempty"`
	ReceivedAt       time.Time `json:"receivedAt"`
}

// WebhookInboundContext is the stable SOP input exposed to agents/scripts.
// Secrets and raw signatures are never included.
type WebhookInboundContext struct {
	ChannelID string         `json:"channelId"`
	EventID   string         `json:"eventId"`
	Payload   map[string]any `json:"payload,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
}

// WebhookNodeTypes are optional SOP trigger markers (no-op at runtime).
func IsWebhookTriggerNode(nodeType string) bool {
	switch strings.ToLower(strings.TrimSpace(nodeType)) {
	case "webhook", "webhook_inbound":
		return true
	default:
		return false
	}
}
