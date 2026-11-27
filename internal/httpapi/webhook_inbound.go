package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"agentflow/internal/domain"
	"agentflow/internal/repo"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// webhookRateLimiter budgets inbound posts per channel and per IP.
type webhookRateLimiter struct {
	mu      sync.Mutex
	entries map[string]webhookAttempts
	now     func() time.Time
}

type webhookAttempts struct {
	count int
	until time.Time
}

const (
	webhookRateChannelLimit = 60
	webhookRateIPLimit      = 120
	webhookRateWindow       = time.Minute
	webhookRateSources      = 8192
)

func newWebhookRateLimiter() *webhookRateLimiter {
	return &webhookRateLimiter{entries: map[string]webhookAttempts{}, now: time.Now}
}

func (l *webhookRateLimiter) allow(key string, limit int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	for k, v := range l.entries {
		if now.After(v.until) {
			delete(l.entries, k)
		}
	}
	v := l.entries[key]
	if v.count >= limit || (v.count == 0 && len(l.entries) >= webhookRateSources) {
		return false
	}
	if v.count == 0 {
		v.until = now.Add(webhookRateWindow)
	}
	v.count++
	l.entries[key] = v
	return true
}

var (
	webhookLimiterInit sync.Mutex
)

func (d *deps) allowWebhookAttempt(channelID, ip string) bool {
	webhookLimiterInit.Lock()
	if d.webhookAttempts == nil {
		d.webhookAttempts = newWebhookRateLimiter()
	}
	l := d.webhookAttempts
	webhookLimiterInit.Unlock()
	if !l.allow("ip:"+ip, webhookRateIPLimit) {
		return false
	}
	return l.allow("ch:"+channelID, webhookRateChannelLimit)
}

