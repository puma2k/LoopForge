package repo

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"agentflow/internal/credcrypt"
	"agentflow/internal/domain"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// WebhooksRepo owns project inbound webhook channels, secrets, events,
// nonces and idempotency keys.
type WebhooksRepo struct {
	*deps
	users *UsersRepo
}

type projectWebhookChannelRow struct {
	ID                string    `gorm:"column:id;primaryKey;size:64"`
	ProjectID         string    `gorm:"column:project_id;size:128;index:idx_project_webhook_channels_project"`
	DeliveryTokenHash string    `gorm:"column:delivery_token_hash;size:64;uniqueIndex"`
	Enabled           bool      `gorm:"column:enabled"`
	Payload           []byte    `gorm:"column:payload;type:json"`
	CreatedAt         time.Time `gorm:"column:created_at"`
	UpdatedAt         time.Time `gorm:"column:updated_at"`
}

func (projectWebhookChannelRow) TableName() string { return "project_webhook_channels" }

type projectWebhookSecretRow struct {
	ID           string     `gorm:"column:id;primaryKey;size:64"`
	ChannelID    string     `gorm:"column:channel_id;size:64;uniqueIndex:idx_project_webhook_secrets_channel_version,priority:1;index:idx_project_webhook_secrets_channel"`
	Version      int        `gorm:"column:version;uniqueIndex:idx_project_webhook_secrets_channel_version,priority:2"`
	SecretHash   string     `gorm:"column:secret_hash;size:64;index:idx_project_webhook_secrets_hash"`
	SecretEnc    string     `gorm:"column:secret_enc;type:text"`
	SecretPrefix string     `gorm:"column:secret_prefix;size:32"`
	CreatedAt    time.Time  `gorm:"column:created_at"`
	GraceUntil   *time.Time `gorm:"column:grace_until"`
}

func (projectWebhookSecretRow) TableName() string { return "project_webhook_secrets" }

type projectWebhookEventRow struct {
	ID               string    `gorm:"column:id;primaryKey;size:64"`
	ChannelID        string    `gorm:"column:channel_id;size:64;index:idx_project_webhook_events_channel_time,priority:1"`
	ProjectID        string    `gorm:"column:project_id;size:128;index:idx_project_webhook_events_project_time,priority:1"`
	Status           string    `gorm:"column:status;size:32"`
	Reason           string    `gorm:"column:reason;size:512"`
	IssueID          string    `gorm:"column:issue_id;size:128;index:idx_project_webhook_events_issue"`
	IssueNumber      int       `gorm:"column:issue_number"`
	TaskID           string    `gorm:"column:task_id;size:128"`
	RunID            string    `gorm:"column:run_id;size:128"`
	IdempotencyKeyID string    `gorm:"column:idempotency_key_id;size:64"`
	ClientIP         string    `gorm:"column:client_ip;size:64"`
	PayloadBytes     int       `gorm:"column:payload_bytes"`
	PayloadSummary   string    `gorm:"column:payload_summary;type:mediumtext"`
	ReceivedAt       time.Time `gorm:"column:received_at;index:idx_project_webhook_events_channel_time,priority:2;index:idx_project_webhook_events_project_time,priority:2"`
}

func (projectWebhookEventRow) TableName() string { return "project_webhook_events" }

type projectWebhookNonceRow struct {
	ChannelID string    `gorm:"column:channel_id;primaryKey;size:64"`
	NonceHash string    `gorm:"column:nonce_hash;primaryKey;size:64"`
	ExpiresAt time.Time `gorm:"column:expires_at;index:idx_project_webhook_nonces_expires"`
}

func (projectWebhookNonceRow) TableName() string { return "project_webhook_nonces" }

type projectWebhookIdempotencyRow struct {
	ChannelID      string    `gorm:"column:channel_id;primaryKey;size:64"`
	KeyHash        string    `gorm:"column:key_hash;primaryKey;size:64"`
	IssueID        string    `gorm:"column:issue_id;size:128"`
	IssueNumber    int       `gorm:"column:issue_number"`
	TaskID         string    `gorm:"column:task_id;size:128"`
	RunID          string    `gorm:"column:run_id;size:128"`
	ResponseJSON   string    `gorm:"column:response_json;type:mediumtext"`
	ResponseStatus int       `gorm:"column:response_status"`
	ExpiresAt      time.Time `gorm:"column:expires_at;index:idx_project_webhook_idempotency_expires"`
}

