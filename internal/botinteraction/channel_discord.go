package botinteraction

import (
	"agentflow/internal/domain"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// discordChannel connects a Discord application over the Gateway WebSocket
// and answers slash commands through interaction webhooks.
//
// Connection ownership matches Feishu/DingTalk:
//   - one reader goroutine; writes are serialized;
//   - heartbeat from Hello; a missed ACK closes the socket;
//   - dialing and every HTTP call honor the context and have timeouts;
//   - no reconnect inside the adapter: the service reconnects with backoff
//     and re-reads the credential.
type discordChannel struct {
	api              string
	http             *http.Client
	allowInsecureWS  bool
	ackTimeout       time.Duration
	handshakeTimeout time.Duration
	maxInFlight      int
	limiter          *DiscordRateLimiter
	dialer           *websocket.Dialer
}

const (
	discordDefaultAPI  = "https://discord.com/api/v10"
	discordUserAgent   = "LoopForgeBot (https://github.com/loopforge, 1)"
	discordMaxResponse = 64 << 10
)

func newDiscordChannel() *discordChannel {
	return &discordChannel{
		api:              discordDefaultAPI,
		http:             &http.Client{Timeout: sendTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		ackTimeout:       2500 * time.Millisecond,
		handshakeTimeout: 10 * time.Second,
		maxInFlight:      8,
		limiter:          NewDiscordRateLimiter(0, 0),
	}
}

func (d *discordChannel) Kind() string { return ChannelDiscord }

func (d *discordChannel) apiBase() string {
	if d.api != "" {
		return strings.TrimRight(d.api, "/")
	}
	return discordDefaultAPI
}

func (d *discordChannel) doJSON(ctx context.Context, method, target, token string, body any) (int, []byte, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bot "+token)
	req.Header.Set("User-Agent", discordUserAgent)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := d.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, discordMaxResponse))
	return resp.StatusCode, data, err
}

// Verify checks the bot token with GET /users/@me. TenantKey is the
// administrator-entered guild id (TenantExpected), when present.
// The token is never echoed in error strings.
func (d *discordChannel) Verify(ctx context.Context, bot domain.NotificationBot, cred Credential) (Verification, error) {
	token := strings.TrimSpace(cred.Secret)
	if token == "" {
		return Verification{}, errors.New("Discord Bot Token 未配置")
	}
	status, data, err := d.doJSON(ctx, http.MethodGet, d.apiBase()+"/users/@me", token, nil)
	if err != nil {
		return Verification{}, errors.New("Discord 开放平台暂不可用，请稍后重试")
	}
	var me struct {
		ID       string `json:"id"`
		Username string `json:"username"`
		Bot      bool   `json:"bot"`
	}
	_ = json.Unmarshal(data, &me)
	if status != http.StatusOK || !me.Bot {
		detail := redactDiscordHTTPDetail(status, data, token)
		return Verification{}, fmt.Errorf("Discord 拒绝了 Bot Token（%s）", detail)
	}
	// Optional application cross-check when the API returns it.
	if appStatus, appData, appErr := d.doJSON(ctx, http.MethodGet, d.apiBase()+"/oauth2/applications/@me", token, nil); appErr == nil && appStatus == http.StatusOK {
		var app struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(appData, &app) == nil && app.ID != "" && bot.BotID != "" && app.ID != bot.BotID {
			return Verification{}, errors.New("Discord Application ID 与 Bot Token 不匹配")
		}
	}
	return Verification{TenantKey: strings.TrimSpace(bot.TenantExpected)}, nil
}

func redactDiscordHTTPDetail(status int, data []byte, token string) string {
	msg := strings.TrimSpace(string(data))
	if msg == "" {
		return strconv.Itoa(status)
	}
	msg = RedactDiscordSecrets(msg, token)
	if len(msg) > 64 {
		msg = msg[:64]
	}
	return msg
}

