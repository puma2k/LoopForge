package httpapi

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agentflow/internal/domain"
	"agentflow/internal/engine"
	"agentflow/internal/repo"
	"agentflow/internal/runtime"
	"agentflow/internal/tracker"
)

func TestProjectInboundWebhooks(t *testing.T) {
	t.Setenv("AGENTFLOW_AUTH_REQUIRED", "true")
	t.Setenv("AGENTFLOW_SESSION_SECRET", "webhook-test-secret")
	t.Setenv("AGENTFLOW_SCHEDULER_ENABLED", "false")
	s, err := repo.OpenTest()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	for _, login := range []string{"wh-owner", "wh-member", "wh-maint"} {
		if err := s.PutUser(domain.User{LoginName: login}); err != nil {
			t.Fatal(err)
		}
	}
	owner, _ := s.UserByLoginName("wh-owner")
	member, _ := s.UserByLoginName("wh-member")
	maint, _ := s.UserByLoginName("wh-maint")
	if err := s.PutProject(domain.Project{
		ID: "wh-project", Name: "Webhooks", OwnerID: owner.ID, WorkflowID: "code-review-merge-auto",
		Members: []domain.ProjectMember{{UserID: member.ID, Role: "member"}, {UserID: maint.ID, Role: "maintainer"}},
	}); err != nil {
		t.Fatal(err)
	}
	h := New(s, engine.New(s, runtime.NewManager(filepath.Join(t.TempDir(), "workspaces"))), tracker.NewService(), "", "")

	requestJSONAuthed[map[string]any](t, h, member, "POST", "/api/projects/wh-project/webhooks", map[string]any{"name": "CI"}, 403)

	type created struct {
		Channel       domain.ProjectWebhookChannel `json:"channel"`
		DeliveryToken string                       `json:"deliveryToken"`
		Secret        string                       `json:"secret"`
		InboundPath   string                       `json:"inboundPath"`
	}
	res := requestJSONAuthed[created](t, h, owner, "POST", "/api/projects/wh-project/webhooks", map[string]any{
		"name": "CI hooks", "authMode": "hmac", "autoDispatch": false,
		"titleTemplate": "{{title}}", "descriptionTemplate": "{{body}}",
		"assigneeUserId": owner.ID, "defaultWorkflowId": "code-review-merge-auto",
	}, 201)
	if !strings.HasPrefix(res.DeliveryToken, "lfw_") || !strings.HasPrefix(res.Secret, "whsec_") {
		t.Fatalf("tokens: %+v", res)
	}
	if !strings.Contains(res.InboundPath, res.DeliveryToken) {
		t.Fatalf("path %q missing token", res.InboundPath)
	}
	listRaw, _ := json.Marshal(requestJSONAuthed[map[string]any](t, h, maint, "GET", "/api/projects/wh-project/webhooks", nil, 200))
	if bytes.Contains(listRaw, []byte(res.Secret)) || bytes.Contains(listRaw, []byte(res.DeliveryToken)) {
		t.Fatal("secret or full delivery token leaked in list")
	}

	path := "/api/webhooks/inbound/" + res.DeliveryToken
	body := []byte(`{"title":"From CI","body":"Please review","extra":1}`)
	post := func(headers map[string]string, payload []byte, want int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest("POST", path, bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Fatalf("want %d got %d body=%s", want, rec.Code, rec.Body.String())
		}
		return rec
	}
	sign := func(secret string, payload []byte) string {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(payload)
		return "sha256=" + hex.EncodeToString(mac.Sum(nil))
	}
	ts := fmt.Sprintf("%d", time.Now().Unix())
	good := map[string]string{
		"X-LoopForge-Timestamp": ts,
		"X-LoopForge-Nonce":     "nonce-1",
		"X-LoopForge-Signature": sign(res.Secret, body),
		"Idempotency-Key":       "idem-1",
	}
	post(map[string]string{"X-LoopForge-Timestamp": ts, "X-LoopForge-Nonce": "n-bad"}, body, 401)
	post(map[string]string{
		"X-LoopForge-Timestamp": ts, "X-LoopForge-Nonce": "n-bad2",
		"X-LoopForge-Signature": sign("wrong", body),
	}, body, 401)
	expired := post(map[string]string{
		"X-LoopForge-Timestamp": fmt.Sprintf("%d", time.Now().Add(-20*time.Minute).Unix()),
		"X-LoopForge-Nonce":     "n-old",
		"X-LoopForge-Signature": sign(res.Secret, body),
	}, body, 401)
	if !strings.Contains(expired.Body.String(), "timestamp_expired") && !strings.Contains(expired.Body.String(), "timestamp outside") {
		t.Fatalf("want timestamp_expired, got %s", expired.Body.String())
	}

	rec := post(good, body, 201)
	var first map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	issueID, _ := first["issueId"].(string)
	if issueID == "" {
		t.Fatalf("missing issue: %s", rec.Body.String())
	}
	issue, ok := s.Issue(issueID)
	if !ok || issue.ProjectID != "wh-project" || issue.Source != "webhook" || issue.Title != "From CI" {
		t.Fatalf("issue=%+v", issue)
	}
	if issue.ExternalRef == nil || issue.ExternalRef.Source != "webhook" {
		t.Fatalf("externalRef=%+v", issue.ExternalRef)
	}

	// Idempotency returns same issue and original HTTP status (201).
	rec2 := post(map[string]string{
		"X-LoopForge-Timestamp": fmt.Sprintf("%d", time.Now().Unix()),
		"X-LoopForge-Nonce":     "nonce-2",
		"X-LoopForge-Signature": sign(res.Secret, body),
		"Idempotency-Key":       "idem-1",
	}, body, 201)
	var second map[string]any
	_ = json.Unmarshal(rec2.Body.Bytes(), &second)
	if second["issueId"] != issueID {
		t.Fatalf("idempotency mismatch: %v vs %v", second, first)
	}

	// Replay nonce.
	post(map[string]string{
		"X-LoopForge-Timestamp": fmt.Sprintf("%d", time.Now().Unix()),
		"X-LoopForge-Nonce":     "nonce-1",
		"X-LoopForge-Signature": sign(res.Secret, body),
	}, body, 401)

	// Oversized payload.
	big := bytes.Repeat([]byte("a"), domain.DefaultWebhookBodyLimit+10)
	bigJSON := append([]byte(`{"title":"`), append(big, []byte(`"}`)...)...)
	post(map[string]string{
		"X-LoopForge-Timestamp": fmt.Sprintf("%d", time.Now().Unix()),
		"X-LoopForge-Nonce":     "nonce-big",
		"X-LoopForge-Signature": sign(res.Secret, bigJSON),
	}, bigJSON, 413)

	// Disable channel.
	requestJSONAuthed[map[string]any](t, h, owner, "PUT", "/api/projects/wh-project/webhooks/"+res.Channel.ID, map[string]any{"enabled": false}, 200)
	post(map[string]string{
		"X-LoopForge-Timestamp": fmt.Sprintf("%d", time.Now().Unix()),
		"X-LoopForge-Nonce":     "nonce-off",
		"X-LoopForge-Signature": sign(res.Secret, body),
	}, body, 403)

	// Re-enable + rotate secret; old secret works briefly, new works.
	requestJSONAuthed[map[string]any](t, h, owner, "PUT", "/api/projects/wh-project/webhooks/"+res.Channel.ID, map[string]any{"enabled": true}, 200)
	rot := requestJSONAuthed[map[string]any](t, h, owner, "POST", "/api/projects/wh-project/webhooks/"+res.Channel.ID+"/rotate-secret", map[string]any{}, 200)
	newSecret, _ := rot["secret"].(string)
	if newSecret == "" || newSecret == res.Secret {
		t.Fatalf("rotate: %+v", rot)
	}
	post(map[string]string{
		"X-LoopForge-Timestamp": fmt.Sprintf("%d", time.Now().Unix()),
		"X-LoopForge-Nonce":     "nonce-old-secret",
		"X-LoopForge-Signature": sign(res.Secret, body),
	}, body, 201)
	post(map[string]string{
		"X-LoopForge-Timestamp": fmt.Sprintf("%d", time.Now().Unix()),
		"X-LoopForge-Nonce":     "nonce-new-secret",
		"X-LoopForge-Signature": sign(newSecret, body),
	}, body, 201)

	// Token auth mode.
	tokCh := requestJSONAuthed[created](t, h, maint, "POST", "/api/projects/wh-project/webhooks", map[string]any{
		"name": "token channel", "authMode": "token",
	}, 201)
	tokPath := "/api/webhooks/inbound/" + tokCh.DeliveryToken
	tokBody := []byte(`{"summary":"token title"}`)
	req := httptest.NewRequest("POST", tokPath, bytes.NewReader(tokBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-LoopForge-Timestamp", fmt.Sprintf("%d", time.Now().Unix()))
	req.Header.Set("X-LoopForge-Nonce", "tok-nonce-1")
	req.Header.Set("X-LoopForge-Token", tokCh.Secret)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 201 {
		t.Fatalf("token auth: %d %s", rec.Code, rec.Body.String())
	}

	// Events list has no secret.
	ev := requestJSONAuthed[map[string]any](t, h, owner, "GET", "/api/projects/wh-project/webhooks/"+res.Channel.ID+"/events", nil, 200)
	evRaw, _ := json.Marshal(ev)
	if bytes.Contains(evRaw, []byte(res.Secret)) || bytes.Contains(evRaw, []byte(newSecret)) || bytes.Contains(evRaw, []byte("whsec_")) {
		t.Fatal("secret leaked in events")
	}
	if bytes.Contains(evRaw, []byte(res.DeliveryToken)) {
		t.Fatal("delivery token leaked in events")
	}

	// Auto-dispatch without workflow -> 502 but issue kept; idempotency still binds.
	ad := requestJSONAuthed[created](t, h, owner, "POST", "/api/projects/wh-project/webhooks", map[string]any{
		"name": "auto bad", "authMode": "hmac", "autoDispatch": true, "defaultWorkflowId": "",
	}, 201)
	adBody := []byte(`{"title":"auto"}`)
	adPath := "/api/webhooks/inbound/" + ad.DeliveryToken
	adReq := httptest.NewRequest("POST", adPath, bytes.NewReader(adBody))
	adReq.Header.Set("Content-Type", "application/json")
	adReq.Header.Set("X-LoopForge-Timestamp", fmt.Sprintf("%d", time.Now().Unix()))
	adReq.Header.Set("X-LoopForge-Nonce", "auto-nonce")
	adReq.Header.Set("X-LoopForge-Signature", sign(ad.Secret, adBody))
	adReq.Header.Set("Idempotency-Key", "idem-502")
	adRec := httptest.NewRecorder()
	h.ServeHTTP(adRec, adReq)
	if adRec.Code != 502 {
		t.Fatalf("auto dispatch missing wf: %d %s", adRec.Code, adRec.Body.String())
	}
	var adResp map[string]any
	_ = json.Unmarshal(adRec.Body.Bytes(), &adResp)
	adIssue, _ := adResp["issueId"].(string)
	if adIssue == "" {
		t.Fatalf("issue should still be created: %s", adRec.Body.String())
	}
	if strings.Contains(adRec.Body.String(), ad.Secret) {
		t.Fatal("secret in error response")
	}
	adRetry := httptest.NewRequest("POST", adPath, bytes.NewReader(adBody))
	adRetry.Header.Set("Content-Type", "application/json")
	adRetry.Header.Set("X-LoopForge-Timestamp", fmt.Sprintf("%d", time.Now().Unix()))
	adRetry.Header.Set("X-LoopForge-Nonce", "auto-nonce-2")
	adRetry.Header.Set("X-LoopForge-Signature", sign(ad.Secret, adBody))
	adRetry.Header.Set("Idempotency-Key", "idem-502")
	adRetryRec := httptest.NewRecorder()
	h.ServeHTTP(adRetryRec, adRetry)
	if adRetryRec.Code != 502 {
		t.Fatalf("502 idempotency retry want 502 got %d %s", adRetryRec.Code, adRetryRec.Body.String())
	}
	var adRetryResp map[string]any
	_ = json.Unmarshal(adRetryRec.Body.Bytes(), &adRetryResp)
	if adRetryResp["issueId"] != adIssue {
		t.Fatalf("502 idempotency created duplicate: %v vs %s", adRetryResp, adIssue)
	}

	// Invalid JSON must not pin Idempotency-Key; retry with valid body succeeds.
	badBody := []byte(`{not-json`)
	badKey := "idem-bad-json"
	post(map[string]string{
		"X-LoopForge-Timestamp": fmt.Sprintf("%d", time.Now().Unix()),
		"X-LoopForge-Nonce":     "nonce-bad-json",
		"X-LoopForge-Signature": sign(newSecret, badBody),
		"Idempotency-Key":       badKey,
	}, badBody, 400)
	goodAfterBad := []byte(`{"title":"after bad json","body":"ok"}`)
	recAfterBad := post(map[string]string{
		"X-LoopForge-Timestamp": fmt.Sprintf("%d", time.Now().Unix()),
		"X-LoopForge-Nonce":     "nonce-after-bad-json",
		"X-LoopForge-Signature": sign(newSecret, goodAfterBad),
		"Idempotency-Key":       badKey,
	}, goodAfterBad, 201)
	var afterBad map[string]any
	if err := json.Unmarshal(recAfterBad.Body.Bytes(), &afterBad); err != nil {
		t.Fatal(err)
	}
	if afterBad["issueId"] == "" {
		t.Fatalf("expected issue after invalid-json retry: %s", recAfterBad.Body.String())
	}
}

func TestWebhookInboundRateLimitHTTP(t *testing.T) {
	t.Setenv("AGENTFLOW_AUTH_REQUIRED", "true")
	t.Setenv("AGENTFLOW_SESSION_SECRET", "webhook-rl-secret")
	t.Setenv("AGENTFLOW_SCHEDULER_ENABLED", "false")
	s, err := repo.OpenTest()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.PutUser(domain.User{LoginName: "wh-rl"}); err != nil {
		t.Fatal(err)
	}
	owner, _ := s.UserByLoginName("wh-rl")
	if err := s.PutProject(domain.Project{ID: "wh-rl", Name: "RL", OwnerID: owner.ID}); err != nil {
		t.Fatal(err)
	}
	h := New(s, engine.New(s, runtime.NewManager(filepath.Join(t.TempDir(), "workspaces"))), tracker.NewService(), "", "")
	type created struct {
		DeliveryToken string `json:"deliveryToken"`
		Secret        string `json:"secret"`
	}
	res := requestJSONAuthed[created](t, h, owner, "POST", "/api/projects/wh-rl/webhooks", map[string]any{
		"name": "rl", "authMode": "hmac",
	}, 201)
	sign := func(secret string, payload []byte) string {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(payload)
		return "sha256=" + hex.EncodeToString(mac.Sum(nil))
	}
	body := []byte(`{"title":"rl"}`)
	path := "/api/webhooks/inbound/" + res.DeliveryToken
	// Exhaust per-channel budget (rate limit runs before auth/body parse).
	var limited *httptest.ResponseRecorder
	for i := 0; i < webhookRateChannelLimit+1; i++ {
		req := httptest.NewRequest("POST", path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-LoopForge-Timestamp", fmt.Sprintf("%d", time.Now().Unix()))
		req.Header.Set("X-LoopForge-Nonce", fmt.Sprintf("rl-%d", i))
		req.Header.Set("X-LoopForge-Signature", sign(res.Secret, body))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code == 429 {
			limited = rec
			break
		}
	}
	if limited == nil {
		t.Fatal("expected HTTP 429 after channel rate budget")
	}
	if limited.Header().Get("Retry-After") == "" {
		t.Fatalf("missing Retry-After: %#v", limited.Header())
	}
}

func TestWebhookTriggerNodeTypeAccepted(t *testing.T) {
	if !domain.IsWebhookTriggerNode("webhook") || !domain.IsWebhookTriggerNode("webhook_inbound") {
		t.Fatal("expected webhook trigger node helpers")
	}
	if domain.IsWebhookTriggerNode("agent") {
		t.Fatal("agent is not a webhook trigger")
	}
}

// Ensure rate limiter rejects after budget (unit-level).
func TestWebhookRateLimiter(t *testing.T) {
	l := newWebhookRateLimiter()
	l.now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	for i := 0; i < webhookRateChannelLimit; i++ {
		if !l.allow("ch:x", webhookRateChannelLimit) {
			t.Fatalf("allowed early at %d", i)
		}
	}
	if l.allow("ch:x", webhookRateChannelLimit) {
		t.Fatal("expected reject")
	}
}