func (h *webhooksHandlers) inbound(c *gin.Context) {
	token := strings.TrimSpace(c.Param("token"))
	if token == "" || len(token) < 16 {
		c.JSON(401, gin.H{"error": "invalid webhook URL"})
		return
	}
	if ct := strings.ToLower(c.GetHeader("Content-Type")); ct != "" && !strings.HasPrefix(ct, "application/json") {
		c.JSON(415, gin.H{"error": "Content-Type must be application/json"})
		return
	}
	now := time.Now().UTC()
	ch, _, err := h.repo.WebhookChannelByDeliveryTokenHash(hexSHA256String(token))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(401, gin.H{"error": "invalid webhook URL"})
		return
	}
	if err != nil {
		respond(c, err)
		return
	}
	eventID := "whe-" + randomID()
	clientIP := c.ClientIP()
	logWebhookMeta("received", ch.ID, eventID, "", 0, clientIP)

	if !ch.Enabled {
		_ = h.repo.RecordWebhookEvent(domain.ProjectWebhookEvent{
			ID: eventID, ChannelID: ch.ID, ProjectID: ch.ProjectID, Status: domain.WebhookEventRejected,
			Reason: "channel_disabled", ClientIP: clientIP, ReceivedAt: now,
		}, "")
		c.JSON(403, gin.H{"error": "webhook channel is disabled"})
		return
	}
	if !h.allowWebhookAttempt(ch.ID, clientIP) {
		_ = h.repo.RecordWebhookEvent(domain.ProjectWebhookEvent{
			ID: eventID, ChannelID: ch.ID, ProjectID: ch.ProjectID, Status: domain.WebhookEventRejected,
			Reason: "rate_limited", ClientIP: clientIP, ReceivedAt: now,
		}, "")
		c.Header("Retry-After", "60")
		c.JSON(429, gin.H{"error": "rate limit exceeded"})
		return
	}

	maxBytes := ch.EffectiveMaxBodyBytes()
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, int64(maxBytes))
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		var sizeErr *http.MaxBytesError
		if errors.As(err, &sizeErr) {
			_ = h.repo.RecordWebhookEvent(domain.ProjectWebhookEvent{
				ID: eventID, ChannelID: ch.ID, ProjectID: ch.ProjectID, Status: domain.WebhookEventRejected,
				Reason: "payload_too_large", ClientIP: clientIP, PayloadBytes: maxBytes + 1, ReceivedAt: now,
			}, "")
			c.JSON(413, gin.H{"error": fmt.Sprintf("payload exceeds %d bytes", maxBytes)})
			return
		}
		c.JSON(400, gin.H{"error": "failed to read body"})
		return
	}

	tsHeader := strings.TrimSpace(c.GetHeader("X-LoopForge-Timestamp"))
	nonce := strings.TrimSpace(c.GetHeader("X-LoopForge-Nonce"))
	if tsHeader == "" || nonce == "" {
		_ = h.repo.RecordWebhookEvent(domain.ProjectWebhookEvent{
			ID: eventID, ChannelID: ch.ID, ProjectID: ch.ProjectID, Status: domain.WebhookEventRejected,
			Reason: "missing_timestamp_or_nonce", ClientIP: clientIP, PayloadBytes: len(raw), ReceivedAt: now,
		}, "")
		c.JSON(401, gin.H{"error": "X-LoopForge-Timestamp and X-LoopForge-Nonce are required"})
		return
	}
	ts, err := strconv.ParseInt(tsHeader, 10, 64)
	if err != nil {
		_ = h.repo.RecordWebhookEvent(domain.ProjectWebhookEvent{
			ID: eventID, ChannelID: ch.ID, ProjectID: ch.ProjectID, Status: domain.WebhookEventRejected,
			Reason: "invalid_timestamp", ClientIP: clientIP, PayloadBytes: len(raw), ReceivedAt: now,
		}, "")
		c.JSON(401, gin.H{"error": "invalid timestamp"})
		return
	}
	skew := now.Sub(time.Unix(ts, 0).UTC())
	if skew < 0 {
		skew = -skew
	}
	if skew > domain.WebhookTimestampSkew {
		_ = h.repo.RecordWebhookEvent(domain.ProjectWebhookEvent{
			ID: eventID, ChannelID: ch.ID, ProjectID: ch.ProjectID, Status: domain.WebhookEventRejected,
			Reason: "timestamp_expired", ClientIP: clientIP, PayloadBytes: len(raw), ReceivedAt: now,
		}, "")
		c.JSON(401, gin.H{"error": "timestamp outside allowed window", "reason": "timestamp_expired"})
		return
	}

	// Verify auth before claiming nonce so forged signatures cannot burn nonces.
	okAuth, authReason := h.verifyWebhookAuth(ch, c.Request, raw, now)
	if !okAuth {
		_ = h.repo.RecordWebhookEvent(domain.ProjectWebhookEvent{
			ID: eventID, ChannelID: ch.ID, ProjectID: ch.ProjectID, Status: domain.WebhookEventRejected,
			Reason: authReason, ClientIP: clientIP, PayloadBytes: len(raw), ReceivedAt: now,
		}, "")
		c.JSON(401, gin.H{"error": "authentication failed"})
		return
	}

	if err := h.repo.ClaimWebhookNonceStrict(ch.ID, nonce, now); err != nil {
		if errors.Is(err, repo.ErrWebhookReplay) {
			_ = h.repo.RecordWebhookEvent(domain.ProjectWebhookEvent{
				ID: eventID, ChannelID: ch.ID, ProjectID: ch.ProjectID, Status: domain.WebhookEventRejected,
				Reason: "replay_nonce", ClientIP: clientIP, PayloadBytes: len(raw), ReceivedAt: now,
			}, "")
			c.JSON(401, gin.H{"error": "nonce already used"})
			return
		}
		respond(c, err)
		return
	}

	idemKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if idemKey != "" {
		claimed, issueID, issueNumber, taskID, runID, responseJSON, hit, err := h.repo.ClaimWebhookIdempotency(ch.ID, idemKey, now)
		if err != nil {
			if errors.Is(err, repo.ErrWebhookIdempotencyPending) {
				_ = h.repo.RecordWebhookEvent(domain.ProjectWebhookEvent{
					ID: eventID, ChannelID: ch.ID, ProjectID: ch.ProjectID, Status: domain.WebhookEventRejected,
					Reason: "idempotency_pending", ClientIP: clientIP, PayloadBytes: len(raw),
					IdempotencyKeyID: shortKeyID(idemKey), ReceivedAt: now,
				}, "")
				c.Header("Retry-After", "1")
				c.JSON(409, gin.H{"error": "idempotency key is being processed"})
				return
			}
			respond(c, err)
			return
		}
		if !claimed && hit {
			_ = h.repo.RecordWebhookEvent(domain.ProjectWebhookEvent{
				ID: eventID, ChannelID: ch.ID, ProjectID: ch.ProjectID, Status: domain.WebhookEventAccepted,
				Reason: "idempotent_replay", IssueID: issueID, IssueNumber: issueNumber, TaskID: taskID, RunID: runID,
				IdempotencyKeyID: shortKeyID(idemKey), ClientIP: clientIP, PayloadBytes: len(raw), ReceivedAt: now,
			}, "")
			if responseJSON != "" {
				c.Data(200, "application/json", []byte(responseJSON))
				return
			}
			c.JSON(200, gin.H{"issueId": issueID, "number": issueNumber, "taskId": taskID, "runId": runID, "idempotent": true})
			return
		}
	}

	var payload map[string]any
	if len(raw) == 0 {
		payload = map[string]any{}
	} else {
		dec := json.NewDecoder(strings.NewReader(string(raw)))
		dec.UseNumber()
		if err := dec.Decode(&payload); err != nil {
			_ = h.repo.RecordWebhookEvent(domain.ProjectWebhookEvent{
				ID: eventID, ChannelID: ch.ID, ProjectID: ch.ProjectID, Status: domain.WebhookEventRejected,
				Reason: "invalid_json", ClientIP: clientIP, PayloadBytes: len(raw), ReceivedAt: now,
			}, "")
			c.JSON(400, gin.H{"error": "invalid JSON payload"})
			return
		}
	}

	title, description := domain.MapWebhookTitleDescription(ch, payload, now)
	filteredHeaders := domain.FilterWebhookHeaders(c.Request.Header)
	ctxObj := domain.WebhookInboundContext{
		ChannelID: ch.ID, EventID: eventID, Payload: payload, Headers: filteredHeaders,
	}
	ctxJSON, _ := json.Marshal(ctxObj)

	_ = h.repo.RecordWebhookEvent(domain.ProjectWebhookEvent{
		ID: eventID, ChannelID: ch.ID, ProjectID: ch.ProjectID, Status: domain.WebhookEventReceived,
		ClientIP: clientIP, PayloadBytes: len(raw), IdempotencyKeyID: shortKeyID(idemKey), ReceivedAt: now,
	}, string(ctxJSON))

	issue := domain.Issue{
		ID:             fmt.Sprintf("issue-%d", time.Now().UnixNano()),
		ProjectID:      ch.ProjectID,
		Title:          title,
		Description:    description,
		Status:         "todo",
		Priority:       "medium",
		Kind:           "requirement",
		Source:         "webhook",
		WorkflowID:     strings.TrimSpace(ch.DefaultWorkflowID),
		AssigneeUserID: ch.AssigneeUserID,
		ExternalRef: &domain.ExternalRef{
			Source: "webhook", ExternalID: ch.ID + "/" + eventID, ExternalType: "webhook_event", SyncedAt: now,
		},
	}
	issue.AppendOperation(domain.IssueOperation{
		Action: "create", Detail: "created via inbound webhook channel " + ch.ID, Actor: "webhook",
	})
	if err := h.repo.PutIssue(issue); err != nil {
		_ = h.repo.UpdateWebhookEvent(eventID, domain.WebhookEventRejected, "issue_create_failed", "", 0, "", "")
		if idemKey != "" {
			_ = h.repo.DeleteWebhookIdempotency(ch.ID, idemKey)
		}
		respond(c, err)
		return
	}
	stored, _ := h.repo.Issue(issue.ID)
	issue = stored

	resp := gin.H{
		"issueId": issue.ID, "number": issue.Number, "eventId": eventID, "channelId": ch.ID,
	}
	status := domain.WebhookEventAccepted
	reason := ""
	var taskID, runID string
	httpStatus := 201
	inputExtra := formatWebhookSOPInput(ctxObj)

	if ch.AutoDispatch {
		wf := strings.TrimSpace(ch.DefaultWorkflowID)
		if wf == "" {
			status = domain.WebhookEventDispatchFailed
			reason = "missing_default_workflow"
			_ = h.repo.PutComment(domain.IssueComment{
				ID: "cmt-" + randomID(), IssueID: issue.ID, AuthorType: "system", AuthorID: "webhook",
				Body: "Auto-dispatch failed: channel has no default SOP/workflow.", CreatedAt: now,
			})
			_ = h.repo.UpdateWebhookEvent(eventID, status, reason, issue.ID, issue.Number, "", "")
			fail := gin.H{"error": "issue created but SOP dispatch failed: no default workflow", "issueId": issue.ID, "number": issue.Number, "eventId": eventID}
			h.finishWebhookIdempotent(c, ch.ID, idemKey, issue, "", "", fail, 502, now, status, eventID, clientIP)
			return
		}
		task, derr := dispatchIssue(h.deps, dispatchRequest{
			IssueID: issue.ID, WorkflowID: wf, TriggerOrigin: "webhook",
			TriggerUserID: ch.AssigneeUserID,
			ExtraInput:   inputExtra,
		})
		// Keep a durable comment for operators / later manual dispatch context.
		_ = h.repo.PutComment(domain.IssueComment{
			ID: "cmt-" + randomID(), IssueID: issue.ID, AuthorType: "system", AuthorID: "webhook",
			Body: "Webhook SOP input variables:\n\n" + inputExtra, CreatedAt: now,
		})
		if derr != nil {
			status = domain.WebhookEventDispatchFailed
			reason = "dispatch_error"
			logWebhookMeta("dispatch_failed", ch.ID, eventID, issue.ID, issue.Number, clientIP)
			_ = h.repo.PutComment(domain.IssueComment{
				ID: "cmt-" + randomID(), IssueID: issue.ID, AuthorType: "system", AuthorID: "webhook",
				Body: "Auto-dispatch failed: " + safeWebhookErr(derr), CreatedAt: now,
			})
			_ = h.repo.UpdateWebhookEvent(eventID, status, reason, issue.ID, issue.Number, "", "")
			fail := gin.H{"error": "issue created but SOP dispatch failed", "detail": safeWebhookErr(derr), "issueId": issue.ID, "number": issue.Number, "eventId": eventID}
			h.finishWebhookIdempotent(c, ch.ID, idemKey, issue, "", "", fail, 502, now, status, eventID, clientIP)
			return
		}
		taskID, runID = task.ID, task.RunID
		resp["taskId"] = taskID
		resp["runId"] = runID
		status = domain.WebhookEventDispatched
		// Defensive: ExtraInput already went into Start; keep append for older runs.
		_ = appendWebhookInputToRun(h.deps, runID, inputExtra)
	}

	_ = h.repo.UpdateWebhookEvent(eventID, status, reason, issue.ID, issue.Number, taskID, runID)
	h.finishWebhookIdempotent(c, ch.ID, idemKey, issue, taskID, runID, resp, httpStatus, now, status, eventID, clientIP)
}

