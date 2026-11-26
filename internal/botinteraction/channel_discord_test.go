package botinteraction

import (
	"agentflow/internal/domain"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/gorilla/websocket"
)

func TestVerifyDiscordInteraction(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"type":1}`)
	ts := "1234567890"
	msg := append([]byte(ts), body...)
	sig := ed25519.Sign(priv, msg)
	if !VerifyDiscordInteraction(hex.EncodeToString(pub), hex.EncodeToString(sig), ts, body) {
		t.Fatal("valid signature rejected")
	}
	if VerifyDiscordInteraction(hex.EncodeToString(pub), hex.EncodeToString(sig), "0", body) {
		t.Fatal("wrong timestamp accepted")
	}
	if VerifyDiscordInteraction(hex.EncodeToString(pub), hex.EncodeToString(make([]byte, 64)), ts, body) {
		t.Fatal("invalid signature accepted")
	}
	if VerifyDiscordInteraction("zz", hex.EncodeToString(sig), ts, body) {
		t.Fatal("bad public key accepted")
	}
}

func TestParseDiscordSlashCommand(t *testing.T) {
	body := []byte(`{
		"type":2,"id":"ix1","token":"tok","guild_id":"g1","channel_id":"c1","application_id":"app1",
		"member":{"user":{"id":"u1","username":"alice"},"nick":"Ali"},
		"data":{"name":"task","options":[{"name":"description","type":3,"value":" fix login "}]}
	}`)
	ix, err := ParseDiscordInteraction(body)
	if err != nil || ix.Type != 2 || ix.GuildID != "g1" || ix.Member.User.ID != "u1" {
		t.Fatalf("parse = %+v %v", ix, err)
	}
	name, arg := DiscordSlashCommand(ix)
	if name != "task" || arg != "fix login" {
		t.Fatalf("slash = %q %q", name, arg)
	}
	ask, _ := ParseDiscordInteraction([]byte(`{"type":2,"data":{"name":"ASK","options":[{"name":"question","type":3,"value":"status?"}]}}`))
	n, a := DiscordSlashCommand(ask)
	if n != "ask" || a != "status?" {
		t.Fatalf("ask = %q %q", n, a)
	}
	link, _ := ParseDiscordInteraction([]byte(`{"type":2,"data":{"name":"link"}}`))
	n, a = DiscordSlashCommand(link)
	if n != "link" || a != "" {
		t.Fatalf("link = %q %q", n, a)
	}
}

func TestChunkDiscordMessage(t *testing.T) {
	if ChunkDiscordMessage("", 2000) != nil {
		t.Fatal("empty")
	}
	short := "hello"
	if got := ChunkDiscordMessage(short, 2000); len(got) != 1 || got[0] != short {
		t.Fatalf("short = %#v", got)
	}
	long := strings.Repeat("字", 2500)
	got := ChunkDiscordMessage(long, 2000)
	if len(got) != 2 || utf8.RuneCountInString(got[0]) != 2000 || utf8.RuneCountInString(got[1]) != 500 {
		t.Fatalf("chunks = %d %d %d", len(got), utf8.RuneCountInString(got[0]), utf8.RuneCountInString(got[1]))
	}
	for _, c := range got {
		if c == "" {
			t.Fatal("empty chunk")
		}
	}
	if got := ChunkDiscordMessage("abc", 0); len(got) != 1 {
		t.Fatalf("default limit = %#v", got)
	}
}

func TestMaskAndRedactDiscordToken(t *testing.T) {
	token := "discord-bot-token-ABCD"
	masked := MaskDiscordToken(token)
	if strings.Contains(masked, token) || !strings.HasPrefix(masked, "••••") || !strings.HasSuffix(masked, "ABCD") {
		t.Fatalf("mask = %q", masked)
	}
	if MaskDiscordToken("ab") != "••••" {
		t.Fatal(MaskDiscordToken("ab"))
	}
	errBody := "invalid token discord-bot-token-ABCD in request"
	redacted := RedactDiscordSecrets(errBody, token)
	if strings.Contains(redacted, token) || !strings.Contains(redacted, "••••ABCD") {
		t.Fatalf("redact = %q", redacted)
	}
}

func TestDiscordAllowed(t *testing.T) {
	bot := domain.NotificationBot{TenantExpected: "guild-1", AllowedChannelIDs: []string{"ch-a", "ch-b"}}
	if ok, _ := DiscordAllowed("guild-1", "ch-a", bot); !ok {
		t.Fatal("expected allow")
	}
	if ok, reason := DiscordAllowed("guild-2", "ch-a", bot); ok || reason != "guild_not_allowed" {
		t.Fatalf("guild = %v %s", ok, reason)
	}
	if ok, reason := DiscordAllowed("guild-1", "ch-x", bot); ok || reason != "channel_not_allowed" {
		t.Fatalf("channel = %v %s", ok, reason)
	}
	open := domain.NotificationBot{TenantKey: "guild-1"}
	if ok, _ := DiscordAllowed("guild-1", "any", open); !ok {
		t.Fatal("empty channel list should allow all")
	}
}

func TestDiscordRateLimiter(t *testing.T) {
	lim := NewDiscordRateLimiter(3, time.Minute)
	now := time.Unix(1_700_000_000, 0).UTC()
	for i := 0; i < 3; i++ {
		if !lim.Allow("u1", now.Add(time.Duration(i)*time.Second)) {
			t.Fatalf("hit %d denied", i)
		}
	}
	if lim.Allow("u1", now.Add(4*time.Second)) {
		t.Fatal("4th should deny")
	}
	if !lim.Allow("u2", now) {
		t.Fatal("other user")
	}
	if !lim.Allow("u1", now.Add(time.Minute+time.Second)) {
		t.Fatal("window rolled")
	}
}

func TestDiscordInboundMapping(t *testing.T) {
	bot := domain.NotificationBot{ID: "cfg", Channel: domain.BotChannelDiscord, BotID: "app-1", TenantExpected: "guild-1"}
	ix := DiscordInteraction{
		Type: DiscordInteractionApplicationCommand, ID: "ix-9", Token: "itok",
		GuildID: "guild-1", ChannelID: "ch-1", AppID: "app-1",
		Member: &struct {
			User DiscordUser `json:"user"`
			Nick string      `json:"nick"`
		}{User: DiscordUser{ID: "u-9", Username: "bob"}, Nick: "Bobby"},
		Data: &DiscordCommandData{Name: "ask", Options: []DiscordCommandOption{{Name: "question", Type: 3, Value: "how?"}}},
	}
	ev, ok := discordInbound(bot, ix)
	if !ok || ev.Channel != ChannelDiscord || ev.AppID != "app-1" || ev.MessageID != "ix-9" || ev.EventID != "ix-9" ||
		ev.SenderID != "u-9" || ev.SenderName != "Bobby" || ev.ChatID != "ch-1" || ev.ChatType != "group" ||
		!ev.Mentioned || ev.MsgType != "text" || ev.Text != "how?" || ev.TenantKey != "guild-1" ||
		ev.Route.Webhook != "itok" || ev.Route.RequestID != "ix-9" || ev.Route.MessageID != "app-1" {
		t.Fatalf("ask = %+v", ev)
	}
	ix.Data = &DiscordCommandData{Name: "link"}
	if ev, ok = discordInbound(bot, ix); !ok || ev.Text != "/bind" {
		t.Fatalf("link = %+v", ev)
	}
	ix.Data = &DiscordCommandData{Name: "link", Options: []DiscordCommandOption{{Name: "code", Type: 3, Value: "BINDCODE1"}}}
	if ev, ok = discordInbound(bot, ix); !ok || ev.Text != "/bind BINDCODE1" {
		t.Fatalf("link code = %+v", ev)
	}
	ix.Data = &DiscordCommandData{Name: "task", Options: []DiscordCommandOption{{Name: "description", Type: 3, Value: "ship it"}}}
	if ev, ok = discordInbound(bot, ix); !ok || ev.Text != "/task ship it" {
		t.Fatalf("task = %+v", ev)
	}
	ix.GuildID = ""
	if ev, ok = discordInbound(bot, ix); !ok || ev.ChatType != "single" {
		t.Fatalf("dm = %+v", ev)
	}
	ix.Data = &DiscordCommandData{Name: "unknown"}
	if _, ok = discordInbound(bot, ix); ok {
		t.Fatal("unknown accepted")
	}
}

func TestDiscordVerifyHTTP(t *testing.T) {
	const secret = "BotTokenSecretVALUE"
	var sawAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/users/@me" {
			sawAuth = r.Header.Get("Authorization")
			if sawAuth != "Bot "+secret {
				presented := strings.TrimPrefix(sawAuth, "Bot ")
				w.WriteHeader(401)
				// Echo the presented credential so Verify must redact it.
				io.WriteString(w, `{"message":"401: Unauthorized `+presented+`","code":0}`)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"id": "bot-user", "username": "lf", "bot": true})
			return
		}
		if r.URL.Path == "/oauth2/applications/@me" {
			json.NewEncoder(w).Encode(map[string]any{"id": "app-1"})
			return
		}
		w.WriteHeader(404)
	}))
	t.Cleanup(srv.Close)
	ch := newDiscordChannel()
	ch.api = srv.URL
	bot := domain.NotificationBot{BotID: "app-1", Channel: domain.BotChannelDiscord, TenantExpected: "guild-1"}
	v, err := ch.Verify(context.Background(), bot, Credential{Secret: secret})
	if err != nil || v.TenantKey != "guild-1" {
		t.Fatalf("verify = %+v %v", v, err)
	}
	if !strings.HasPrefix(sawAuth, "Bot ") || strings.Contains(sawAuth, "••••") {
		t.Fatalf("auth header = %q", sawAuth)
	}
	_, err = ch.Verify(context.Background(), bot, Credential{Secret: "wrong-token-ZZ99"})
	if err == nil {
		t.Fatal("wrong token accepted")
	}
	if strings.Contains(err.Error(), "wrong-token-ZZ99") || strings.Contains(err.Error(), secret) {
		t.Fatalf("token leaked in error: %v", err)
	}
}

func TestDiscordVerifyRedactsTokenInErrorBody(t *testing.T) {
	const secret = "super-secret-token-XYZ1"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		io.WriteString(w, `{"message":"bad token `+secret+`"}`)
	}))
	t.Cleanup(srv.Close)
	ch := newDiscordChannel()
	ch.api = srv.URL
	_, err := ch.Verify(context.Background(), domain.NotificationBot{BotID: "app"}, Credential{Secret: secret})
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("token in error: %v", err)
	}
	if !strings.Contains(err.Error(), "••••XYZ1") {
		t.Fatalf("expected masked hint: %v", err)
	}
}

func TestDiscordReplyChunks(t *testing.T) {
	var mu sync.Mutex
	var patches, posts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			Content string `json:"content"`
		}
		_ = json.Unmarshal(body, &payload)
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.Method == http.MethodPatch && strings.HasSuffix(r.URL.Path, "/messages/@original"):
			patches = append(patches, payload.Content)
			json.NewEncoder(w).Encode(map[string]any{"id": "m-orig"})
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/webhooks/"):
			posts = append(posts, payload.Content)
			json.NewEncoder(w).Encode(map[string]any{"id": "m-follow"})
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	ch := newDiscordChannel()
	ch.api = srv.URL
	long := strings.Repeat("a", 2500)
	res := ch.replyInteraction(context.Background(), domain.NotificationBot{BotID: "app-1"}, "bot-tok", OutboundMessage{
		Text:  long,
		Route: ReplyRoute{Webhook: "ix-tok", MessageID: "app-1", ExpiresAt: time.Now().Add(time.Hour)},
	})
	if res.Outcome != Delivered || res.PlatformMessageID != "m-orig" {
		t.Fatalf("reply = %+v", res)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(patches) != 1 || utf8.RuneCountInString(patches[0]) != 2000 {
		t.Fatalf("patches = %#v", patches)
	}
	if len(posts) != 1 || utf8.RuneCountInString(posts[0]) != 500 {
		t.Fatalf("posts = %#v", posts)
	}
	joined := patches[0] + posts[0]
	if joined != long {
		t.Fatal("chunk content mismatch")
	}
}

func TestDiscordPush(t *testing.T) {
	var gotAuth, gotPath, gotContent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		var body struct {
			Content string `json:"content"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		gotContent = body.Content
		json.NewEncoder(w).Encode(map[string]any{"id": "msg-1"})
	}))
	t.Cleanup(srv.Close)
	ch := newDiscordChannel()
	ch.api = srv.URL
	res := ch.Push(context.Background(), domain.NotificationBot{BotID: "app"}, Credential{Secret: "tok"}, OutboundMessage{ChatID: "ch-9", Text: "hello"})
	if res.Outcome != Delivered || res.PlatformMessageID != "msg-1" {
		t.Fatalf("push = %+v", res)
	}
	if gotAuth != "Bot tok" || gotPath != "/channels/ch-9/messages" || gotContent != "hello" {
		t.Fatalf("req auth=%q path=%q content=%q", gotAuth, gotPath, gotContent)
	}
}

