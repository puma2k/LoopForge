package httpapi

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"time"

	"agentflow/internal/domain"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type webhooksHandlers struct{ *deps }

const webhookInboundPathPrefix = "/api/webhooks/inbound/"

func (h *webhooksHandlers) manageProject(c *gin.Context, projectID string) bool {
	user, ok := h.requestUser(c.Request, true)
	if !ok {
		c.JSON(401, gin.H{"error": "请先登录"})
		return false
	}
	project, ok := h.repo.Project(projectID)
	if !ok {
		c.JSON(404, gin.H{"error": "项目不存在"})
		return false
	}
	if h.isPlatformAdmin(c.Request) || (user.ID > 0 && project.OwnerID == user.ID) {
		return true
	}
	for _, member := range project.Members {
		if member.UserID == user.ID && member.Role == "maintainer" {
			return true
		}
	}
	c.JSON(403, gin.H{"error": "只有项目负责人、维护者或平台管理员可以管理 Webhook 渠道"})
	return false
}

func (h *webhooksHandlers) list(c *gin.Context) {
	projectID := strings.TrimSpace(c.Param("id"))
	if !h.manageProject(c, projectID) {
		return
	}
	channels, err := h.repo.WebhookChannels(projectID)
	if err != nil {
		respond(c, err)
		return
	}
	base := webhookPublicBase()
	for i := range channels {
		enrichWebhookChannelPaths(&channels[i], base, "")
	}
	c.JSON(200, gin.H{"channels": channels})
}

func enrichWebhookChannelPaths(ch *domain.ProjectWebhookChannel, base, fullToken string) {
	if fullToken != "" {
		ch.InboundPath = webhookInboundPathPrefix + fullToken
		if base != "" {
			ch.InboundURL = strings.TrimRight(base, "/") + ch.InboundPath
		}
		return
	}
	if ch.DeliveryTokenPrefix != "" {
		ch.InboundPath = webhookInboundPathPrefix + ch.DeliveryTokenPrefix + "…"
		if base != "" {
			ch.InboundURL = strings.TrimRight(base, "/") + ch.InboundPath
		}
	}
}

func webhookPublicBase() string {
	if s := strings.TrimSpace(os.Getenv("AGENTFLOW_PUBLIC_URL")); s != "" {
		return strings.TrimRight(s, "/")
	}
	return ""
}