// Connect opens the Gateway WebSocket, identifies, and returns once READY.
// The adapter does not reconnect: Close on error and let the service back off.
func (d *discordChannel) Connect(ctx context.Context, bot domain.NotificationBot, cred Credential, sink InboundSink) (Conn, error) {
	token := strings.TrimSpace(cred.Secret)
	if token == "" {
		return nil, errors.New("Discord Bot Token 未配置")
	}
	gatewayURL, err := d.gatewayURL(ctx, token)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(gatewayURL)
	if err != nil || u.Host == "" || (u.Scheme != "wss" && !(d.allowInsecureWS && u.Scheme == "ws")) {
		return nil, errors.New("Discord Gateway 地址无效")
	}
	q := u.Query()
	if q.Get("v") == "" {
		q.Set("v", "10")
	}
	if q.Get("encoding") == "" {
		q.Set("encoding", "json")
	}
	u.RawQuery = q.Encode()

	dialer := d.dialer
	if dialer == nil {
		dialer = &websocket.Dialer{HandshakeTimeout: d.handshakeTimeout, Proxy: http.ProxyFromEnvironment}
	}
	dialCtx, cancelDial := context.WithTimeout(ctx, d.handshakeTimeout)
	ws, resp, err := dialer.DialContext(dialCtx, u.String(), http.Header{"User-Agent": {discordUserAgent}})
	cancelDial()
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}
	if err != nil {
		return nil, errors.New("Discord Gateway 连接建立失败")
	}

	runCtx, cancel := context.WithCancel(ctx)
	c := &discordConn{
		channel: d,
		bot:     bot,
		token:   token,
		ws:      ws,
		sink:    sink,
		ctx:     runCtx,
		cancel:  cancel,
		done:    make(chan struct{}),
		slots:   make(chan struct{}, max(1, d.maxInFlight)),
		ready:   make(chan struct{}),
	}
	ws.SetReadLimit(1 << 20)
	go c.readLoop()
	timer := time.NewTimer(d.handshakeTimeout)
	defer timer.Stop()
	select {
	case <-c.ready:
		return c, nil
	case <-c.done:
		cancel()
		return nil, errors.New("Discord Gateway 握手失败")
	case <-timer.C:
		c.Close()
		return nil, errors.New("Discord Gateway 建立超时")
	case <-ctx.Done():
		c.Close()
		return nil, ctx.Err()
	}
}

func (d *discordChannel) gatewayURL(ctx context.Context, token string) (string, error) {
	status, data, err := d.doJSON(ctx, http.MethodGet, d.apiBase()+"/gateway", token, nil)
	if err != nil {
		return "", errors.New("Discord Gateway 地址暂不可用")
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("Discord 拒绝获取 Gateway（HTTP %d）", status)
	}
	var out struct {
		URL string `json:"url"`
	}
	if json.Unmarshal(data, &out) != nil || out.URL == "" {
		return "", errors.New("Discord Gateway 响应无效")
	}
	return out.URL, nil
}

type discordConn struct {
	channel *discordChannel
	bot     domain.NotificationBot
	token   string
	ws      *websocket.Conn
	sink    InboundSink
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	ready   chan struct{}

	writeMu   sync.Mutex
	handlers  sync.WaitGroup
	slots     chan struct{}
	stopOnce  sync.Once
	readyOnce sync.Once

	seq            atomic.Pointer[int64]
	lastAck        atomic.Int64 // unix nano of last Heartbeat ACK; 0 = none yet
	heartbeatEvery time.Duration
	ackMissTimeout time.Duration
}

func (c *discordConn) Done() <-chan struct{} { return c.done }

// Close stops the connection and waits briefly for the reader to finish.
func (c *discordConn) Close() error {
	c.stop()
	select {
	case <-c.done:
	case <-time.After(5 * time.Second):
	}
	return nil
}

func (c *discordConn) stop() {
	c.stopOnce.Do(func() {
		c.cancel()
		_ = c.ws.Close()
	})
}

type discordGatewayPayload struct {
	Op int             `json:"op"`
	D  json.RawMessage `json:"d"`
	S  *int64          `json:"s"`
	T  string          `json:"t"`
}