func TestDiscordConnectInteractionFlow(t *testing.T) {
	var upgrader websocket.Upgrader
	interactions := make(chan map[string]any, 8)
	identifyOK := make(chan struct{}, 1)
	holdWS := make(chan struct{})

	wsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		_ = c.WriteJSON(map[string]any{"op": DiscordOpcodeHello, "d": map[string]any{"heartbeat_interval": 60000}})
		_, raw, err := c.ReadMessage()
		if err != nil {
			return
		}
		var identify struct {
			Op int `json:"op"`
			D  struct {
				Token   string `json:"token"`
				Intents int    `json:"intents"`
			} `json:"d"`
		}
		_ = json.Unmarshal(raw, &identify)
		if identify.Op == DiscordOpcodeIdentify && identify.D.Token == "bot-secret" && identify.D.Intents == DiscordIntentGuilds {
			identifyOK <- struct{}{}
		}
		_ = c.WriteJSON(map[string]any{"op": DiscordOpcodeDispatch, "t": "READY", "s": 1, "d": map[string]any{"session_id": "s1"}})
		ix := map[string]any{
			"type": 2, "id": "ix-flow", "token": "cb-tok", "guild_id": "guild-1", "channel_id": "ch-1",
			"application_id": "app-1",
			"member":         map[string]any{"user": map[string]any{"id": "u1", "username": "alice"}},
			"data":           map[string]any{"name": "ask", "options": []map[string]any{{"name": "question", "type": 3, "value": "ping"}}},
		}
		rawIX, _ := json.Marshal(ix)
		_ = c.WriteJSON(map[string]any{"op": DiscordOpcodeDispatch, "t": "INTERACTION_CREATE", "s": 2, "d": json.RawMessage(rawIX)})
		<-holdWS
	}))
	t.Cleanup(func() { close(holdWS); wsSrv.Close() })

	apiMux := http.NewServeMux()
	apiMux.HandleFunc("/gateway", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"url": "ws" + strings.TrimPrefix(wsSrv.URL, "http")})
	})
	apiMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/interactions/") {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			interactions <- body
			w.WriteHeader(204)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/applications/") {
			io.WriteString(w, "[]")
			return
		}
		w.WriteHeader(404)
	})
	api := httptest.NewServer(apiMux)
	t.Cleanup(api.Close)

	ch := newDiscordChannel()
	ch.api = api.URL
	ch.allowInsecureWS = true
	ch.ackTimeout = 500 * time.Millisecond
	ch.handshakeTimeout = 2 * time.Second
	ch.limiter = NewDiscordRateLimiter(100, time.Minute)

	bot := domain.NotificationBot{ID: "cfg", Channel: domain.BotChannelDiscord, BotID: "app-1", TenantExpected: "guild-1"}
	got := make(chan InboundEvent, 1)
	conn, err := ch.Connect(context.Background(), bot, Credential{Secret: "bot-secret"}, func(_ context.Context, ev InboundEvent) (InboundAck, error) {
		got <- ev
		return InboundAck{Accepted: true}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	select {
	case <-identifyOK:
	case <-time.After(2 * time.Second):
		t.Fatal("identify timeout")
	}
	select {
	case body := <-interactions:
		if int(body["type"].(float64)) != DiscordResponseDeferredChannelMessage {
			t.Fatalf("ack = %#v", body)
		}
		data, _ := body["data"].(map[string]any)
		if int(data["flags"].(float64)) != DiscordMessageFlagEphemeral {
			t.Fatalf("flags = %#v", data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("interaction ack timeout")
	}
	select {
	case ev := <-got:
		if ev.Text != "ping" || ev.SenderID != "u1" || ev.Route.Webhook != "cb-tok" {
			t.Fatalf("event = %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("sink timeout")
	}
}

func TestStartDiscordTaskThread(t *testing.T) {
	var mu sync.Mutex
	var posts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		posts = append(posts, r.Method+" "+r.URL.Path)
		mu.Unlock()
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bot ") {
			t.Errorf("bad auth header shape: %q", auth)
		}
		if strings.Contains(string(mustRead(r)), "SECRETTOKEN") {
			t.Fatal("token leaked into body")
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/channels/ch-1/messages":
			io.WriteString(w, `{"id":"msg-1"}`)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/threads"):
			io.WriteString(w, `{"id":"thread-9"}`)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	id, err := StartDiscordTaskThread(context.Background(), srv.URL, "SECRETTOKEN", "ch-1", "ix-long-id-12345")
	if err != nil || id != "thread-9" {
		t.Fatalf("id=%q err=%v posts=%v", id, err, posts)
	}
}

func mustRead(r *http.Request) []byte {
	b, _ := io.ReadAll(r.Body)
	return b
}
