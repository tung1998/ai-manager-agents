package channels

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

// Discord is a bot on the Discord Gateway: it answers direct messages and
// messages that tag it.
type Discord struct {
	Token      string
	GatewayURL string // "" = wss://gateway.discord.gg/?v=10&encoding=json
	APIBase    string // "" = https://discord.com/api/v10
	client     http.Client
	botID      string
}

// intents: guild messages, direct messages, message content
const discordIntents = 1<<9 | 1<<12 | 1<<15

type gwPayload struct {
	Op int             `json:"op"`
	D  json.RawMessage `json:"d"`
	S  *int64          `json:"s"`
	T  string          `json:"t"`
}

// Run keeps a gateway session up (reconnecting) until ctx ends.
func (d *Discord) Run(ctx context.Context, onReady func(string), onMessage func(Incoming)) error {
	backoff := time.Second
	for ctx.Err() == nil {
		err := d.session(ctx, onReady, onMessage)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil && strings.Contains(err.Error(), "4004") {
			return errors.New("discord: token không hợp lệ") // authentication failed: no retry
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, time.Minute)
	}
	return nil
}

func (d *Discord) session(ctx context.Context, onReady func(string), onMessage func(Incoming)) error {
	url := d.GatewayURL
	if url == "" {
		url = "wss://gateway.discord.gg/?v=10&encoding=json"
	}
	dctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	c, err := dialWS(dctx, url)
	cancel()
	if err != nil {
		return err
	}
	defer c.Close()
	go func() { <-ctx.Done(); c.Close() }()
	var seq atomic.Int64
	seq.Store(-1)
	next := func() (gwPayload, error) {
		raw, err := c.Read()
		if err != nil {
			return gwPayload{}, err
		}
		var p gwPayload
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			return gwPayload{}, err
		}
		if p.S != nil {
			seq.Store(*p.S)
		}
		return p, nil
	}
	hello, err := next()
	if err != nil {
		return err
	}
	if hello.Op != 10 {
		return fmt.Errorf("discord: expected hello, got op %d", hello.Op)
	}
	var h struct {
		Interval int `json:"heartbeat_interval"`
	}
	_ = json.Unmarshal(hello.D, &h)
	if h.Interval <= 0 {
		h.Interval = 41250
	}
	hbCtx, stop := context.WithCancel(ctx)
	defer stop()
	go func() {
		tick := time.NewTicker(time.Duration(h.Interval) * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-hbCtx.Done():
				return
			case <-tick.C:
				s := seq.Load()
				beat := `{"op":1,"d":null}`
				if s >= 0 {
					beat = fmt.Sprintf(`{"op":1,"d":%d}`, s)
				}
				if c.WriteText(beat) != nil {
					return
				}
			}
		}
	}()
	identify, _ := json.Marshal(map[string]any{"op": 2, "d": map[string]any{"token": d.Token, "intents": discordIntents,
		"properties": map[string]string{"os": "linux", "browser": "agent-office", "device": "agent-office"}}})
	if err := c.WriteText(string(identify)); err != nil {
		return err
	}
	for {
		p, err := next()
		if err != nil {
			return err
		}
		switch p.Op {
		case 7, 9: // reconnect, invalid session
			return errors.New("discord: gateway asked to reconnect")
		case 1:
			_ = c.WriteText(fmt.Sprintf(`{"op":1,"d":%d}`, seq.Load()))
		case 0:
			switch p.T {
			case "READY":
				var r struct {
					User struct {
						ID       string `json:"id"`
						Username string `json:"username"`
					} `json:"user"`
				}
				_ = json.Unmarshal(p.D, &r)
				d.botID = r.User.ID
				onReady(r.User.Username)
			case "MESSAGE_CREATE":
				if m, ok := d.addressed(p.D); ok {
					onMessage(m)
				}
			}
		}
	}
}

// addressed: a direct message, or one that tags the bot; never a bot's own.
func (d *Discord) addressed(raw json.RawMessage) (Incoming, bool) {
	var m struct {
		ChannelID string `json:"channel_id"`
		GuildID   string `json:"guild_id"`
		Content   string `json:"content"`
		Author    struct {
			ID       string `json:"id"`
			Username string `json:"username"`
			Bot      bool   `json:"bot"`
		} `json:"author"`
		Mentions []struct {
			ID string `json:"id"`
		} `json:"mentions"`
	}
	if json.Unmarshal(raw, &m) != nil || m.Author.Bot || m.Author.ID == d.botID {
		return Incoming{}, false
	}
	in := Incoming{ChatID: m.ChannelID, UserID: m.Author.ID, UserName: m.Author.Username, Private: m.GuildID == ""}
	tagged := false
	for _, x := range m.Mentions {
		if x.ID == d.botID && d.botID != "" {
			tagged = true
		}
	}
	if !in.Private && !tagged {
		return in, false
	}
	text := m.Content
	if d.botID != "" {
		text = strings.NewReplacer("<@"+d.botID+">", "", "<@!"+d.botID+">", "").Replace(text)
	}
	in.Text = strings.TrimSpace(text)
	return in, in.Text != ""
}

func (d *Discord) rest(ctx context.Context, path string, body any) error {
	base := d.APIBase
	if base == "" {
		base = "https://discord.com/api/v10"
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", base+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bot "+d.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("discord %s: %s", path, resp.Status)
	}
	return nil
}

func (d *Discord) Send(ctx context.Context, chatID, text string) error {
	for _, part := range chunks(text, 1900) {
		if err := d.rest(ctx, "/channels/"+chatID+"/messages", map[string]string{"content": part}); err != nil {
			return err
		}
	}
	return nil
}

func (d *Discord) Typing(ctx context.Context, chatID string) {
	_ = d.rest(ctx, "/channels/"+chatID+"/typing", map[string]any{})
}
