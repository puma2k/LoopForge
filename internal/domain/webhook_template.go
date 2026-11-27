package domain

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var webhookTemplateToken = regexp.MustCompile(`\{\{\s*([^{}]+?)\s*\}\}`)

const webhookDescMaxRunes = 8000

// RenderWebhookTemplate expands {{path}} tokens from a JSON object (dot-path).
// Unknown paths become empty strings. Supports {{title}}, {{.issue.title}},
// {{payload.title}} (payload. prefix optional when root is the payload).
func RenderWebhookTemplate(tmpl string, root map[string]any) string {
	tmpl = strings.TrimSpace(tmpl)
	if tmpl == "" {
		return ""
	}
	return webhookTemplateToken.ReplaceAllStringFunc(tmpl, func(match string) string {
		inner := webhookTemplateToken.FindStringSubmatch(match)
		if len(inner) < 2 {
			return ""
		}
		path := strings.TrimSpace(inner[1])
		path = strings.TrimPrefix(path, ".")
		path = strings.TrimPrefix(path, "payload.")
		path = strings.TrimPrefix(path, "Payload.")
		v, ok := lookupDotPath(root, path)
		if !ok || v == nil {
			return ""
		}
		switch t := v.(type) {
		case string:
			return t
		case float64:
			if t == float64(int64(t)) {
				return strconv.FormatInt(int64(t), 10)
			}
			return strconv.FormatFloat(t, 'f', -1, 64)
		case bool:
			return strconv.FormatBool(t)
		case json.Number:
			return t.String()
		default:
			b, err := json.Marshal(t)
			if err != nil {
				return fmt.Sprint(t)
			}
			return string(b)
		}
	})
}

func lookupDotPath(root map[string]any, path string) (any, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, false
	}
	parts := strings.Split(path, ".")
	var cur any = root
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			return nil, false
		}
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		next, ok := m[p]
		if !ok {
			return nil, false
		}
		cur = next
	}
	return cur, true
}

// MapWebhookTitleDescription builds title/description from templates + fallbacks.
func MapWebhookTitleDescription(channel ProjectWebhookChannel, payload map[string]any, receivedAt time.Time) (title, description string) {
	if payload == nil {
		payload = map[string]any{}
	}
	title = strings.TrimSpace(RenderWebhookTemplate(channel.TitleTemplate, payload))
	if title == "" {
		title = firstStringField(payload, "title", "summary", "subject", "name")
	}
	if title == "" {
		name := strings.TrimSpace(channel.Name)
		if name == "" {
			name = "webhook"
		}
		title = fmt.Sprintf("[%s] inbound %s", name, receivedAt.UTC().Format(time.RFC3339))
	}
	if utf8.RuneCountInString(title) > 200 {
		title = truncateRunes(title, 200)
	}

	description = strings.TrimSpace(RenderWebhookTemplate(channel.DescriptionTemplate, payload))
	if description == "" {
		description = firstStringField(payload, "description", "body", "content", "message", "text")
	}
	if description == "" {
		description = summarizeWebhookPayload(payload)
	}
	if utf8.RuneCountInString(description) > webhookDescMaxRunes {
		description = truncateRunes(description, webhookDescMaxRunes) + "\n\n…(truncated)"
	}
	return title, description
}

func firstStringField(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok {
				if t := strings.TrimSpace(s); t != "" {
					return t
				}
			}
		}
	}
	return ""
}

func summarizeWebhookPayload(payload map[string]any) string {
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "(payload unavailable)"
	}
	s := string(b)
	const max = 4000
	if len(s) > max {
		s = s[:max] + "\n…(truncated)"
	}
	return "### Webhook payload (summarized)\n\n```json\n" + s + "\n```"
}

func truncateRunes(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max])
}

// FilterWebhookHeaders keeps non-sensitive request headers for SOP input.
func FilterWebhookHeaders(h map[string][]string) map[string]string {
	out := map[string]string{}
	for k, vals := range h {
		lk := strings.ToLower(k)
		switch {
		case strings.Contains(lk, "authorization"),
			strings.Contains(lk, "secret"),
			strings.Contains(lk, "signature"),
			strings.Contains(lk, "token"),
			lk == "cookie",
			lk == "set-cookie",
			lk == "x-loopforge-signature",
			lk == "x-loopforge-token":
			continue
		}
		if len(vals) == 0 {
			continue
		}
		out[k] = vals[0]
	}
	return out
}
