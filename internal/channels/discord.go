package channels

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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
	appID      string // the application: its commands, its interactions' replies
	menu       menu
}

// intents: guilds (threads made), guild messages, direct messages, message content
const discordIntents = 1<<0 | 1<<9 | 1<<12 | 1<<15

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
		ready, err := d.session(ctx, onReady, onMessage)
		if ctx.Err() != nil {
			return nil
		}
		var ce *CloseError
		if errors.As(err, &ce) {
			if why, fatal := discordFatal[ce.Code]; fatal {
				return errors.New("discord: " + why) // retrying cannot heal these
			}
		}
		if ready {
			backoff = time.Second // a session that worked: a quick reconnect
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

// discordFatal are the gateway close codes a reconnect does not fix.
var discordFatal = map[int]string{
	4004: "token không hợp lệ",
	4010: "shard không hợp lệ",
	4011: "bot cần sharding",
	4012: "phiên bản gateway không hợp lệ",
	4013: "intents không hợp lệ",
	4014: "bot chưa được bật Message Content Intent (Developer Portal → Bot → Privileged Gateway Intents)",
}

func (d *Discord) session(ctx context.Context, onReady func(string), onMessage func(Incoming)) (ready bool, err error) {
	url := d.GatewayURL
	if url == "" {
		url = "wss://gateway.discord.gg/?v=10&encoding=json"
	}
	dctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	c, err := dialWS(dctx, url)
	cancel()
	if err != nil {
		return false, err
	}
	sctx, end := context.WithCancel(ctx)
	defer end()
	go func() { <-sctx.Done(); c.Close() }() // ctx ending unblocks Read; the session ending frees this
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
		return false, err
	}
	if hello.Op != 10 {
		return false, fmt.Errorf("discord: expected hello, got op %d", hello.Op)
	}
	var h struct {
		Interval int `json:"heartbeat_interval"`
	}
	_ = json.Unmarshal(hello.D, &h)
	if h.Interval <= 0 {
		h.Interval = 41250
	}
	hbCtx, stop := context.WithCancel(sctx)
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
		return false, err
	}
	for {
		p, err := next()
		if err != nil {
			return ready, err
		}
		switch p.Op {
		case 7, 9: // reconnect, invalid session
			return ready, errors.New("discord: gateway asked to reconnect")
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
					Application struct {
						ID string `json:"id"`
					} `json:"application"`
				}
				_ = json.Unmarshal(p.D, &r)
				d.botID, d.appID = r.User.ID, r.Application.ID
				ready = true
				onReady(r.User.Username)
			case "INTERACTION_CREATE":
				if m, ok := d.interaction(ctx, p.D); ok {
					onMessage(m)
				}
			case "MESSAGE_CREATE":
				if m, ok := d.addressed(p.D); ok {
					onMessage(m)
				}
			case "THREAD_CREATE":
				if m, ok := d.threadMade(ctx, p.D); ok {
					onMessage(m)
				}
			}
		}
	}
}

// addressed: a direct message, or one that tags the bot; never a bot's own.
func (d *Discord) addressed(raw json.RawMessage) (Incoming, bool) {
	var m struct {
		ID        string `json:"id"`
		Type      int    `json:"type"`
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
		Replied *struct {
			ID     string `json:"id"`
			Author struct {
				ID string `json:"id"`
			} `json:"author"`
		} `json:"referenced_message"`
	}
	if json.Unmarshal(raw, &m) != nil || m.Author.Bot || m.Author.ID == d.botID {
		return Incoming{}, false
	}
	if m.Type != 0 && m.Type != 19 { // a person's message or reply; not "X started a thread", a pin, a join…
		return Incoming{}, false
	}
	in := Incoming{ChatID: m.ChannelID, UserID: m.Author.ID, UserName: m.Author.Username, Private: m.GuildID == "", MessageID: m.ID, GuildID: m.GuildID}
	tagged := false
	for _, x := range m.Mentions {
		if x.ID == d.botID && d.botID != "" {
			tagged = true
		}
	}
	replied := m.Replied != nil && d.botID != "" && m.Replied.Author.ID == d.botID
	if replied {
		in.ReplyTo = m.Replied.ID
	}
	// every message comes up: Addressed = for the bot (a DM, a tag, a reply to
	// it, a command typed without a tag); a kept conversation hears the rest
	in.Addressed = in.Private || tagged || replied || d.menu.has(m.Content)
	text := m.Content
	if d.botID != "" {
		text = strings.NewReplacer("<@"+d.botID+">", "", "<@!"+d.botID+">", "").Replace(text)
	}
	in.Text = strings.TrimSpace(text)
	return in, in.Text != ""
}

// threadMade: a new thread. The bot joins it (to hear what is said there);
// made from a message, its id is that message's.
func (d *Discord) threadMade(ctx context.Context, raw json.RawMessage) (Incoming, bool) {
	var th struct {
		ID           string `json:"id"`
		GuildID      string `json:"guild_id"`
		ParentID     string `json:"parent_id"`
		NewlyCreated bool   `json:"newly_created"`
	}
	if json.Unmarshal(raw, &th) != nil || th.ID == "" || !th.NewlyCreated {
		return Incoming{}, false
	}
	go func() {
		if err := d.do(context.WithoutCancel(ctx), "PUT", "/channels/"+th.ID+"/thread-members/@me", nil); err != nil {
			slog.Warn("discord: not in the thread", "thread", th.ID, "err", err)
		}
	}()
	return Incoming{ChatID: th.ID, GuildID: th.GuildID, ThreadOf: th.ID}, true
}