func (projectWebhookIdempotencyRow) TableName() string { return "project_webhook_idempotency" }

type webhookChannelPayload struct {
	Name                string `json:"name"`
	Description         string `json:"description,omitempty"`
	AuthMode            string `json:"authMode"`
	DefaultWorkflowID   string `json:"defaultWorkflowId,omitempty"`
	AssigneeUserID      int64  `json:"assigneeUserId,omitempty"`
	TitleTemplate       string `json:"titleTemplate,omitempty"`
	DescriptionTemplate string `json:"descriptionTemplate,omitempty"`
	AutoDispatch        bool   `json:"autoDispatch"`
	MaxBodyBytes        int    `json:"maxBodyBytes,omitempty"`
	DeliveryTokenPrefix string `json:"deliveryTokenPrefix"`
}

func decodeWebhookChannel(row projectWebhookChannelRow) (domain.ProjectWebhookChannel, error) {
	var p webhookChannelPayload
	if len(row.Payload) > 0 {
		if err := json.Unmarshal(row.Payload, &p); err != nil {
			return domain.ProjectWebhookChannel{}, err
		}
	}
	ch := domain.ProjectWebhookChannel{
		ID:                  row.ID,
		ProjectID:           row.ProjectID,
		Name:                p.Name,
		Description:         p.Description,
		Enabled:             row.Enabled,
		DeliveryTokenPrefix: p.DeliveryTokenPrefix,
		AuthMode:            domain.NormalizeWebhookAuthMode(p.AuthMode),
		DefaultWorkflowID:   p.DefaultWorkflowID,
		AssigneeUserID:      p.AssigneeUserID,
		TitleTemplate:       p.TitleTemplate,
		DescriptionTemplate: p.DescriptionTemplate,
		AutoDispatch:        p.AutoDispatch,
		MaxBodyBytes:        p.MaxBodyBytes,
		CreatedAt:           row.CreatedAt,
		UpdatedAt:           row.UpdatedAt,
		InboundPath:         "/api/webhooks/inbound/" + p.DeliveryTokenPrefix + "…",
	}
	return ch, nil
}

func encodeWebhookChannelPayload(ch domain.ProjectWebhookChannel) ([]byte, error) {
	return json.Marshal(webhookChannelPayload{
		Name:                ch.Name,
		Description:         ch.Description,
		AuthMode:            domain.NormalizeWebhookAuthMode(ch.AuthMode),
		DefaultWorkflowID:   ch.DefaultWorkflowID,
		AssigneeUserID:      ch.AssigneeUserID,
		TitleTemplate:       ch.TitleTemplate,
		DescriptionTemplate: ch.DescriptionTemplate,
		AutoDispatch:        ch.AutoDispatch,
		MaxBodyBytes:        ch.MaxBodyBytes,
		DeliveryTokenPrefix: ch.DeliveryTokenPrefix,
	})
}

func webhookSecretAAD(channelID string) string {
	return "webhook-secret|" + channelID
}

func (r *WebhooksRepo) sealWebhookSecret(channelID, secretPlain string) (string, error) {
	if r.users == nil || r.users.credentialKeyring() == nil {
		return "", ErrCredentialKeyMissing
	}
	return r.users.credentialKeyring().Seal([]byte(secretPlain), webhookSecretAAD(channelID))
}

