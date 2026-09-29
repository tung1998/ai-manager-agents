package channels

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Incoming is a message addressed to the bot.
type Incoming struct {
	ChatID   string // where to answer (a private chat, a group, a Discord channel)
	UserID   string
	UserName string
	Text     string // without the bot's @mention
	Private  bool
	// ReplyTo: the bot's message this one replies to ("" = none): its
	// conversation goes on.
	ReplyTo string
	// Addressed: for the bot (private, tagged, a reply to it, a command).
	// Others come up too: a kept conversation hears its whole chat.
	Addressed bool
	// Respond answers a slash command (Discord shows "thinking…" until it
	// does); nil for a message, which is answered with Send.
	Respond func(ctx context.Context, text string) error
}

// Commands are the bot's own: every channel offers them in its menu.
var Commands = []struct{ Name, Description, Option string }{
	{"job", "Giao việc cho đội: /job <việc cần làm>", "viec"},
	{"create-conversation", "Bắt đầu hội thoại: bot nhớ những gì bạn nói ở đây", ""},
	{"close-conversation", "Kết thúc hội thoại: mỗi tin được trả lời riêng", ""},
}

// isCommand: one of the bot's commands, however typed (see command in manager.go).
func isCommand(text string) bool {
	_, _, ok := command(text)
	return ok
}

// Adapter connects one bot.
type Adapter interface {
	// Run delivers messages addressed to the bot until ctx ends (onReady gets
	// the bot's name once connected).
	Run(ctx context.Context, onReady func(bot string), onMessage func(Incoming)) error
	// Send posts text (in parts if long) and returns the ids of what it posted.
	Send(ctx context.Context, chatID, text string) ([]string, error)
	Typing(ctx context.Context, chatID string)
}

// Telegram is a bot over the Bot API with long polling (no public URL).
type Telegram struct {
	Token   string
	BaseURL string // "" = https://api.telegram.org
	client  http.Client
	bot     string
	retry   time.Duration // wait before retrying a failed call (0 = 5s)
}

