package channels

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDiscord(t *testing.T) {
	gw := wsServe(t, func(send func(string)) {
		send(`{"op":10,"d":{"heartbeat_interval":45000}}`)
	}, func(msg string, send func(string)) {
		var p struct {
			Op int `json:"op"`
			D  struct {
				Token   string `json:"token"`
				Intents int    `json:"intents"`
			} `json:"d"`
		}
		json.Unmarshal([]byte(msg), &p)
		if p.Op != 2 || p.D.Token != "TOK" || p.D.Intents&(1<<15) == 0 {
			return
		}
		send(`{"op":0,"s":1,"t":"READY","d":{"user":{"id":"99","username":"shopbot"}}}`)
		send(`{"op":0,"s":2,"t":"MESSAGE_CREATE","d":{"channel_id":"c1","content":"xin chào","author":{"id":"7","username":"an"},"mentions":[]}}`)
		send(`{"op":0,"s":3,"t":"MESSAGE_CREATE","d":{"channel_id":"c2","guild_id":"g","content":"chuyện riêng","author":{"id":"8"},"mentions":[]}}`)
		send(`{"op":0,"s":4,"t":"MESSAGE_CREATE","d":{"channel_id":"c2","guild_id":"g","content":"<@99> đơn 123","author":{"id":"8","username":"binh"},"mentions":[{"id":"99"}]}}`)
		send(`{"op":0,"s":5,"t":"MESSAGE_CREATE","d":{"channel_id":"c2","guild_id":"g","content":"<@99> bot","author":{"id":"5","bot":true},"mentions":[{"id":"99"}]}}`)
		send(`{"op":0,"s":6,"t":"MESSAGE_CREATE","d":{"channel_id":"c2","guild_id":"g","content":"còn đơn 456?","author":{"id":"8","username":"binh"},"mentions":[],"referenced_message":{"author":{"id":"99"}}}}`)
	})
	defer gw.Close()
	var mu sync.Mutex
	var posted []string
	rest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bot TOK" {
			w.WriteHeader(401)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/messages") {
			var b map[string]string
			json.NewDecoder(r.Body).Decode(&b)
			mu.Lock()
			posted = append(posted, r.URL.Path+"|"+b["content"])
			mu.Unlock()
		}
		w.Write([]byte(`{}`))
	}))
	defer rest.Close()
	d := &Discord{Token: "TOK", GatewayURL: "ws" + strings.TrimPrefix(gw.URL, "http"), APIBase: rest.URL}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	got := make(chan Incoming, 10)
	bot := make(chan string, 1)
	go d.Run(ctx, func(name string) { bot <- name }, func(m Incoming) { got <- m })
	var msgs []Incoming
	for len(msgs) < 4 {
		select {
		case m := <-got:
			msgs = append(msgs, m)
		case <-ctx.Done():
			t.Fatalf("got %+v", msgs)
		}
	}
	// every message comes up; Addressed says whether it is for the bot (a
	// kept conversation listens to the rest of its chat, ADR-049)
	if <-bot != "shopbot" || !msgs[0].Private || !msgs[0].Addressed || msgs[0].ChatID != "c1" {
		t.Fatalf("dm = %+v", msgs[0])
	}
	if msgs[1].Text != "chuyện riêng" || msgs[1].Addressed {
		t.Fatalf("untagged = %+v", msgs[1])
	}
	if msgs[2].Text != "đơn 123" || !msgs[2].Addressed || msgs[2].UserName != "binh" {
		t.Fatalf("tagged = %+v", msgs[2])
	}
	if msgs[3].Text != "còn đơn 456?" || !msgs[3].Addressed {
		t.Fatalf("a reply to the bot = %+v", msgs[3])
	}
	select {
	case m := <-got:
		t.Fatalf("a bot's own message came up: %+v", m)
	case <-time.After(200 * time.Millisecond):
	}
	if _, err := d.Send(ctx, "c2", strings.Repeat("b", 2500)); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(posted) != 2 || !strings.HasPrefix(posted[0], "/channels/c2/messages|") {
		t.Fatalf("posted = %d %v", len(posted), posted)
	}
}