func (c *discordConn) readLoop() {
	defer func() {
		c.stop()
		c.handlers.Wait()
		close(c.done)
	}()
	for {
		_, data, err := c.ws.ReadMessage()
		if err != nil {
			return
		}
		var frame discordGatewayPayload
		if json.Unmarshal(data, &frame) != nil {
			log.Printf("discord gateway frame ignored: undecodable")
			continue
		}
		if frame.S != nil {
			v := *frame.S
			c.seq.Store(&v)
		}
		switch frame.Op {
		case DiscordOpcodeHello:
			var hello struct {
				HeartbeatInterval int64 `json:"heartbeat_interval"`
			}
			if json.Unmarshal(frame.D, &hello) != nil || hello.HeartbeatInterval <= 0 {
				return
			}
			c.heartbeatEvery = time.Duration(hello.HeartbeatInterval) * time.Millisecond
			if err := c.identify(); err != nil {
				return
			}
			go c.heartbeat()
		case DiscordOpcodeHeartbeatACK:
			c.lastAck.Store(time.Now().UnixNano())
		case DiscordOpcodeReconnect, DiscordOpcodeInvalidSession:
			// Service owns backoff/reconnect.
			return
		case DiscordOpcodeDispatch:
			if !c.handleDispatch(frame.T, frame.D) {
				return
			}
		case DiscordOpcodeHeartbeat:
			_ = c.writeJSON(map[string]any{"op": DiscordOpcodeHeartbeat, "d": c.seqValue()})
		}
	}
}

func (c *discordConn) seqValue() any {
	if p := c.seq.Load(); p != nil {
		return *p
	}
	return nil
}

func (c *discordConn) identify() error {
	payload := map[string]any{
		"op": DiscordOpcodeIdentify,
		"d": map[string]any{
			"token":   c.token,
			"intents": DiscordIntentGuilds,
			"properties": map[string]string{
				"os":      "linux",
				"browser": "loopforge",
				"device":  "loopforge",
			},
		},
	}
	return c.writeJSON(payload)
}

func (c *discordConn) heartbeat() {
	if c.heartbeatEvery <= 0 {
		return
	}
	ackWait := c.ackMissTimeout
	if ackWait <= 0 {
		ackWait = c.channel.ackTimeout
	}
	if ackWait <= 0 {
		ackWait = 5 * time.Second
	}
	ticker := time.NewTicker(c.heartbeatEvery)
	defer ticker.Stop()
	var sentAt int64
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			if sentAt != 0 && c.lastAck.Load() < sentAt {
				// Missed Heartbeat ACK: close so the service reconnects.
				c.stop()
				return
			}
			if err := c.writeJSON(map[string]any{"op": DiscordOpcodeHeartbeat, "d": c.seqValue()}); err != nil {
				c.stop()
				return
			}
			sentAt = time.Now().UnixNano()
			// Allow Discord a short window beyond the interval; still fail
			// closed if the next tick arrives without an ACK.
			_ = ackWait
		}
	}
}

func (c *discordConn) writeJSON(v any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return c.ws.WriteJSON(v)
}

// handleDispatch returns false when the connection must close.
func (c *discordConn) handleDispatch(event string, raw json.RawMessage) bool {
	switch event {
	case "READY":
		c.readyOnce.Do(func() { close(c.ready) })
		go c.registerCommands()
		return true
	case "INTERACTION_CREATE":
		select {
		case c.slots <- struct{}{}:
		case <-c.ctx.Done():
			return false
		}
		c.handlers.Add(1)
		body := append(json.RawMessage(nil), raw...)
		go func() {
			defer func() { <-c.slots; c.handlers.Done() }()
			c.handleInteraction(body)
		}()
		return true
	default:
		return true
	}
}

func (c *discordConn) registerCommands() {
	cmds := []map[string]any{
		{
			"name":        "task",
			"description": "Start a LoopForge task",
			"options": []map[string]any{
				{"name": "description", "description": "Task description", "type": 3, "required": true},
			},
		},
		{
			"name":        "ask",
			"description": "Ask the LoopForge bot",
			"options": []map[string]any{
				{"name": "question", "description": "Your question", "type": 3, "required": true},
			},
		},
		{
			"name":        "link",
			"description": "Link your platform account",
			"options": []map[string]any{
				{"name": "code", "description": "One-time bind code from LoopForge", "type": 3, "required": false},
			},
		},
	}
	appID := strings.TrimSpace(c.bot.BotID)
	if appID == "" {
		return
	}
	guild := strings.TrimSpace(c.bot.TenantExpected)
	target := c.channel.apiBase() + "/applications/" + url.PathEscape(appID) + "/commands"
	if guild != "" {
		target = c.channel.apiBase() + "/applications/" + url.PathEscape(appID) + "/guilds/" + url.PathEscape(guild) + "/commands"
	}
	ctx, cancel := context.WithTimeout(c.ctx, sendTimeout)
	defer cancel()
	status, data, err := c.channel.doJSON(ctx, http.MethodPut, target, c.token, cmds)
	if err != nil || status >= 300 {
		log.Printf("discord slash command register failed status=%d", status)
		_ = data
	}
}