func (h *webhooksHandlers) create(c *gin.Context) {
	projectID := strings.TrimSpace(c.Param("id"))
	if !h.manageProject(c, projectID) {
		return
	}
	var input struct {
		Name                string `json:"name"`
		Description         string `json:"description"`
		Enabled             *bool  `json:"enabled"`
		AuthMode            string `json:"authMode"`
		DefaultWorkflowID   string `json:"defaultWorkflowId"`
		AssigneeUserID      int64  `json:"assigneeUserId"`
		TitleTemplate       string `json:"titleTemplate"`
		DescriptionTemplate string `json:"descriptionTemplate"`
		AutoDispatch        bool   `json:"autoDispatch"`
		MaxBodyBytes        int    `json:"maxBodyBytes"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(400, gin.H{"error": "无效的 JSON"})
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || len(input.Name) > 128 {
		c.JSON(400, gin.H{"error": "名称须为 1–128 字符"})
		return
	}
	if input.MaxBodyBytes < 0 || input.MaxBodyBytes > domain.MaxWebhookBodyLimit {
		c.JSON(400, gin.H{"error": "maxBodyBytes 超出安全上限"})
		return
	}
	if input.AssigneeUserID > 0 && !h.assigneeInProject(projectID, input.AssigneeUserID) {
		c.JSON(400, gin.H{"error": "负责人须为本项目成员或 Owner"})
		return
	}
	deliveryToken, err := newWebhookDeliveryToken()
	if err != nil {
		respond(c, err)
		return
	}
	secret, err := newWebhookSecret()
	if err != nil {
		respond(c, err)
		return
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	now := time.Now().UTC()
	ch := domain.ProjectWebhookChannel{
		ID: "whc-" + randomID(), ProjectID: projectID, Name: input.Name, Description: strings.TrimSpace(input.Description),
		Enabled: enabled, DeliveryTokenPrefix: tokenPrefix(deliveryToken), AuthMode: domain.NormalizeWebhookAuthMode(input.AuthMode),
		DefaultWorkflowID: strings.TrimSpace(input.DefaultWorkflowID), AssigneeUserID: input.AssigneeUserID,
		TitleTemplate: input.TitleTemplate, DescriptionTemplate: input.DescriptionTemplate,
		AutoDispatch: input.AutoDispatch, MaxBodyBytes: input.MaxBodyBytes,
		SecretPrefix: tokenPrefix(secret), SecretVersion: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := h.repo.CreateWebhookChannel(ch, deliveryToken, secret); err != nil {
		respond(c, err)
		return
	}
	base := webhookPublicBase()
	enrichWebhookChannelPaths(&ch, base, deliveryToken)
	c.Header("Cache-Control", "no-store")
	c.JSON(201, gin.H{
		"channel":       ch,
		"deliveryToken": deliveryToken,
		"secret":        secret,
		"inboundURL":    ch.InboundURL,
		"inboundPath":   ch.InboundPath,
	})
}

func (h *webhooksHandlers) assigneeInProject(projectID string, userID int64) bool {
	project, ok := h.repo.Project(projectID)
	if !ok {
		return false
	}
	if project.OwnerID == userID {
		return true
	}
	for _, m := range project.Members {
		if m.UserID == userID {
			return true
		}
	}
	return false
}

func (h *webhooksHandlers) get(c *gin.Context) {
	projectID := strings.TrimSpace(c.Param("id"))
	channelID := strings.TrimSpace(c.Param("channelId"))
	if !h.manageProject(c, projectID) {
		return
	}
	ch, err := h.repo.WebhookChannel(channelID)
	if errors.Is(err, gorm.ErrRecordNotFound) || ch.ProjectID != projectID {
		c.JSON(404, gin.H{"error": "渠道不存在"})
		return
	}
	if err != nil {
		respond(c, err)
		return
	}
	enrichWebhookChannelPaths(&ch, webhookPublicBase(), "")
	c.JSON(200, gin.H{"channel": ch})
}

func (h *webhooksHandlers) update(c *gin.Context) {
	projectID := strings.TrimSpace(c.Param("id"))
	channelID := strings.TrimSpace(c.Param("channelId"))
	if !h.manageProject(c, projectID) {
		return
	}
	ch, err := h.repo.WebhookChannel(channelID)
	if errors.Is(err, gorm.ErrRecordNotFound) || ch.ProjectID != projectID {
		c.JSON(404, gin.H{"error": "渠道不存在"})
		return
	}
	if err != nil {
		respond(c, err)
		return
	}
	var input struct {
		Name                *string `json:"name"`
		Description         *string `json:"description"`
		Enabled             *bool   `json:"enabled"`
		AuthMode            *string `json:"authMode"`
		DefaultWorkflowID   *string `json:"defaultWorkflowId"`
		AssigneeUserID      *int64  `json:"assigneeUserId"`
		TitleTemplate       *string `json:"titleTemplate"`
		DescriptionTemplate *string `json:"descriptionTemplate"`
		AutoDispatch        *bool   `json:"autoDispatch"`
		MaxBodyBytes        *int    `json:"maxBodyBytes"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(400, gin.H{"error": "无效的 JSON"})
		return
	}
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" || len(name) > 128 {
			c.JSON(400, gin.H{"error": "名称须为 1–128 字符"})
			return
		}
		ch.Name = name
	}
	if input.Description != nil {
		ch.Description = strings.TrimSpace(*input.Description)
	}
	if input.Enabled != nil {
		ch.Enabled = *input.Enabled
	}
	if input.AuthMode != nil {
		ch.AuthMode = domain.NormalizeWebhookAuthMode(*input.AuthMode)
	}
	if input.DefaultWorkflowID != nil {
		ch.DefaultWorkflowID = strings.TrimSpace(*input.DefaultWorkflowID)
	}
	if input.AssigneeUserID != nil {
		if *input.AssigneeUserID > 0 && !h.assigneeInProject(projectID, *input.AssigneeUserID) {
			c.JSON(400, gin.H{"error": "负责人须为本项目成员或 Owner"})
			return
		}
		ch.AssigneeUserID = *input.AssigneeUserID
	}
	if input.TitleTemplate != nil {
		ch.TitleTemplate = *input.TitleTemplate
	}
	if input.DescriptionTemplate != nil {
		ch.DescriptionTemplate = *input.DescriptionTemplate
	}
	if input.AutoDispatch != nil {
		ch.AutoDispatch = *input.AutoDispatch
	}
	if input.MaxBodyBytes != nil {
		if *input.MaxBodyBytes < 0 || *input.MaxBodyBytes > domain.MaxWebhookBodyLimit {
			c.JSON(400, gin.H{"error": "maxBodyBytes 超出安全上限"})
			return
		}
		ch.MaxBodyBytes = *input.MaxBodyBytes
	}
	if err := h.repo.UpdateWebhookChannel(ch); err != nil {
		respond(c, err)
		return
	}
	stored, _ := h.repo.WebhookChannel(channelID)
	enrichWebhookChannelPaths(&stored, webhookPublicBase(), "")
	c.JSON(200, gin.H{"channel": stored})
}