// finishWebhookIdempotent records the idempotency mapping (including 502 bodies) then writes the HTTP response.
func (h *webhooksHandlers) finishWebhookIdempotent(c *gin.Context, channelID, idemKey string, issue domain.Issue, taskID, runID string, resp gin.H, httpStatus int, now time.Time, status, eventID, clientIP string) {
	body, _ := json.Marshal(resp)
	if idemKey != "" {
		_ = h.repo.PutWebhookIdempotency(channelID, idemKey, issue.ID, issue.Number, taskID, runID, string(body), now)
	}
	logWebhookMeta(status, channelID, eventID, issue.ID, issue.Number, clientIP)
	c.Data(httpStatus, "application/json", body)
}

// webhookExtraInputForIssue reloads filtered webhook context for manual dispatch of webhook-sourced issues.
func webhookExtraInputForIssue(d *deps, issue domain.Issue) string {
	if d == nil || d.repo == nil || !strings.EqualFold(strings.TrimSpace(issue.Source), "webhook") {
		return ""
	}
	if issue.ExternalRef == nil {
		return ""
	}
	ext := strings.TrimSpace(issue.ExternalRef.ExternalID)
	channelID, eventID, ok := strings.Cut(ext, "/")
	if !ok || strings.TrimSpace(eventID) == "" {
		return ""
	}
	summary, err := d.repo.WebhookEventPayloadSummary(eventID)
	if err != nil || strings.TrimSpace(summary) == "" {
		return ""
	}
	var ctx domain.WebhookInboundContext
	if err := json.Unmarshal([]byte(summary), &ctx); err != nil {
		return ""
	}
	if ctx.ChannelID == "" {
		ctx.ChannelID = channelID
	}
	if ctx.EventID == "" {
		ctx.EventID = eventID
	}
	return formatWebhookSOPInput(ctx)
}