func (r *WebhooksRepo) openWebhookSecret(channelID, stored string) (string, error) {
	if !credcrypt.IsSealed(stored) {
		// Legacy/test fallback: treat as plaintext only when not sealed.
		return stored, nil
	}
	if r.users == nil || r.users.credentialKeyring() == nil {
		return "", ErrCredentialKeyMissing
	}
	plain, err := r.users.credentialKeyring().Open(stored, webhookSecretAAD(channelID))
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// CreateWebhookChannel persists a channel and its first secret version.
// deliveryToken is hashed; secret is hashed + sealed (never stored as plaintext).
func (r *WebhooksRepo) CreateWebhookChannel(ch domain.ProjectWebhookChannel, deliveryToken, secretPlain string) error {
	now := time.Now().UTC()
	if ch.CreatedAt.IsZero() {
		ch.CreatedAt = now
	}
	ch.UpdatedAt = now
	ch.AuthMode = domain.NormalizeWebhookAuthMode(ch.AuthMode)
	payload, err := encodeWebhookChannelPayload(ch)
	if err != nil {
		return err
	}
	sealed, err := r.sealWebhookSecret(ch.ID, secretPlain)
	if err != nil {
		return err
	}
	return r.DB.Transaction(func(tx *gorm.DB) error {
		row := projectWebhookChannelRow{
			ID: ch.ID, ProjectID: ch.ProjectID, DeliveryTokenHash: hexSHA256(deliveryToken),
			Enabled: ch.Enabled, Payload: payload, CreatedAt: ch.CreatedAt, UpdatedAt: ch.UpdatedAt,
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		sec := projectWebhookSecretRow{
			ID: "whs-" + ch.ID + "-v1", ChannelID: ch.ID, Version: 1,
			SecretHash: hexSHA256(secretPlain), SecretEnc: sealed, SecretPrefix: maskPrefix(secretPlain),
			CreatedAt: now,
		}
		return tx.Create(&sec).Error
	})
}

func maskPrefix(secret string) string {
	secret = strings.TrimSpace(secret)
	if len(secret) <= 8 {
		return secret
	}
	return secret[:8]
}

// WebhookChannel returns a channel by id (without inbound URL/token).
func (r *WebhooksRepo) WebhookChannel(id string) (domain.ProjectWebhookChannel, error) {
	var row projectWebhookChannelRow
	if err := r.DB.First(&row, "id = ?", id).Error; err != nil {
		return domain.ProjectWebhookChannel{}, err
	}
	ch, err := decodeWebhookChannel(row)
	if err != nil {
		return ch, err
	}
	r.attachSecretMeta(&ch)
	return ch, nil
}

func (r *WebhooksRepo) attachSecretMeta(ch *domain.ProjectWebhookChannel) {
	var rows []projectWebhookSecretRow
	_ = r.DB.Where("channel_id = ?", ch.ID).Order("version desc").Find(&rows).Error
	now := time.Now().UTC()
	for _, s := range rows {
		if s.GraceUntil == nil {
			ch.SecretPrefix = s.SecretPrefix
			ch.SecretVersion = s.Version
			continue
		}
		if s.GraceUntil.After(now) {
			ch.HasPreviousSecret = true
			until := *s.GraceUntil
			ch.PreviousSecretUntil = &until
		}
	}
	if ch.SecretVersion == 0 && len(rows) > 0 {
		ch.SecretPrefix = rows[0].SecretPrefix
		ch.SecretVersion = rows[0].Version
	}
}

// WebhookChannels lists channels for a project.
func (r *WebhooksRepo) WebhookChannels(projectID string) ([]domain.ProjectWebhookChannel, error) {
	var rows []projectWebhookChannelRow
	if err := r.DB.Where("project_id = ?", projectID).Order("created_at desc").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]domain.ProjectWebhookChannel, 0, len(rows))
	for _, row := range rows {
		ch, err := decodeWebhookChannel(row)
		if err != nil {
			return nil, err
		}
		r.attachSecretMeta(&ch)
		out = append(out, ch)
	}
	return out, nil
}

// WebhookChannelByDeliveryTokenHash looks up a channel by hashed path token.
func (r *WebhooksRepo) WebhookChannelByDeliveryTokenHash(tokenHash string) (domain.ProjectWebhookChannel, string, error) {
	var row projectWebhookChannelRow
	if err := r.DB.First(&row, "delivery_token_hash = ?", tokenHash).Error; err != nil {
		return domain.ProjectWebhookChannel{}, "", err
	}
	ch, err := decodeWebhookChannel(row)
	if err != nil {
		return ch, "", err
	}
	r.attachSecretMeta(&ch)
	return ch, row.DeliveryTokenHash, nil
}