// A gateway close that retrying cannot heal (bad token, intents not enabled)
// stops the bot with a reason instead of reconnecting forever.
func TestDiscordStopsOnFatalClose(t *testing.T) {
	for code, want := range map[string]string{"4004": "token", "4014": "Message Content"} {
		gw := wsServe(t, func(send func(string)) {
			send(`{"op":10,"d":{"heartbeat_interval":45000}}`)
		}, func(msg string, send func(string)) { send("CLOSE:" + code) })
		d := &Discord{Token: "TOK", GatewayURL: "ws" + strings.TrimPrefix(gw.URL, "http")}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err := d.Run(ctx, func(string) {}, func(Incoming) {})
		if ctx.Err() != nil || err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("close %s: err = %v (timed out: %v)", code, err, ctx.Err() != nil)
		}
		cancel()
		gw.Close()
	}
}

// Slash commands (ADR-049): registered on connect, they come down the
// gateway (no public URL); the answer edits the "thinking…" reply.
func TestDiscordSlashCommands(t *testing.T) {
	gw := wsServe(t, func(send func(string)) {
		send(`{"op":10,"d":{"heartbeat_interval":45000}}`)
	}, func(msg string, send func(string)) {
		if !strings.Contains(msg, `"op":2`) {
			return
		}
		send(`{"op":0,"s":1,"t":"READY","d":{"user":{"id":"99","username":"shopbot"},"application":{"id":"app1"}}}`)
		send(`{"op":0,"s":2,"t":"INTERACTION_CREATE","d":{"id":"int1","token":"itok","type":2,"channel_id":"c2","guild_id":"g","data":{"name":"create-conversation"},"member":{"user":{"id":"8","username":"binh"}}}}`)
		send(`{"op":0,"s":3,"t":"MESSAGE_CREATE","d":{"channel_id":"c2","guild_id":"g","content":"/close-conversation","author":{"id":"8","username":"binh"},"mentions":[]}}`)
		send(`{"op":0,"s":4,"t":"INTERACTION_CREATE","d":{"id":"int2","token":"itok2","type":2,"channel_id":"c2","guild_id":"g","data":{"name":"job","options":[{"name":"viec","type":3,"value":"sửa lỗi thanh toán"}]},"member":{"user":{"id":"8","username":"binh"}}}}`)
	})
	defer gw.Close()
	var mu sync.Mutex
	var calls []string
	rest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		calls = append(calls, r.Method+" "+r.URL.Path+" "+string(raw))
		mu.Unlock()
		w.Write([]byte(`{}`))
	}))
	defer rest.Close()
	d := &Discord{Token: "TOK", GatewayURL: "ws" + strings.TrimPrefix(gw.URL, "http"), APIBase: rest.URL}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	got := make(chan Incoming, 10)
	go d.Run(ctx, func(string) {}, func(m Incoming) { got <- m })
	var msgs []Incoming
	for len(msgs) < 3 {
		select {
		case m := <-got:
			msgs = append(msgs, m)
		case <-ctx.Done():
			t.Fatalf("got %+v", msgs)
		}
	}
	if msgs[2].Text != "/job sửa lỗi thanh toán" {
		t.Fatalf("job command = %+v", msgs[2])
	}
	if msgs[0].Text != "/create-conversation" || msgs[0].ChatID != "c2" || msgs[0].UserID != "8" || msgs[0].Respond == nil || !msgs[0].Addressed {
		t.Fatalf("command = %+v", msgs[0])
	}
	if msgs[1].Text != "/close-conversation" { // typed without tagging the bot
		t.Fatalf("typed command = %+v", msgs[1])
	}
	if err := msgs[0].Respond(ctx, "Đã bắt đầu"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	all := strings.Join(calls, "\n")
	for _, want := range []string{"PUT /applications/app1/commands", "create-conversation", `"name":"job"`, `"required":true`, "POST /interactions/int1/itok/callback", `"type":5`,
		"PATCH /webhooks/app1/itok/messages/@original", "Đã bắt đầu"} {
		if !strings.Contains(all, want) {
			t.Errorf("no %q in:\n%s", want, all)
		}
	}
}
