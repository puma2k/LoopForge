package httpapi

import (
	"agentflow/internal/botinteraction"
	"agentflow/internal/domain"
	"agentflow/internal/notification"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestBotChannelOpenIncludesDiscord(t *testing.T) {
	if !botChannelOpen(domain.BotChannelDiscord) {
		t.Fatal("discord not open")
	}
	if botChannelName(domain.NotificationBot{Channel: domain.BotChannelDiscord}) != "Discord" {
		t.Fatal("display name")
	}
}

func TestDiscordBotSaveRequiresGuildAndMasksSecret(t *testing.T) {
	f := cvBNewHTTPFixture(t)
	nh := &notificationHandlers{deps: &deps{repo: f.s, engine: f.e}, service: notification.New(f.s)}
	nh.service.Interactions.RegisterChannel(stubChannel{botinteraction.ChannelDiscord})
	create := func(body map[string]any) (int, map[string]any) {
		rec := cvBDirectHandlerRequest(t, f, http.MethodPost, "/api/notification-bots", body, nh.bots, nil, true)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	base := func() map[string]any {
		return map[string]any{
			"projectId": "platform", "name": "Discord", "channel": "discord",
			"botId": "app-123", "secret": "good", "enabled": true, "interactionEnabled": true,
		}
	}
	if code, out := create(base()); code != 400 || out["code"] != "tenant_required" {
		t.Fatalf("missing guild = %d %v", code, out)
	}
	ok := base()
	ok["tenantKey"] = " guild-abc "
	ok["publicKey"] = "aabb"
	ok["allowedChannelIds"] = []string{" ch-1 ", "ch-1", "ch-2"}
	ok["defaultWorkflowId"] = "wf-1"
	ok["writeApproval"] = "confirm"
	code, out := create(ok)
	if code != 200 {
		t.Fatalf("create = %d %v", code, out)
	}
	if out["tenantExpected"] != "guild-abc" {
		t.Fatalf("tenantExpected = %v", out["tenantExpected"])
	}
	if out["secretHint"] != botinteraction.MaskDiscordToken("good") {
		t.Fatalf("secretHint = %v", out["secretHint"])
	}
	raw, _ := json.Marshal(out)
	if strings.Contains(string(raw), `"secret"`) && strings.Contains(string(raw), `"good"`) {
		t.Fatal("secret leaked in JSON", string(raw))
	}
	if secret, err := f.s.BotCredential(out["id"].(string)); err != nil || secret != "good" {
		t.Fatalf("stored secret = %q %v", secret, err)
	}
	stored, _ := f.s.NotificationBot(out["id"].(string))
	if len(stored.AllowedChannelIDs) != 2 || stored.AllowedChannelIDs[0] != "ch-1" || stored.PublicKey != "aabb" || stored.DefaultWorkflowID != "wf-1" {
		t.Fatalf("discord fields = %+v", stored)
	}
	auto := base()
	auto["tenantKey"], auto["writeApproval"] = "guild-2", "auto"
	if code, _ := create(auto); code != 400 {
		t.Fatalf("auto write on Discord = %d", code)
	}
}
