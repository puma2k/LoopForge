package domain

import (
	"strings"
	"testing"
	"time"
)

func TestRenderWebhookTemplateDotPath(t *testing.T) {
	payload := map[string]any{
		"title": "Hello",
		"issue": map[string]any{"title": "Nested"},
	}
	if got := RenderWebhookTemplate("{{title}} / {{.issue.title}}", payload); got != "Hello / Nested" {
		t.Fatalf("got %q", got)
	}
	if got := RenderWebhookTemplate("{{payload.title}}", payload); got != "Hello" {
		t.Fatalf("payload prefix: %q", got)
	}
	if got := RenderWebhookTemplate("{{missing}}", payload); got != "" {
		t.Fatalf("missing should be empty, got %q", got)
	}
}

func TestMapWebhookTitleDescriptionFallbacks(t *testing.T) {
	ch := ProjectWebhookChannel{Name: "CI"}
	title, desc := MapWebhookTitleDescription(ch, map[string]any{"summary": "Ship it", "body": "details"}, time.Unix(0, 0).UTC())
	if title != "Ship it" {
		t.Fatalf("title=%q", title)
	}
	if desc != "details" {
		t.Fatalf("desc=%q", desc)
	}
	title, desc = MapWebhookTitleDescription(ch, map[string]any{"x": 1}, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	if !strings.Contains(title, "CI") || !strings.Contains(title, "2026") {
		t.Fatalf("default title=%q", title)
	}
	if !strings.Contains(desc, "Webhook payload") {
		t.Fatalf("default desc=%q", desc)
	}
}

func TestFilterWebhookHeadersStripsSecrets(t *testing.T) {
	h := FilterWebhookHeaders(map[string][]string{
		"Content-Type":          {"application/json"},
		"Authorization":         {"Bearer secret"},
		"X-LoopForge-Signature": {"sha256=abc"},
		"X-LoopForge-Token":     {"tok"},
		"X-Custom":              {"ok"},
	})
	if _, ok := h["Authorization"]; ok {
		t.Fatal("authorization leaked")
	}
	if _, ok := h["X-LoopForge-Signature"]; ok {
		t.Fatal("signature leaked")
	}
	if h["X-Custom"] != "ok" || h["Content-Type"] != "application/json" {
		t.Fatalf("got %#v", h)
	}
}