func (t *Telegram) call(ctx context.Context, method string, body any, out any) error {
	base := t.BaseURL
	if base == "" {
		base = "https://api.telegram.org"
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", base+"/bot"+t.Token+"/"+method, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.client.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) { // its text has the URL, and the URL has the token
			return fmt.Errorf("telegram %s: %v", method, ue.Err)
		}
		return fmt.Errorf("telegram %s: lỗi mạng", method)
	}
	defer resp.Body.Close()
	var env struct {
		OK          bool            `json:"ok"`
		Description string          `json:"description"`
		Result      json.RawMessage `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return fmt.Errorf("telegram %s: %s", method, resp.Status)
	}
	if !env.OK {
		return fmt.Errorf("telegram %s: %s", method, env.Description)
	}
	if out != nil {
		return json.Unmarshal(env.Result, out)
	}
	return nil
}

type tgMessage struct {
	Text    string `json:"text"`
	Caption string `json:"caption"`
	Chat    struct {
		ID   int64  `json:"id"`
		Type string `json:"type"`
	} `json:"chat"`
	From struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
	} `json:"from"`
	ReplyTo *struct {
		MessageID int64 `json:"message_id"`
		From      struct {
			Username string `json:"username"`
		} `json:"from"`
	} `json:"reply_to_message"`
}

func (t *Telegram) Run(ctx context.Context, onReady func(string), onMessage func(Incoming)) error {
	var me struct {
		Username string `json:"username"`
	}
	wait := t.retry
	if wait == 0 {
		wait = 5 * time.Second
	}
	for backoff := wait; ; backoff = min(backoff*2, 12*wait) { // a network hiccup at start is retried
		err := t.call(ctx, "getMe", map[string]any{}, &me)
		if err == nil {
			break
		}
		if ctx.Err() != nil {
			return nil
		}
		if strings.Contains(err.Error(), "Unauthorized") || strings.Contains(err.Error(), "Not Found") {
			return err // a wrong token does not heal by retrying
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
	}
	t.bot = me.Username
	var menu []map[string]string // Telegram commands: a-z, 0-9 and _ only
	for _, c := range Commands {
		menu = append(menu, map[string]string{"command": strings.ReplaceAll(c.Name, "-", "_"), "description": c.Description})
	}
	_ = t.call(ctx, "setMyCommands", map[string]any{"commands": menu}, nil)
	onReady(me.Username)
	offset := int64(0)
	for ctx.Err() == nil {
		var updates []struct {
			UpdateID int64      `json:"update_id"`
			Message  *tgMessage `json:"message"`
		}
		pctx, cancel := context.WithTimeout(ctx, 40*time.Second)
		err := t.call(pctx, "getUpdates", map[string]any{"offset": offset, "timeout": 25, "allowed_updates": []string{"message"}}, &updates)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if strings.Contains(err.Error(), "Unauthorized") {
				return err // a wrong token does not heal by retrying
			}
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(5 * time.Second):
			}
			continue
		}
		for _, u := range updates {
			offset = u.UpdateID + 1
			if m, ok := t.addressed(u.Message); ok {
				onMessage(m)
			}
		}
	}
	return nil
}

// addressed: a private message, or in a group one that tags the bot or
// replies to it.
func (t *Telegram) addressed(m *tgMessage) (Incoming, bool) {
	if m == nil {
		return Incoming{}, false
	}
	text := strings.TrimSpace(m.Text + m.Caption)
	in := Incoming{ChatID: strconv.FormatInt(m.Chat.ID, 10), UserID: strconv.FormatInt(m.From.ID, 10), UserName: m.From.Username, Private: m.Chat.Type == "private"}
	if text == "" {
		return in, false
	}
	in.Text, in.Addressed = text, true
	if m.ReplyTo != nil && t.bot != "" && strings.EqualFold(m.ReplyTo.From.Username, t.bot) {
		in.ReplyTo = strconv.FormatInt(m.ReplyTo.MessageID, 10)
	}
	switch {
	case in.Private, isCommand(text): // "/create_conversation" in a group needs no tag
	case t.bot != "" && removeTag(&text, "@"+t.bot):
		in.Text = strings.TrimSpace(text)
	case m.ReplyTo != nil && strings.EqualFold(m.ReplyTo.From.Username, t.bot):
	default:
		in.Addressed = false
	}
	return in, in.Text != ""
}

func (t *Telegram) Send(ctx context.Context, chatID, text string) ([]string, error) {
	id, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil {
		return nil, errors.New("telegram: chat id không hợp lệ")
	}
	var ids []string
	for _, part := range chunks(text, 4000) {
		var sent struct {
			MessageID int64 `json:"message_id"`
		}
		if err := t.call(ctx, "sendMessage", map[string]any{"chat_id": id, "text": part}, &sent); err != nil {
			return ids, err
		}
		ids = append(ids, strconv.FormatInt(sent.MessageID, 10))
	}
	return ids, nil
}

func (t *Telegram) Typing(ctx context.Context, chatID string) {
	if id, err := strconv.ParseInt(chatID, 10, 64); err == nil {
		_ = t.call(ctx, "sendChatAction", map[string]any{"chat_id": id, "action": "typing"}, nil)
	}
}

// chunks splits text into pieces of at most n bytes, on line breaks when it can.
func chunks(text string, n int) []string {
	var out []string
	for len(text) > n {
		cut := strings.LastIndex(text[:n], "\n")
		if cut < n/2 {
			cut = n
			for cut > 0 && (text[cut]&0xC0) == 0x80 { // not inside a UTF-8 character
				cut--
			}
		}
		out = append(out, text[:cut])
		text = strings.TrimLeft(text[cut:], "\n")
	}
	if strings.TrimSpace(text) != "" || len(out) == 0 {
		out = append(out, text)
	}
	return out
}

// removeTag drops the first "@bot" (any case) from s, rune by rune so a
// character that changes length when lowercased never throws the index off.
func removeTag(s *string, tag string) bool {
	rs, tr := []rune(*s), []rune(tag)
	for i := 0; i+len(tr) <= len(rs); i++ {
		if strings.EqualFold(string(rs[i:i+len(tr)]), tag) {
			*s = string(rs[:i]) + string(rs[i+len(tr):])
			return true
		}
	}
	return false
}
