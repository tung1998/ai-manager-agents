package channels

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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
}

// Adapter connects one bot.
type Adapter interface {
	// Run delivers messages addressed to the bot until ctx ends (onReady gets
	// the bot's name once connected).
	Run(ctx context.Context, onReady func(bot string), onMessage func(Incoming)) error
	Send(ctx context.Context, chatID, text string) error
	Typing(ctx context.Context, chatID string)
}

// Telegram is a bot over the Bot API with long polling (no public URL).
type Telegram struct {
	Token   string
	BaseURL string // "" = https://api.telegram.org
	client  http.Client
	bot     string
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
		return err
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
		From struct {
			Username string `json:"username"`
		} `json:"from"`
	} `json:"reply_to_message"`
}

func (t *Telegram) Run(ctx context.Context, onReady func(string), onMessage func(Incoming)) error {
	var me struct {
		Username string `json:"username"`
	}
	if err := t.call(ctx, "getMe", map[string]any{}, &me); err != nil {
		return err
	}
	t.bot = me.Username
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
	if in.Private {
		in.Text = text
		return in, true
	}
	tag := "@" + t.bot
	switch {
	case t.bot != "" && strings.Contains(strings.ToLower(text), strings.ToLower(tag)):
		i := strings.Index(strings.ToLower(text), strings.ToLower(tag))
		in.Text = strings.TrimSpace(text[:i] + text[i+len(tag):])
	case m.ReplyTo != nil && strings.EqualFold(m.ReplyTo.From.Username, t.bot):
		in.Text = text
	default:
		return in, false
	}
	return in, in.Text != ""
}

func (t *Telegram) Send(ctx context.Context, chatID, text string) error {
	id, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil {
		return errors.New("telegram: chat id không hợp lệ")
	}
	for _, part := range chunks(text, 4000) {
		if err := t.call(ctx, "sendMessage", map[string]any{"chat_id": id, "text": part}, nil); err != nil {
			return err
		}
	}
	return nil
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
