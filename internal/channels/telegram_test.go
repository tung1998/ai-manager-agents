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

func TestTelegram(t *testing.T) {
	var mu sync.Mutex
	var sent []string
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/botTOK/") {
			http.NotFound(w, r)
			return
		}
		switch strings.TrimPrefix(r.URL.Path, "/botTOK/") {
		case "getMe":
			w.Write([]byte(`{"ok":true,"result":{"username":"shop_bot"}}`))
		case "getUpdates":
			mu.Lock()
			calls++
			n := calls
			mu.Unlock()
			if n > 1 {
				time.Sleep(50 * time.Millisecond)
				w.Write([]byte(`{"ok":true,"result":[]}`))
				return
			}
			w.Write([]byte(`{"ok":true,"result":[
				{"update_id":1,"message":{"text":"xin chào","chat":{"id":42,"type":"private"},"from":{"id":7,"username":"an"}}},
				{"update_id":2,"message":{"text":"nói chuyện riêng","chat":{"id":-5,"type":"group"},"from":{"id":8}}},
				{"update_id":3,"message":{"text":"@shop_bot đơn 123 đâu","chat":{"id":-5,"type":"group"},"from":{"id":8,"username":"binh"}}},
				{"update_id":4,"message":{"text":"còn đơn 456?","chat":{"id":-5,"type":"supergroup"},"from":{"id":9},"reply_to_message":{"from":{"username":"shop_bot"}}}}
			]}`))
		case "sendMessage", "sendChatAction":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if r.URL.Path == "/botTOK/sendMessage" {
				mu.Lock()
				sent = append(sent, body["text"].(string))
				mu.Unlock()
			}
			w.Write([]byte(`{"ok":true,"result":{}}`))
		}
	}))
	defer srv.Close()
	tg := &Telegram{Token: "TOK", BaseURL: srv.URL}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	got := make(chan Incoming, 10)
	bot := ""
	go tg.Run(ctx, func(name string) { bot = name }, func(m Incoming) { got <- m })
	var msgs []Incoming
	for len(msgs) < 3 {
		select {
		case m := <-got:
			msgs = append(msgs, m)
		case <-ctx.Done():
			t.Fatalf("got %+v", msgs)
		}
	}
	if bot != "shop_bot" || msgs[0].ChatID != "42" || !msgs[0].Private || msgs[1].Text != "đơn 123 đâu" || msgs[2].Text != "còn đơn 456?" || msgs[1].UserName != "binh" {
		t.Fatalf("bot %q msgs %+v", bot, msgs)
	}
	if err := tg.Send(ctx, "42", strings.Repeat("a", 5000)); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(sent) != 2 || len(sent[0]) > 4096 {
		t.Fatalf("sent %d parts", len(sent))
	}
}

// Review C1: a tag after characters that grow when lowercased never panics.
func TestTelegramMentionUnicode(t *testing.T) {
	tg := &Telegram{bot: "shop_bot"}
	for _, text := range []string{"ȺȺȺ @shop_bot đơn 1", "@SHOP_BOT ẞ hỏi", "İİİİ @Shop_Bot"} {
		m := &tgMessage{Text: text}
		m.Chat.Type = "group"
		in, ok := tg.addressed(m)
		if !ok || strings.Contains(strings.ToLower(in.Text), "@shop_bot") {
			t.Errorf("%q → %q %v", text, in.Text, ok)
		}
	}
}

// Review I4: an error never carries the bot token (it is in the URL).
func TestTelegramErrorHidesToken(t *testing.T) {
	tg := &Telegram{Token: "123:SECRET", BaseURL: "http://127.0.0.1:1"}
	err := tg.call(context.Background(), "getMe", map[string]any{}, nil)
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("err = %v", err)
	}
}
