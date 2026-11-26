package botinteraction

import (
	"agentflow/internal/domain"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Discord interaction / gateway constants.
const (
	DiscordInteractionPing               = 1
	DiscordInteractionApplicationCommand = 2

	DiscordResponsePong                   = 1
	DiscordResponseChannelMessage         = 4
	DiscordResponseDeferredChannelMessage = 5
	DiscordResponseDeferredUpdateMessage  = 6
	DiscordMessageFlagEphemeral           = 64

	DiscordOpcodeDispatch       = 0
	DiscordOpcodeHeartbeat      = 1
	DiscordOpcodeIdentify       = 2
	DiscordOpcodeResume         = 6
	DiscordOpcodeReconnect      = 7
	DiscordOpcodeInvalidSession = 9
	DiscordOpcodeHello          = 10
	DiscordOpcodeHeartbeatACK   = 11

	DiscordIntentGuilds = 1 << 0

	// DiscordMaxTextLength is Discord's content limit for a single message.
	DiscordMaxTextLength = 2000

	discordRateLimitPerMinute = 10
	discordRateWindow         = time.Minute
	discordInteractionTTL     = 14 * time.Minute
)

// DiscordInteraction is the subset of an Interactions payload used by the adapter.
type DiscordInteraction struct {
	Type      int    `json:"type"`
	ID        string `json:"id"`
	Token     string `json:"token"`
	GuildID   string `json:"guild_id"`
	ChannelID string `json:"channel_id"`
	AppID     string `json:"application_id"`
	Member    *struct {
		User DiscordUser `json:"user"`
		Nick string      `json:"nick"`
	} `json:"member"`
	User *DiscordUser        `json:"user"`
	Data *DiscordCommandData `json:"data"`
}

// DiscordUser is a Discord user object subset.
type DiscordUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Global   string `json:"global_name"`
}

// DiscordCommandData is application-command interaction data.
type DiscordCommandData struct {
	Name    string                 `json:"name"`
	Options []DiscordCommandOption `json:"options"`
}

// DiscordCommandOption is one slash-command option.
type DiscordCommandOption struct {
	Name  string `json:"name"`
	Type  int    `json:"type"`
	Value any    `json:"value"`
}

// VerifyDiscordInteraction checks an Interactions HTTP request with Ed25519.
func VerifyDiscordInteraction(publicKeyHex, signatureHex, timestamp string, body []byte) bool {
	pub, err := hex.DecodeString(strings.TrimSpace(publicKeyHex))
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return false
	}
	sig, err := hex.DecodeString(strings.TrimSpace(signatureHex))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return false
	}
	msg := append([]byte(timestamp), body...)
	return ed25519.Verify(ed25519.PublicKey(pub), msg, sig)
}

// ParseDiscordInteraction decodes a Discord Interactions JSON body.
func ParseDiscordInteraction(body []byte) (DiscordInteraction, error) {
	var ix DiscordInteraction
	if err := json.Unmarshal(body, &ix); err != nil {
		return DiscordInteraction{}, err
	}
	return ix, nil
}

// DiscordSlashCommand extracts the slash command name and first string option.
func DiscordSlashCommand(ix DiscordInteraction) (name, arg string) {
	if ix.Data == nil {
		return "", ""
	}
	name = strings.ToLower(strings.TrimSpace(ix.Data.Name))
	for _, opt := range ix.Data.Options {
		switch v := opt.Value.(type) {
		case string:
			return name, strings.TrimSpace(v)
		}
	}
	return name, ""
}

// ChunkDiscordMessage splits s into chunks of at most limit runes (default 2000).
// Non-empty input never yields empty chunks.
func ChunkDiscordMessage(s string, limit int) []string {
	if limit <= 0 {
		limit = DiscordMaxTextLength
	}
	if s == "" {
		return nil
	}
	if utf8.RuneCountInString(s) <= limit {
		return []string{s}
	}
	runes := []rune(s)
	out := make([]string, 0, (len(runes)/limit)+1)
	for len(runes) > 0 {
		n := limit
		if n > len(runes) {
			n = len(runes)
		}
		out = append(out, string(runes[:n]))
		runes = runes[n:]
	}
	return out
}

// MaskDiscordToken returns a masked hint (•••• + last 4 runes) for API display.
func MaskDiscordToken(token string) string {
	r := []rune(token)
	if len(r) < 4 {
		return "••••"
	}
	return "••••" + string(r[len(r)-4:])
}

// RedactDiscordSecrets replaces occurrences of token in s.
func RedactDiscordSecrets(s string, token string) string {
	if token == "" || !strings.Contains(s, token) {
		return s
	}
	return strings.ReplaceAll(s, token, MaskDiscordToken(token))
}

// DiscordAllowed checks guild allowlist (TenantExpected/TenantKey) and channel allowlist.
// An empty AllowedChannelIDs list means every channel in the allowed guild.
func DiscordAllowed(guildID, channelID string, bot domain.NotificationBot) (ok bool, reason string) {
	allowedGuild := strings.TrimSpace(bot.TenantExpected)
	if allowedGuild == "" {
		allowedGuild = strings.TrimSpace(bot.TenantKey)
	}
	if allowedGuild != "" && guildID != allowedGuild {
		return false, "guild_not_allowed"
	}
	if len(bot.AllowedChannelIDs) == 0 {
		return true, ""
	}
	for _, id := range bot.AllowedChannelIDs {
		if id == channelID {
			return true, ""
		}
	}
	return false, "channel_not_allowed"
}

// DiscordRateLimiter caps commands per user (default 10 / minute).
type DiscordRateLimiter struct {
	mu     sync.Mutex
	hits   map[string][]time.Time
	limit  int
	window time.Duration
}

// NewDiscordRateLimiter builds a limiter; zero limit/window use defaults.
func NewDiscordRateLimiter(limit int, window time.Duration) *DiscordRateLimiter {
	if limit <= 0 {
		limit = discordRateLimitPerMinute
	}
	if window <= 0 {
		window = discordRateWindow
	}
	return &DiscordRateLimiter{hits: map[string][]time.Time{}, limit: limit, window: window}
}

// Allow reports whether userID may issue another command at now.
func (r *DiscordRateLimiter) Allow(userID string, now time.Time) bool {
	if r == nil {
		return true
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.hits == nil {
		r.hits = map[string][]time.Time{}
	}
	cutoff := now.Add(-r.window)
	kept := r.hits[userID][:0]
	for _, t := range r.hits[userID] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= r.limit {
		r.hits[userID] = kept
		return false
	}
	r.hits[userID] = append(kept, now)
	return true
}

// discordTextLimit is the outbound chunk size, capped by Discord and the channel text limit.
func discordTextLimit() int {
	if channelTextLimit > 0 && channelTextLimit < DiscordMaxTextLength {
		return channelTextLimit
	}
	return DiscordMaxTextLength
}

// discordSender returns the interaction user id and display name.
func discordSender(ix DiscordInteraction) (id, name string) {
	if ix.Member != nil && ix.Member.User.ID != "" {
		id = ix.Member.User.ID
		name = strings.TrimSpace(ix.Member.Nick)
		if name == "" {
			name = discordUserName(ix.Member.User)
		}
		return id, name
	}
	if ix.User != nil {
		return ix.User.ID, discordUserName(*ix.User)
	}
	return "", ""
}

func discordUserName(u DiscordUser) string {
	if n := strings.TrimSpace(u.Global); n != "" {
		return n
	}
	return strings.TrimSpace(u.Username)
}
