package channels

import (
	"context"
	"encoding/json"
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
	for len(msgs) < 2 {
		select {
		case m := <-got:
			msgs = append(msgs, m)
		case <-ctx.Done():
			t.Fatalf("got %+v", msgs)
		}
	}
	if <-bot != "shopbot" || !msgs[0].Private || msgs[0].ChatID != "c1" || msgs[1].Text != "đơn 123" || msgs[1].UserName != "binh" {
		t.Fatalf("msgs = %+v", msgs)
	}
	select {
	case m := <-got:
		t.Fatalf("a message not for the bot: %+v", m)
	case <-time.After(200 * time.Millisecond):
	}
	if err := d.Send(ctx, "c2", strings.Repeat("b", 2500)); err != nil {
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