func (h *webhooksHandlers) delete(c *gin.Context) {
	projectID := strings.TrimSpace(c.Param("id"))
	channelID := strings.TrimSpace(c.Param("channelId"))
	if !h.manageProject(c, projectID) {
		return
	}
	if err := h.repo.DeleteWebhookChannel(projectID, channelID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(404, gin.H{"error": "渠道不存在"})
			return
		}
		respond(c, err)
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (h *webhooksHandlers) rotateSecret(c *gin.Context) {
	projectID := strings.TrimSpace(c.Param("id"))
	channelID := strings.TrimSpace(c.Param("channelId"))
	if !h.manageProject(c, projectID) {
		return
	}
	ch, err := h.repo.WebhookChannel(channelID)
	if errors.Is(err, gorm.ErrRecordNotFound) || ch.ProjectID != projectID {
		c.JSON(404, gin.H{"error": "渠道不存在"})
		return
	}
	if err != nil {
		respond(c, err)
		return
	}
	secret, err := newWebhookSecret()
	if err != nil {
		respond(c, err)
		return
	}
	ver, prefix, err := h.repo.RotateWebhookSecret(channelID, secret, domain.WebhookSecretGrace)
	if err != nil {
		respond(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(200, gin.H{
		"secret":               secret,
		"secretPrefix":         prefix,
		"secretVersion":        ver,
		"previousSecretGrace":  domain.WebhookSecretGrace.String(),
		"previousSecretUntil":  time.Now().UTC().Add(domain.WebhookSecretGrace),
	})
}

func (h *webhooksHandlers) events(c *gin.Context) {
	projectID := strings.TrimSpace(c.Param("id"))
	channelID := strings.TrimSpace(c.Param("channelId"))
	if !h.manageProject(c, projectID) {
		return
	}
	ch, err := h.repo.WebhookChannel(channelID)
	if errors.Is(err, gorm.ErrRecordNotFound) || ch.ProjectID != projectID {
		c.JSON(404, gin.H{"error": "渠道不存在"})
		return
	}
	if err != nil {
		respond(c, err)
		return
	}
	events, err := h.repo.WebhookEvents(channelID, 50)
	if err != nil {
		respond(c, err)
		return
	}
	c.JSON(200, gin.H{"events": events})
}

func newWebhookDeliveryToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "lfw_" + hex.EncodeToString(raw), nil
}

func newWebhookSecret() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "whsec_" + base64.RawURLEncoding.EncodeToString(raw), nil
}
