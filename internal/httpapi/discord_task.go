package httpapi

import (
	"agentflow/internal/botinteraction"
	"agentflow/internal/domain"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const discordTaskDescMax = 2000
const discordTaskTitleMax = 80

// handleDiscordTask handles Discord "/task …" from a bound user: creates a
// local requirement and dispatches the resolved SOP when write policy allows.
// Non-task messages return handled=false so the normal model path continues.
func (h *assistantHandlers) handleDiscordTask(ctx context.Context, bot domain.NotificationBot, conv domain.BotConversation, item domain.BotInteraction, user domain.User) (string, bool) {
	cmd := botIdentityCommand(item.Content)
	desc, ok := discordTaskDescription(cmd)
	if !ok {
		return "", false
	}
	if desc == "" {
		return "用法：`/task 需求描述`\n会在当前项目创建需求并按频道/默认/项目 SOP 启动。描述当作普通文本，不会当命令执行。", true
	}
	if utf8.RuneCountInString(desc) > discordTaskDescMax {
		desc = truncateRunes(desc, discordTaskDescMax)
	}
	// SOP map keys are Discord channel IDs; keep conv.ChatID as the channel
	// (Gateway never rewrites it to a thread id before bind).
	channelID := strings.TrimSpace(conv.ChatID)
	workflowID := discordTaskWorkflowID(bot, channelID)
	if workflowID == "" {
		if project, ok := h.repo.Project(bot.ProjectID); ok {
			workflowID = strings.TrimSpace(project.WorkflowID)
		}
	}
	title := truncateRunes(desc, discordTaskTitleMax)

	switch bot.EffectiveWriteApproval() {
	case domain.BotWriteReadOnly:
		return "当前 Bot 为只读模式：`/task` 需要创建需求并启动 SOP。请到平台页面操作，或让管理员将写入策略改为「网页确认」或「自动执行」。", true
	case domain.BotWriteConfirm:
		body, err := json.Marshal(map[string]any{
			"projectId": bot.ProjectID, "title": title, "description": desc,
			"kind": "requirement", "source": "local", "workflowId": workflowID,
			"assigneeUserId": user.ID, "priority": "medium", "status": "todo",
		})
		if err != nil {
			return "准备需求草稿失败，请稍后重试或到平台页面手动创建。", true
		}
		head := "已准备创建需求「" + title + "」。"
		return head + h.confirmBotDrafts(ctx, bot, user, item, []assistantDraft{{Tool: "issues_save", Body: body}}), true
	}

	issue := domain.Issue{
		ID:             fmt.Sprintf("issue-%d", time.Now().UnixNano()),
		ProjectID:      bot.ProjectID,
		Title:          title,
		Description:    desc,
		Status:         "todo",
		Priority:       "medium",
		Kind:           "requirement",
		Source:         "local",
		WorkflowID:     workflowID,
		AssigneeUserID: user.ID,
	}
	actor := strings.TrimSpace(user.ChineseName)
	if actor == "" {
		actor = user.LoginName
	}
	issue.AppendOperation(domain.IssueOperation{
		Actor: actor, ActorUserID: user.ID, Action: "create",
		Detail: "Discord /task 创建需求",
		ActorChannel: bot.ChannelKind(), ActorSender: item.SenderID, ActorSenderName: item.SenderName,
	})
	if err := h.repo.PutIssue(issue); err != nil {
		return "创建需求失败，请稍后重试或到平台页面手动创建。", true
	}
	stored, ok := h.repo.Issue(issue.ID)
	if !ok {
		stored = issue
	}
	progressChatID := channelID
	threadKey := strings.TrimSpace(item.PlatformEventID)
	if threadKey == "" {
		threadKey = item.ID
	}
	if threadID := h.discordOpenTaskThread(ctx, bot, channelID, threadKey); threadID != "" {
		progressChatID = threadID
	}
	origin := domain.BotChatOrigin{
		ConfigID: bot.ID, ProjectID: bot.ProjectID, ConversationID: conv.ID,
		ChatType: conv.ChatType, ChatID: progressChatID, SenderID: item.SenderID, UserID: user.ID,
	}
	h.recordBotOrigin(origin, true, stored.ID)
	task, err := dispatchIssue(h.deps, dispatchRequest{
		IssueID: stored.ID, WorkflowID: workflowID, TriggerUserID: user.ID, ChatOrigin: &origin,
	})
	label := h.botIssueTitle(stored)
	if err != nil {
		return "已创建需求 " + label + "，但启动 SOP 失败：" + truncateRunes(err.Error(), 200) + "。可在平台手动启动。", true
	}
	runURL, issueURL := discordTaskLinks(h, stored, task)
	steps := []string{"已创建需求 " + label, "已启动 SOP" + botRunLabel(task.RunID)}
	return FormatDiscordTaskProgress(steps, runURL, issueURL, ""), true
}

// discordOpenTaskThread creates a public progress thread after bind. Empty on
// any failure so /task still replies ephemerally in-channel.
func (h *assistantHandlers) discordOpenTaskThread(ctx context.Context, bot domain.NotificationBot, channelID, interactionID string) string {
	if h == nil || h.repo == nil || strings.TrimSpace(channelID) == "" {
		return ""
	}
	secret, err := h.repo.BotCredential(bot.ID)
	if err != nil || strings.TrimSpace(secret) == "" {
		return ""
	}
	threadID, err := botinteraction.StartDiscordTaskThread(ctx, "", secret, channelID, interactionID)
	if err != nil || threadID == "" {
		return ""
	}
	return threadID
}

func discordTaskDescription(cmd string) (desc string, ok bool) {
	switch {
	case cmd == "/task":
		return "", true
	case strings.HasPrefix(cmd, "/task "):
		return strings.TrimSpace(strings.TrimPrefix(cmd, "/task ")), true
	default:
		return "", false
	}
}

func discordTaskWorkflowID(bot domain.NotificationBot, chatID string) string {
	chatID = strings.TrimSpace(chatID)
	if chatID != "" && bot.ChannelWorkflowIDs != nil {
		if wf := strings.TrimSpace(bot.ChannelWorkflowIDs[chatID]); wf != "" {
			return wf
		}
	}
	return strings.TrimSpace(bot.DefaultWorkflowID)
}

func discordTaskLinks(h *assistantHandlers, issue domain.Issue, task domain.Task) (runURL, issueURL string) {
	base := ""
	if h.notifications != nil && h.notifications.service != nil {
		base = h.notifications.service.PublicAddress()
	}
	if !domain.UsablePublicURL(base) {
		return "", ""
	}
	base = strings.TrimRight(base, "/")
	if issue.ID != "" {
		issueURL = base + "/?view=issues&issue=" + issue.ID
	}
	if task.RunID != "" {
		runURL = base + "/?view=runs&run=" + task.RunID
	} else if task.ID != "" {
		runURL = base + "/?view=runs&task=" + task.ID
	}
	return runURL, issueURL
}

// FormatDiscordTaskProgress builds a Discord-safe progress reply (≤2000 runes).
func FormatDiscordTaskProgress(steps []string, runURL, prURL, errMsg string) string {
	var b strings.Builder
	for _, step := range steps {
		step = strings.TrimSpace(step)
		if step == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("• ")
		b.WriteString(step)
	}
	if u := strings.TrimSpace(runURL); u != "" {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("运行：")
		b.WriteString(u)
	}
	if u := strings.TrimSpace(prURL); u != "" {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("链接：")
		b.WriteString(u)
	}
	if msg := strings.TrimSpace(errMsg); msg != "" {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("错误：")
		b.WriteString(msg)
	}
	out := b.String()
	if out == "" {
		out = "任务已受理。"
	}
	chunks := botinteraction.ChunkDiscordMessage(out, botinteraction.DiscordMaxTextLength)
	if len(chunks) == 0 {
		return ""
	}
	return chunks[0]
}