// UpdateWebhookChannel updates mutable channel fields (not secrets/token).
func (r *WebhooksRepo) UpdateWebhookChannel(ch domain.ProjectWebhookChannel) error {
	ch.UpdatedAt = time.Now().UTC()
	ch.AuthMode = domain.NormalizeWebhookAuthMode(ch.AuthMode)
	payload, err := encodeWebhookChannelPayload(ch)
	if err != nil {
		return err
	}
	res := r.DB.Model(&projectWebhookChannelRow{}).Where("id = ? AND project_id = ?", ch.ID, ch.ProjectID).Updates(map[string]any{
		"enabled": ch.Enabled, "payload": payload, "updated_at": ch.UpdatedAt,
	})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// DeleteWebhookChannel removes a channel and related secret/nonce/idempotency rows.
func (r *WebhooksRepo) DeleteWebhookChannel(projectID, id string) error {
	return r.DB.Transaction(func(tx *gorm.DB) error {
		res := tx.Where("id = ? AND project_id = ?", id, projectID).Delete(&projectWebhookChannelRow{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		_ = tx.Where("channel_id = ?", id).Delete(&projectWebhookSecretRow{}).Error
		_ = tx.Where("channel_id = ?", id).Delete(&projectWebhookNonceRow{}).Error
		_ = tx.Where("channel_id = ?", id).Delete(&projectWebhookIdempotencyRow{}).Error
		return nil
	})
}

// RotateWebhookSecret creates a new secret version and puts the previous one in grace.
func (r *WebhooksRepo) RotateWebhookSecret(channelID, secretPlain string, grace time.Duration) (version int, prefix string, err error) {
	now := time.Now().UTC()
	if grace <= 0 {
		grace = domain.WebhookSecretGrace
	}
	sealed, err := r.sealWebhookSecret(channelID, secretPlain)
	if err != nil {
		return 0, "", err
	}
	err = r.DB.Transaction(func(tx *gorm.DB) error {
		var current projectWebhookSecretRow
		q := tx.Where("channel_id = ? AND grace_until IS NULL", channelID).Order("version desc").Limit(1).Find(&current)
		if q.Error != nil {
			return q.Error
		}
		nextVer := 1
		if current.ID != "" {
			nextVer = current.Version + 1
			until := now.Add(grace)
			if err := tx.Model(&projectWebhookSecretRow{}).Where("id = ?", current.ID).Update("grace_until", until).Error; err != nil {
				return err
			}
		}
		sec := projectWebhookSecretRow{
			ID: "whs-" + channelID + "-v" + itoa(nextVer), ChannelID: channelID, Version: nextVer,
			SecretHash: hexSHA256(secretPlain), SecretEnc: sealed, SecretPrefix: maskPrefix(secretPlain),
			CreatedAt: now,
		}
		version = nextVer
		prefix = sec.SecretPrefix
		return tx.Create(&sec).Error
	})
	return version, prefix, err
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// ActiveWebhookSecrets returns secret hashes still valid for authentication.
func (r *WebhooksRepo) ActiveWebhookSecrets(channelID string, now time.Time) ([]projectWebhookSecretRow, error) {
	var rows []projectWebhookSecretRow
	err := r.DB.Where("channel_id = ? AND (grace_until IS NULL OR grace_until > ?)", channelID, now).Order("version desc").Find(&rows).Error
	return rows, err
}

// MatchWebhookSecret reports whether secretPlain matches any active secret (timing-safe via hash equality).
func (r *WebhooksRepo) MatchWebhookSecret(channelID, secretPlain string, now time.Time) (bool, int, error) {
	rows, err := r.ActiveWebhookSecrets(channelID, now)
	if err != nil {
		return false, 0, err
	}
	want := hexSHA256(secretPlain)
	for _, row := range rows {
		if subtleConstantTimeEqual(row.SecretHash, want) {
			return true, row.Version, nil
		}
	}
	return false, 0, nil
}

func subtleConstantTimeEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := 0; i < len(a); i++ {
		v |= a[i] ^ b[i]
	}
	return v == 0
}

// WebhookHMACSignedPayload builds the bytes covered by inbound HMAC:
// timestamp + "." + nonce + "." + rawBody. Timestamp and nonce must be in the
// MAC so a captured signature cannot be replayed with a fresh nonce/ts.
func WebhookHMACSignedPayload(timestamp, nonce string, body []byte) []byte {
	n := len(timestamp) + 1 + len(nonce) + 1 + len(body)
	out := make([]byte, 0, n)
	out = append(out, timestamp...)
	out = append(out, '.')
	out = append(out, nonce...)
	out = append(out, '.')
	out = append(out, body...)
	return out
}

// MatchWebhookHMAC opens active sealed secrets and verifies
// HMAC-SHA256(secret, timestamp + "." + nonce + "." + body).
func (r *WebhooksRepo) MatchWebhookHMAC(channelID, timestamp, nonce string, body, wantMAC []byte, now time.Time) (bool, int, error) {
	rows, err := r.ActiveWebhookSecrets(channelID, now)
	if err != nil {
		return false, 0, err
	}
	signed := WebhookHMACSignedPayload(timestamp, nonce, body)
	for _, row := range rows {
		plain, err := r.openWebhookSecret(channelID, row.SecretEnc)
		if err != nil {
			continue
		}
		mac := hmac.New(sha256.New, []byte(plain))
		mac.Write(signed)
		got := mac.Sum(nil)
		if subtle.ConstantTimeCompare(got, wantMAC) == 1 {
			return true, row.Version, nil
		}
	}
	return false, 0, nil
}

// ErrWebhookReplay means the nonce was already consumed.
var ErrWebhookReplay = errors.New("webhook nonce already used")

// ClaimWebhookNonceStrict inserts and returns ErrWebhookReplay when the nonce already exists.
func (r *WebhooksRepo) ClaimWebhookNonceStrict(channelID, nonce string, now time.Time) error {
	nonce = strings.TrimSpace(nonce)
	if nonce == "" {
		return errors.New("nonce required")
	}
	_ = r.DB.Where("expires_at < ?", now).Delete(&projectWebhookNonceRow{}).Error
	row := projectWebhookNonceRow{
		ChannelID: channelID, NonceHash: hexSHA256(nonce), ExpiresAt: now.Add(domain.WebhookNonceTTL),
	}
	res := r.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrWebhookReplay
	}
	return nil
}

// ErrWebhookIdempotencyPending means another request holds the key but has not finished yet.
var ErrWebhookIdempotencyPending = errors.New("webhook idempotency key pending")

// webhookIdempotencyReplayStatus maps a stored status; 0 (legacy) means 200.
func webhookIdempotencyReplayStatus(stored int) int {
	if stored <= 0 {
		return 200
	}
	return stored
}

// WebhookIdempotencyLookup returns a prior response for the same key if still valid.
func (r *WebhooksRepo) WebhookIdempotencyLookup(channelID, key string, now time.Time) (issueID string, issueNumber int, taskID, runID, responseJSON string, httpStatus int, ok bool, err error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", 0, "", "", "", 0, false, nil
	}
	_ = r.DB.Where("expires_at < ?", now).Delete(&projectWebhookIdempotencyRow{}).Error
	var row projectWebhookIdempotencyRow
	q := r.DB.Where("channel_id = ? AND key_hash = ? AND expires_at > ?", channelID, hexSHA256(key), now).Limit(1).Find(&row)
	if q.Error != nil {
		return "", 0, "", "", "", 0, false, q.Error
	}
	if row.IssueID == "" {
		return "", 0, "", "", "", 0, false, nil
	}
	return row.IssueID, row.IssueNumber, row.TaskID, row.RunID, row.ResponseJSON, webhookIdempotencyReplayStatus(row.ResponseStatus), true, nil
}

// ClaimWebhookIdempotency inserts a pending row for the key. claimed=true means this
// caller owns creation. claimed=false with ok=true returns the winner's completed
// response (waits briefly if still pending).
func (r *WebhooksRepo) ClaimWebhookIdempotency(channelID, key string, now time.Time) (claimed bool, issueID string, issueNumber int, taskID, runID, responseJSON string, httpStatus int, ok bool, err error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return true, "", 0, "", "", "", 0, false, nil
	}
	_ = r.DB.Where("expires_at < ?", now).Delete(&projectWebhookIdempotencyRow{}).Error
	row := projectWebhookIdempotencyRow{
		ChannelID: channelID, KeyHash: hexSHA256(key), ExpiresAt: now.Add(domain.WebhookIdempotencyTTL),
	}
	res := r.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
	if res.Error != nil {
		return false, "", 0, "", "", "", 0, false, res.Error
	}
	if res.RowsAffected == 1 {
		return true, "", 0, "", "", "", 0, false, nil
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		var existing projectWebhookIdempotencyRow
		q := r.DB.Where("channel_id = ? AND key_hash = ?", channelID, hexSHA256(key)).Limit(1).Find(&existing)
		if q.Error != nil {
			return false, "", 0, "", "", "", 0, false, q.Error
		}
		if existing.IssueID != "" {
			return false, existing.IssueID, existing.IssueNumber, existing.TaskID, existing.RunID, existing.ResponseJSON, webhookIdempotencyReplayStatus(existing.ResponseStatus), true, nil
		}
		if time.Now().After(deadline) {
			return false, "", 0, "", "", "", 0, false, ErrWebhookIdempotencyPending
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// PutWebhookIdempotency stores (or completes) the response for a key.
func (r *WebhooksRepo) PutWebhookIdempotency(channelID, key, issueID string, issueNumber int, taskID, runID, responseJSON string, httpStatus int, now time.Time) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	if httpStatus <= 0 {
		httpStatus = 200
	}
	row := projectWebhookIdempotencyRow{
		ChannelID: channelID, KeyHash: hexSHA256(key), IssueID: issueID, IssueNumber: issueNumber,
		TaskID: taskID, RunID: runID, ResponseJSON: responseJSON, ResponseStatus: httpStatus,
		ExpiresAt: now.Add(domain.WebhookIdempotencyTTL),
	}
	return r.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "channel_id"}, {Name: "key_hash"}},
		DoUpdates: clause.AssignmentColumns([]string{"issue_id", "issue_number", "task_id", "run_id", "response_json", "response_status", "expires_at"}),
	}).Create(&row).Error
}