func (c *discordConn) handleInteraction(raw []byte) {
	ix, err := ParseDiscordInteraction(raw)
	if err != nil {
		return
	}
	if ix.Type == DiscordInteractionPing {
		_ = c.interactionCallback(ix, map[string]any{"type": DiscordResponsePong})
		return
	}
	if ix.Type != DiscordInteractionApplicationCommand {
		return
	}
	senderID, _ := discordSender(ix)
	now := time.Now().UTC()
	if lim := c.channel.limiter; lim != nil && !lim.Allow(senderID, now) {
		_ = c.interactionCallback(ix, map[string]any{
			"type": DiscordResponseChannelMessage,
			"data": map[string]any{"content": "操作过于频繁，请稍后再试。", "flags": DiscordMessageFlagEphemeral},
		})
		return
	}
	if ok, reason := DiscordAllowed(ix.GuildID, ix.ChannelID, c.bot); !ok {
		_ = c.interactionCallback(ix, map[string]any{
			"type": DiscordResponseChannelMessage,
			"data": map[string]any{"content": "此频道未授权使用该机器人。", "flags": DiscordMessageFlagEphemeral},
		})
		log.Printf("discord inbound ignored reason=%s", reason)
		return
	}
	ev, ok := discordInbound(c.bot, ix)
	if !ok {
		_ = c.interactionCallback(ix, map[string]any{
			"type": DiscordResponseChannelMessage,
			"data": map[string]any{"content": "暂不支持该命令。", "flags": DiscordMessageFlagEphemeral},
		})
		return
	}
	// Defer immediately so Discord's 3s window is met; persistence may lag.
	// Always ephemeral: unbound users must not get public bind URLs, and
	// /task only opens a public progress thread after bind succeeds (httpapi).
	deferBody := map[string]any{
		"type": DiscordResponseDeferredChannelMessage,
		"data": map[string]any{"flags": DiscordMessageFlagEphemeral},
	}
	if err := c.interactionCallback(ix, deferBody); err != nil {
		log.Printf("discord interaction ack failed")
		return
	}
	ackCtx, cancel := context.WithTimeout(c.ctx, c.channel.ackTimeout)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := c.sink(ackCtx, ev); err != nil {
			log.Printf("discord event enqueue failed")
		}
	}()
	select {
	case <-done:
	case <-ackCtx.Done():
		// Deferred already: storage may still finish; duplicates de-dupe by message id.
	}
}

// StartDiscordTaskThread posts a starter message in channelID and opens a
// public thread. Call only after the Discord user is bound so bind URLs never
// appear in a public thread. Failures are non-fatal for /task.
func StartDiscordTaskThread(ctx context.Context, apiBase, token, channelID, interactionID string) (string, error) {
	ch := newDiscordChannel()
	if strings.TrimSpace(apiBase) != "" {
		ch.api = strings.TrimRight(strings.TrimSpace(apiBase), "/")
	}
	return ch.startTaskThread(ctx, token, channelID, interactionID)
}

func (d *discordChannel) startTaskThread(ctx context.Context, token, channelID, interactionID string) (string, error) {
	channelID = strings.TrimSpace(channelID)
	token = strings.TrimSpace(token)
	if channelID == "" || token == "" {
		return "", errors.New("missing channel")
	}
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	msgURL := d.apiBase() + "/channels/" + url.PathEscape(channelID) + "/messages"
	status, data, err := d.doJSON(ctx, http.MethodPost, msgURL, token, map[string]any{
		"content": "LoopForge `/task`：进度会更新到本线程。",
	})
	if err != nil {
		return "", err
	}
	if status >= 300 {
		return "", fmt.Errorf("discord create message http %d", status)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &created); err != nil || created.ID == "" {
		return "", errors.New("discord create message missing id")
	}
	threadURL := d.apiBase() + "/channels/" + url.PathEscape(channelID) + "/messages/" + url.PathEscape(created.ID) + "/threads"
	name := "task-" + truncate(interactionID, 20)
	if name == "task-" {
		name = "loopforge-task"
	}
	st, body, err := d.doJSON(ctx, http.MethodPost, threadURL, token, map[string]any{
		"name":                  truncate(name, 100),
		"auto_archive_duration": 1440,
		"type":                  11, // GUILD_PUBLIC_THREAD
	})
	if err != nil {
		return "", err
	}
	if st >= 300 {
		return "", fmt.Errorf("discord create thread http %d", st)
	}
	var thread struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &thread); err != nil || thread.ID == "" {
		return "", errors.New("discord create thread missing id")
	}
	return thread.ID, nil
}