func appendWebhookInputToRun(d *deps, runID, block string) error {
	runID = strings.TrimSpace(runID)
	if runID == "" || d == nil || d.repo == nil {
		return nil
	}
	run, ok := d.repo.Run(runID)
	if !ok {
		return nil
	}
	if strings.Contains(run.Input, "webhook.payload") {
		return nil
	}
	run.Input = strings.TrimSpace(run.Input) + "\n\n" + block
	return d.repo.PutRun(run)
}

func formatWebhookSOPInput(ctx domain.WebhookInboundContext) string {
	payload, _ := json.MarshalIndent(ctx.Payload, "", "  ")
	headers, _ := json.MarshalIndent(ctx.Headers, "", "  ")
	return strings.TrimSpace(fmt.Sprintf(`## Webhook input (stable variables)
- webhook.channelId: %s
- webhook.eventId: %s
- webhook.headers:
`+"```json\n%s\n```"+`
- webhook.payload:
`+"```json\n%s\n```"+`

Scripts may also read these from the issue comment and from run input. Secrets and signatures are never included.`,
		ctx.ChannelID, ctx.EventID, string(headers), string(payload)))
}

func (h *webhooksHandlers) verifyWebhookAuth(ch domain.ProjectWebhookChannel, r *http.Request, raw []byte, now time.Time) (bool, string) {
	mode := domain.NormalizeWebhookAuthMode(ch.AuthMode)
	sigHeader := strings.TrimSpace(r.Header.Get("X-LoopForge-Signature"))
	tokenHeader := strings.TrimSpace(r.Header.Get("X-LoopForge-Token"))
	if tokenHeader == "" {
		if bearer, ok := bearerToken(r); ok {
			tokenHeader = bearer
		}
	}

	hmacOK := false
	tokenOK := false
	if sigHeader != "" {
		secret, ver, err := h.findSecretMatchingHMAC(ch.ID, raw, sigHeader, now)
		_ = secret
		_ = ver
		if err == nil && secret != "" {
			hmacOK = true
		}
	}
	if tokenHeader != "" {
		ok, _, err := h.repo.MatchWebhookSecret(ch.ID, tokenHeader, now)
		if err == nil && ok {
			tokenOK = true
		}
	}

	switch mode {
	case domain.WebhookAuthToken:
		if tokenOK {
			return true, ""
		}
		return false, "bad_token"
	case domain.WebhookAuthHMACOrToken:
		if hmacOK || tokenOK {
			return true, ""
		}
		return false, "bad_hmac_or_token"
	default: // hmac
		if hmacOK {
			return true, ""
		}
		return false, "bad_hmac"
	}
}

