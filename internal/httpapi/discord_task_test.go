package httpapi

import (
	"agentflow/internal/botinteraction"
	"agentflow/internal/domain"
	"context"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestHandleDiscordTaskCreatesIssueAndResolvesWorkflow(t *testing.T) {
	s, h, _ := botFixture(t)
	bot := domain.NotificationBot{
		ID: "b-discord", BotID: "app-discord", ProjectID: "p", OwnerID: 42, Channel: domain.BotChannelDiscord,
		Enabled: true, InteractionEnabled: true, Model: domain.DefaultNotificationReportModel,
		TenantExpected: "guild-1", DefaultWorkflowID: "wf-default",
		ChannelWorkflowIDs: map[string]string{"ch-special": "wf-channel"},
	}
	if err := s.SaveNotificationBot(bot, "discord-bot-token-TEST"); err != nil {
		t.Fatal(err)
	}
	bot, _ = s.NotificationBot(bot.ID)
	bindBotTestUser(t, s, bot, "discord-user-1", 43)
	for _, wf := range []domain.WorkflowDefinition{
		{ID: "wf-channel", Name: "频道 SOP", ProjectID: "p", SandboxProfileID: "test-native", Nodes: []domain.WorkflowNode{{ID: "n1", Name: "步骤", Type: "agent", AgentID: "dev", MaxAttempts: 1}}},
		{ID: "wf-default", Name: "默认 SOP", ProjectID: "p", SandboxProfileID: "test-native", Nodes: []domain.WorkflowNode{{ID: "n1", Name: "步骤", Type: "agent", AgentID: "dev", MaxAttempts: 1}}},
	} {
		if err := s.PutWorkflow(wf); err != nil {
			t.Fatal(err)
		}
	}
	user, ok := s.User(43)
	if !ok {
		t.Fatal("missing user")
	}
	conv := domain.BotConversation{ID: "conv-dc", SessionID: "sess", ChatID: "ch-special", ChatType: "group"}
	reply, handled := h.handleDiscordTask(context.Background(), bot, conv, domain.BotInteraction{
		SenderID: "discord-user-1", SenderName: "Alice", Content: "/task 修复登录页白屏",
	}, user)
	if !handled {
		t.Fatal("expected handled")
	}
	if !strings.Contains(reply, "已创建需求") && !strings.Contains(reply, "登录页") {
		t.Fatalf("reply = %q", reply)
	}
	var found domain.Issue
	for _, iss := range s.Issues() {
		if iss.ProjectID != "p" {
			continue
		}
		if strings.Contains(iss.Title, "登录页") || strings.Contains(iss.Description, "修复登录页白屏") {
			found = iss
			break
		}
	}
	if found.ID == "" {
		t.Fatalf("issue not created; reply=%q", reply)
	}
	if found.AssigneeUserID != 43 || found.Kind != "requirement" || found.Source != "local" {
		t.Fatalf("issue fields = %+v", found)
	}
	if found.WorkflowID != "wf-channel" {
		t.Fatalf("workflow = %q want wf-channel", found.WorkflowID)
	}
}

func TestHandleDiscordTaskIgnoresNonTask(t *testing.T) {
	_, h, _ := botFixture(t)
	bot := domain.NotificationBot{Channel: domain.BotChannelDiscord}
	user := domain.User{ID: 43, LoginName: "other-user"}
	if reply, handled := h.handleDiscordTask(context.Background(), bot, domain.BotConversation{}, domain.BotInteraction{Content: "hello"}, user); handled || reply != "" {
		t.Fatalf("non-task handled=%v reply=%q", handled, reply)
	}
	if reply, handled := h.handleDiscordTask(context.Background(), bot, domain.BotConversation{}, domain.BotInteraction{Content: "/task"}, user); !handled || !strings.Contains(reply, "用法") {
		t.Fatalf("empty task = handled=%v reply=%q", handled, reply)
	}
}

func TestFormatDiscordTaskProgressTruncates(t *testing.T) {
	long := strings.Repeat("进展", 1200)
	out := FormatDiscordTaskProgress([]string{long}, "https://example.com/run", "https://example.com/pr", "失败原因")
	if utf8.RuneCountInString(out) > botinteraction.DiscordMaxTextLength {
		t.Fatalf("len=%d", utf8.RuneCountInString(out))
	}
	short := FormatDiscordTaskProgress([]string{"已创建", "已启动"}, "https://r", "https://i", "")
	if !strings.Contains(short, "已创建") || !strings.Contains(short, "运行：https://r") || !strings.Contains(short, "链接：https://i") {
		t.Fatal(short)
	}
}