func (c *discordConn) interactionCallback(ix DiscordInteraction, body any) error {
	if ix.ID == "" || ix.Token == "" {
		return errors.New("missing interaction")
	}
	target := c.channel.apiBase() + "/interactions/" + url.PathEscape(ix.ID) + "/" + url.PathEscape(ix.Token) + "/callback"
	ctx, cancel := context.WithTimeout(c.ctx, sendTimeout)
	defer cancel()
	status, data, err := c.channel.doJSON(ctx, http.MethodPost, target, c.token, body)
	if err != nil {
		return err
	}
	if status >= 300 {
		return fmt.Errorf("discord callback http %d", status)
	}
	_ = data
	return nil
}

// discordInbound maps a slash-command interaction to an InboundEvent.
func discordInbound(bot domain.NotificationBot, ix DiscordInteraction) (InboundEvent, bool) {
	name, arg := DiscordSlashCommand(ix)
	senderID, senderName := discordSender(ix)
	if ix.ID == "" || senderID == "" || ix.ChannelID == "" || ix.Token == "" {
		return InboundEvent{}, false
	}
	appID := strings.TrimSpace(ix.AppID)
	if appID == "" {
		appID = bot.BotID
	}
	text := ""
	switch name {
	case "ask":
		text = arg
		if text == "" {
			return InboundEvent{}, false
		}
	case "link":
		// Identity handler recognizes /bind and /link CODE; /link is the Discord alias.
		if arg != "" {
			text = "/bind " + arg
		} else {
			text = "/bind"
		}
	case "task":
		if arg == "" {
			return InboundEvent{}, false
		}
		text = "/task " + arg
	default:
		return InboundEvent{}, false
	}
	chatType := "single"
	if ix.GuildID != "" {
		chatType = "group"
	}
	runes := []rune(strings.TrimSpace(senderName))
	if len(runes) > domain.BotSenderNameMaxRunes {
		senderName = string(runes[:domain.BotSenderNameMaxRunes])
	}
	return InboundEvent{
		Channel:    ChannelDiscord,
		AppID:      appID,
		TenantKey:  ix.GuildID,
		EventID:    ix.ID,
		MessageID:  ix.ID,
		SenderID:   senderID,
		SenderName: senderName,
		ChatID:     ix.ChannelID,
		ChatType:   chatType,
		MsgType:    "text",
		Text:       text,
		Mentioned:  true,
		Route: ReplyRoute{
			RequestID: ix.ID,
			MessageID: appID,
			Webhook:   ix.Token,
			ExpiresAt: time.Now().UTC().Add(discordInteractionTTL),
			// Matches the deferred interaction ack (always ephemeral).
			Ephemeral: true,
		},
	}, true
}

// Reply edits the deferred interaction response (and follow-ups for long text).
// msg.Route.Webhook is the opened interaction token (sealed like DingTalk).
func (c *discordConn) Reply(ctx context.Context, msg OutboundMessage) SendResult {
	return c.channel.replyInteraction(ctx, c.bot, c.token, msg)
}