func (h *webhooksHandlers) findSecretMatchingHMAC(channelID string, raw []byte, sigHeader string, now time.Time) (matchedSecret string, version int, err error) {
	sigHeader = strings.TrimSpace(sigHeader)
	lower := strings.ToLower(sigHeader)
	if strings.HasPrefix(lower, "sha256=") {
		sigHeader = strings.TrimSpace(sigHeader[len("sha256="):])
	}
	want, err := hex.DecodeString(sigHeader)
	if err != nil || len(want) != sha256.Size {
		return "", 0, errors.New("bad signature encoding")
	}
	ok, ver, err := h.repo.MatchWebhookHMAC(channelID, raw, want, now)
	if err != nil || !ok {
		return "", 0, errors.New("hmac mismatch")
	}
	return "ok", ver, nil
}

func hexSHA256String(v string) string {
	sum := sha256.Sum256([]byte(v))
	return hex.EncodeToString(sum[:])
}

func shortKeyID(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:8])
}

func safeWebhookErr(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	lower := strings.ToLower(msg)
	for _, bad := range []string{"whsec_", "authorization", "bearer ", "signature", "secret"} {
		if strings.Contains(lower, bad) {
			return "dispatch failed"
		}
	}
	if len(msg) > 300 {
		return msg[:300]
	}
	return msg
}

func logWebhookMeta(status, channelID, eventID, issueID string, issueNumber int, ip string) {
	log.Printf("webhook inbound status=%s channel=%s event=%s issue=%s number=%d ip=%s", status, channelID, eventID, issueID, issueNumber, ip)
}