// DeleteWebhookIdempotency drops a pending/completed key so a failed create can retry.
func (r *WebhooksRepo) DeleteWebhookIdempotency(channelID, key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	return r.DB.Where("channel_id = ? AND key_hash = ?", channelID, hexSHA256(key)).Delete(&projectWebhookIdempotencyRow{}).Error
}

// RecordWebhookEvent appends an audit event (never store secrets).
func (r *WebhooksRepo) RecordWebhookEvent(ev domain.ProjectWebhookEvent, payloadSummary string) error {
	if ev.ReceivedAt.IsZero() {
		ev.ReceivedAt = time.Now().UTC()
	}
	row := projectWebhookEventRow{
		ID: ev.ID, ChannelID: ev.ChannelID, ProjectID: ev.ProjectID, Status: ev.Status, Reason: ev.Reason,
		IssueID: ev.IssueID, IssueNumber: ev.IssueNumber, TaskID: ev.TaskID, RunID: ev.RunID,
		IdempotencyKeyID: ev.IdempotencyKeyID, ClientIP: ev.ClientIP, PayloadBytes: ev.PayloadBytes,
		PayloadSummary: payloadSummary, ReceivedAt: ev.ReceivedAt,
	}
	return r.DB.Create(&row).Error
}

// UpdateWebhookEvent patches outcome fields after create/dispatch.
func (r *WebhooksRepo) UpdateWebhookEvent(id string, status, reason, issueID string, issueNumber int, taskID, runID string) error {
	return r.DB.Model(&projectWebhookEventRow{}).Where("id = ?", id).Updates(map[string]any{
		"status": status, "reason": reason, "issue_id": issueID, "issue_number": issueNumber,
		"task_id": taskID, "run_id": runID,
	}).Error
}