// MakeThread opens a public thread: from a message (the thread takes its id),
// or on its own in chatID; a day without messages archives it.
func (d *Discord) MakeThread(ctx context.Context, chatID, fromMsg, name string) (string, error) {
	var out struct {
		ID string `json:"id"`
	}
	body := map[string]any{"name": name, "auto_archive_duration": 1440}
	path := "/channels/" + chatID + "/threads"
	if fromMsg != "" {
		path = "/channels/" + chatID + "/messages/" + fromMsg + "/threads"
	} else {
		body["type"] = 11 // public thread
	}
	if err := d.do(ctx, "POST", path, body, &out); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", errors.New("discord: no thread id")
	}
	return out.ID, nil
}

// threadMenu is the entry of a message's "Apps" menu that makes a thread from it.
const threadMenu = "Create thread"

// SetCommands puts the bot's slash commands in Discord's "/" menu (after
// READY: it needs the application's id).
func (d *Discord) SetCommands(ctx context.Context, cmds []Command) {
	cmds = capped(cmds)
	d.menu.set(cmds)
	if d.appID == "" {
		return
	}
	var list []map[string]any
	for _, c := range cmds {
		cmd := map[string]any{"name": c.Name, "description": clip(firstNonEmpty(c.Description, c.Name), 100), "type": 1, "contexts": []int{0, 1, 2}}
		if c.Arg != "" { // the text after the command (a string, required)
			cmd["options"] = []map[string]any{{"type": 3, "name": argName(c.Arg), "description": clip(c.Arg, 100), "required": true}}
		}
		list = append(list, cmd)
	}
	for _, c := range cmds {
		if c.Name == "create-thread" { // and in a message's Apps menu: the thread grows from that message
			list = append(list, map[string]any{"name": threadMenu, "type": 3, "contexts": []int{0}})
		}
	}
	if err := d.do(ctx, "PUT", "/applications/"+d.appID+"/commands", list); err != nil {
		slog.Warn("discord: slash commands not registered", "err", err)
	}
}

// interaction is a slash command: acknowledged at once ("thinking…"), its
// answer edits that reply.
func (d *Discord) interaction(ctx context.Context, raw json.RawMessage) (Incoming, bool) {
	type user struct {
		ID       string `json:"id"`
		Username string `json:"username"`
	}
	var x struct {
		ID        string `json:"id"`
		Token     string `json:"token"`
		Type      int    `json:"type"`
		ChannelID string `json:"channel_id"`
		GuildID   string `json:"guild_id"`
		Data      struct {
			Name     string `json:"name"`
			Type     int    `json:"type"`      // 3: a message's "Apps" menu
			TargetID string `json:"target_id"` // …on that message
			Options  []struct {
				Value any `json:"value"`
			} `json:"options"`
		} `json:"data"`
		Member *struct {
			User user `json:"user"`
		} `json:"member"`
		User *user `json:"user"`
	}
	if json.Unmarshal(raw, &x) != nil || x.Type != 2 || x.Token == "" {
		return Incoming{}, false
	}
	if err := d.do(ctx, "POST", "/interactions/"+x.ID+"/"+x.Token+"/callback", map[string]any{"type": 5}); err != nil {
		return Incoming{}, false
	}
	in := Incoming{ChatID: x.ChannelID, Text: "/" + x.Data.Name, Private: x.GuildID == "", Addressed: true, GuildID: x.GuildID}
	if x.Data.Type == 3 && x.Data.Name == threadMenu { // Apps → Create thread, on one message
		in.Text, in.ReplyTo = "/create-thread", x.Data.TargetID
	}
	for _, o := range x.Data.Options {
		if s, ok := o.Value.(string); ok {
			in.Text += " " + s
		}
	}
	if x.Member != nil {
		in.UserID, in.UserName = x.Member.User.ID, x.Member.User.Username
	} else if x.User != nil {
		in.UserID, in.UserName = x.User.ID, x.User.Username
	}
	app, token := d.appID, x.Token
	in.Respond = func(ctx context.Context, text string) (string, error) {
		var sent struct {
			ID string `json:"id"`
		}
		err := d.do(ctx, "PATCH", "/webhooks/"+app+"/"+token+"/messages/@original", map[string]string{"content": text}, &sent)
		return sent.ID, err
	}
	return in, true
}

func (d *Discord) rest(ctx context.Context, path string, body any) error {
	return d.do(ctx, "POST", path, body, nil)
}

func (d *Discord) do(ctx context.Context, method, path string, body any, out ...any) error {
	base := d.APIBase
	if base == "" {
		base = "https://discord.com/api/v10"
	}
	var raw []byte
	if body != nil { // none: no "null" (joining a thread takes no body)
		raw, _ = json.Marshal(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bot "+d.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("discord %s: %s", path, resp.Status)
	}
	if len(out) > 0 && out[0] != nil {
		_ = json.NewDecoder(resp.Body).Decode(out[0])
	}
	return nil
}

func (d *Discord) Send(ctx context.Context, chatID, text string) ([]string, error) {
	var ids []string
	for _, part := range chunks(text, 1900) {
		var sent struct {
			ID string `json:"id"`
		}
		if err := d.do(ctx, "POST", "/channels/"+chatID+"/messages", map[string]string{"content": part}, &sent); err != nil {
			return ids, err
		}
		ids = append(ids, sent.ID)
	}
	return ids, nil
}

func (d *Discord) Typing(ctx context.Context, chatID string) {
	_ = d.rest(ctx, "/channels/"+chatID+"/typing", map[string]any{})
}