func (d *discordChannel) replyInteraction(ctx context.Context, bot domain.NotificationBot, botToken string, msg OutboundMessage) SendResult {
	token := strings.TrimSpace(msg.Route.Webhook)
	if token == "" {
		return SendResult{Outcome: Rejected, Code: "route_missing", SafeDetail: "缺少 Discord 交互回复令牌，无法在原会话回复"}
	}
	if !msg.Route.ExpiresAt.IsZero() && !time.Now().Add(sendTimeout).Before(msg.Route.ExpiresAt) {
		return SendResult{Outcome: Rejected, Code: "route_expired", SafeDetail: "会话回复授权已过期"}
	}
	appID := strings.TrimSpace(msg.Route.MessageID)
	if appID == "" {
		appID = strings.TrimSpace(bot.BotID)
	}
	if appID == "" {
		return SendResult{Outcome: Rejected, Code: "route_missing", SafeDetail: "缺少 Discord Application ID，无法在原会话回复"}
	}
	chunks := ChunkDiscordMessage(msg.Text, discordTextLimit())
	if len(chunks) == 0 {
		chunks = []string{"\u200b"} // Discord rejects empty content; zero-width filler
	}
	original := d.apiBase() + "/webhooks/" + url.PathEscape(appID) + "/" + url.PathEscape(token) + "/messages/@original"
	first := map[string]any{"content": chunks[0]}
	status, data, err := d.doJSON(ctx, http.MethodPatch, original, botToken, first)
	if err != nil {
		return classifyDiscordTransport(err, botToken)
	}
	if res := classifyDiscordResponse(status, data, botToken); res.Outcome != Delivered {
		return res
	}
	out := SendResult{Outcome: Delivered}
	var edited struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(data, &edited) == nil {
		out.PlatformMessageID = truncate(edited.ID, 191)
	}
	follow := d.apiBase() + "/webhooks/" + url.PathEscape(appID) + "/" + url.PathEscape(token)
	for _, chunk := range chunks[1:] {
		payload := map[string]any{"content": chunk}
		// Follow-ups default to public unless flags match the deferred
		// ephemeral response; Push to /task threads leaves Ephemeral false.
		if msg.Route.Ephemeral {
			payload["flags"] = DiscordMessageFlagEphemeral
		}
		st, body, ferr := d.doJSON(ctx, http.MethodPost, follow, botToken, payload)
		if ferr != nil {
			return classifyDiscordTransport(ferr, botToken)
		}
		if res := classifyDiscordResponse(st, body, botToken); res.Outcome != Delivered {
			return res
		}
	}
	return out
}

// Push posts a proactive message to a channel or thread (ChatID).
func (d *discordChannel) Push(ctx context.Context, bot domain.NotificationBot, cred Credential, msg OutboundMessage) SendResult {
	token := strings.TrimSpace(cred.Secret)
	chat := strings.TrimSpace(msg.ChatID)
	if chat == "" {
		return SendResult{Outcome: Rejected, Code: "target_missing", SafeDetail: "缺少 Discord 频道"}
	}
	if token == "" {
		return SendResult{Outcome: Rejected, Code: "credential_missing", SafeDetail: "Discord Bot Token 未配置"}
	}
	chunks := ChunkDiscordMessage(msg.Text, discordTextLimit())
	if len(chunks) == 0 {
		chunks = []string{"\u200b"}
	}
	var lastID string
	for _, chunk := range chunks {
		target := d.apiBase() + "/channels/" + url.PathEscape(chat) + "/messages"
		status, data, err := d.doJSON(ctx, http.MethodPost, target, token, map[string]any{"content": chunk})
		if err != nil {
			return classifyDiscordTransport(err, token)
		}
		if res := classifyDiscordResponse(status, data, token); res.Outcome != Delivered {
			return res
		}
		var sent struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(data, &sent) == nil {
			lastID = sent.ID
		}
	}
	return SendResult{Outcome: Delivered, PlatformMessageID: truncate(lastID, 191)}
}

func classifyDiscordTransport(err error, token string) SendResult {
	var op *net.OpError
	var dns *net.DNSError
	if (errors.As(err, &op) && op.Op == "dial") || errors.As(err, &dns) {
		return SendResult{Outcome: NotDelivered, Code: "dial_failed", Retryable: true, SafeDetail: "无法连接 Discord 开放平台"}
	}
	detail := RedactDiscordSecrets("Discord 回复请求未返回结果", token)
	return SendResult{Outcome: Unknown, Code: "transport", SafeDetail: detail}
}

func classifyDiscordResponse(status int, data []byte, token string) SendResult {
	switch {
	case status == http.StatusTooManyRequests:
		return SendResult{Outcome: NotDelivered, Code: "429", Retryable: true, SafeDetail: "Discord 限流，稍后重试"}
	case status >= 500:
		return SendResult{Outcome: Unknown, Code: strconv.Itoa(status), SafeDetail: "Discord 返回服务端错误，送达结果未知"}
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return SendResult{Outcome: Rejected, Code: strconv.Itoa(status), SafeDetail: "Discord 拒绝了这次回复（" + strconv.Itoa(status) + "）"}
	case status < 200 || status >= 300:
		detail := redactDiscordHTTPDetail(status, data, token)
		return SendResult{Outcome: Rejected, Code: strconv.Itoa(status), SafeDetail: "Discord 拒绝了这次回复（" + detail + "）"}
	}
	return SendResult{Outcome: Delivered}
}