// WebhookEvents lists recent events for a channel.
func (r *WebhooksRepo) WebhookEvents(channelID string, limit int) ([]domain.ProjectWebhookEvent, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	var rows []projectWebhookEventRow
	if err := r.DB.Where("channel_id = ?", channelID).Order("received_at desc").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]domain.ProjectWebhookEvent, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.ProjectWebhookEvent{
			ID: row.ID, ChannelID: row.ChannelID, ProjectID: row.ProjectID, Status: row.Status, Reason: row.Reason,
			IssueID: row.IssueID, IssueNumber: row.IssueNumber, TaskID: row.TaskID, RunID: row.RunID,
			IdempotencyKeyID: row.IdempotencyKeyID, ClientIP: row.ClientIP, PayloadBytes: row.PayloadBytes,
			ReceivedAt: row.ReceivedAt,
		})
	}
	return out, nil
}

// WebhookEventPayloadSummary returns the stored summary for SOP injection.
func (r *WebhooksRepo) WebhookEventPayloadSummary(eventID string) (string, error) {
	var row projectWebhookEventRow
	if err := r.DB.Select("payload_summary").First(&row, "id = ?", eventID).Error; err != nil {
		return "", err
	}
	return row.PayloadSummary, nil
}

// StoreWebhookEventContext stores full (filtered) JSON context in payload_summary for SOP reads.
// Callers must already strip secrets.
func (r *WebhooksRepo) StoreWebhookEventContext(eventID, contextJSON string) error {
	return r.DB.Model(&projectWebhookEventRow{}).Where("id = ?", eventID).Update("payload_summary", contextJSON).Error
}
