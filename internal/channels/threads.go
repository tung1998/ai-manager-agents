package channels

import (
	"context"
	"strconv"
	"strings"

	"bitbucket.org/senprints/agent-office/internal/attach"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

// Discord threads: a thread made from a message goes on with that message's
// conversation (the thread's id is the message's). As anywhere, the bot
// answers a tag; /create-conversation in the thread makes it hear the rest. The conversation links to where it
// is on Discord: its first message, then its thread.

func msgKey(messageID string) string { return "thread:" + messageID } // a message → its conversation
func inKey(chatID string) string     { return "in:" + chatID }        // a thread → its conversation

const linkKey = "conv_link/"

// lastKey: the bot's latest answer in a chat (/create-thread from the "/" menu grows from it).
func lastKey(channelID, chatID string) string { return "channel_last/" + channelID + "/" + chatID }

// ConversationLink is where a conversation is on Discord or Telegram ("" = nowhere).
func ConversationLink(ctx context.Context, st storage.Store, convID string) string {
	var v string
	_, _ = st.Settings().Get(ctx, linkKey+convID, &v)
	return v
}

func discordURL(guild, chat, msg string) string {
	if guild == "" {
		guild = "@me"
	}
	u := "https://discord.com/channels/" + guild + "/" + chat
	if msg != "" {
		u += "/" + msg
	}
	return u
}

// threadOf is the conversation a thread chat goes on with ("" = none).
func (m *Manager) threadOf(ctx context.Context, ch storage.Channel, chatID string) string {
	id, err := m.store.Channels().Thread(ctx, ch.ID, inKey(chatID))
	if err != nil {
		return ""
	}
	return id
}

// placed: a message of a conversation (the person's or the bot's) is known by
// its id, so a thread made from it finds the conversation; the conversation
// links to its first message, until it has a thread.
func (m *Manager) placed(ctx context.Context, ch storage.Channel, convID, guild, chat, msgID string) {
	if convID == "" || msgID == "" {
		return
	}
	url := ""
	switch ch.Kind {
	case "discord":
		_ = m.store.Channels().SetThread(ctx, ch.ID, msgKey(msgID), convID)
		url = discordURL(guild, chat, msgID)
	case "telegram":
		url = telegramURL(ch.BotName, chat, msgID)
	}
	if url != "" && ConversationLink(ctx, m.store, convID) == "" {
		_ = m.store.Settings().Set(ctx, linkKey+convID, url)
	}
}

// telegramURL: a supergroup's message (t.me/c/…), or the private chat with
// the bot; a basic group has no link.
func telegramURL(bot, chat, msg string) string {
	switch {
	case strings.HasPrefix(chat, "-100"):
		return "https://t.me/c/" + strings.TrimPrefix(chat, "-100") + "/" + msg
	case !strings.HasPrefix(chat, "-") && bot != "":
		return "https://t.me/" + bot
	}
	return ""
}

// threadMade binds a new thread to the conversation of the message it grew from.
func (m *Manager) threadMade(ctx context.Context, ch storage.Channel, in Incoming) {
	conv, err := m.store.Channels().Thread(ctx, ch.ID, msgKey(in.ThreadOf))
	if (err != nil || conv == "") && in.ParentID != "" { // a bot answer remembered before threads were (its reply key)
		conv, err = m.store.Channels().Thread(ctx, ch.ID, "msg:"+in.ParentID+":"+in.ThreadOf)
	}
	if err != nil || conv == "" {
		return // a thread of a message office did not take part in
	}
	if cur, _ := m.store.Channels().Thread(ctx, ch.ID, inKey(in.ChatID)); cur == conv {
		return // bound already (an open thread seen again on connect)
	}
	_ = m.store.Channels().SetThread(ctx, ch.ID, inKey(in.ChatID), conv)
	_ = m.store.Settings().Set(ctx, linkKey+conv, discordURL(in.GuildID, in.ChatID, ""))
}

// ThreadMaker is an adapter that can open a thread (Discord): from a message
// (fromMsg; the thread takes its id) or on its own; it returns the thread.
type ThreadMaker interface {
	MakeThread(ctx context.Context, chatID, fromMsg, name string) (string, error)
}

// makeThread answers /create-thread: a thread from the bot's answer replied to
// (its conversation goes on there), else from the command's own message (or,
// a slash command, on its own), which keeps one conversation.
func (m *Manager) makeThread(ctx context.Context, ch storage.Channel, ad Adapter, in Incoming, name string) string {
	tm, ok := ad.(ThreadMaker)
	if !ok || in.Private {
		return "Ở đây không tạo thread được."
	}
	conv := ""
	from := in.MessageID
	if in.ReplyTo == "" && in.Respond != nil { // from the "/" menu (no message it replies to): the latest answer here
		var last string
		if ok, _ := m.store.Settings().Get(ctx, lastKey(ch.ID, in.ChatID), &last); ok && last != "" {
			in.ReplyTo = last
		}
	}
	if in.ReplyTo != "" {
		from = in.ReplyTo
		if id, err := m.store.Channels().Thread(ctx, ch.ID, msgKey(in.ReplyTo)); err == nil {
			conv = id
		}
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Hội thoại"
		if conv != "" {
			if c, err := m.store.Chat().GetConversation(ctx, conv); err == nil && c.Title != "" {
				name = c.Title
			}
		}
	}
	if r := []rune(name); len(r) > 100 {
		name = string(r[:99]) + "…"
	}
	thread, err := tm.MakeThread(ctx, in.ChatID, from, name)
	if err != nil {
		return "Chưa tạo được thread (bot cần quyền Create Public Threads): " + err.Error()
	}
	if conv != "" {
		_ = m.store.Channels().SetThread(ctx, ch.ID, inKey(thread), conv)
		_ = m.store.Settings().Set(ctx, linkKey+conv, discordURL(in.GuildID, thread, ""))
	}
	m.setKeep(ctx, ch.ID, thread, true) // asked for: a conversation from the start, no tag needed there
	hint := "Đã mở thread: nhắn tiếp ở đây, không cần tag. Gõ /close-conversation nếu muốn phải tag mới trả lời."
	if conv != "" {
		hint = "Đã mở thread, tiếp tục hội thoại ở trên: nhắn tiếp ở đây, không cần tag. Gõ /close-conversation nếu muốn phải tag mới trả lời."
	}
	_, _ = ad.Send(ctx, thread, hint)
	if in.Respond != nil {
		return "Đã tạo thread <#" + thread + ">."
	}
	return ""
}

func closedKey(channelID, chatID string) string { return "channel_closed/" + channelID + "/" + chatID }

// startKeep turns on a kept conversation: it goes on with the conversation of
// the bot's latest answer here (tagging, then keeping: nothing lost), unless
// that one was closed with /close-conversation (then a fresh one).
func (m *Manager) startKeep(ctx context.Context, ch storage.Channel, chatID string) string {
	var last, closed string
	_, _ = m.store.Settings().Get(ctx, lastKey(ch.ID, chatID), &last)
	_, _ = m.store.Settings().Get(ctx, closedKey(ch.ID, chatID), &closed)
	msg := m.setKeep(ctx, ch.ID, chatID, true)
	if last == "" || last == closed {
		return msg
	}
	conv, _ := m.store.Channels().Thread(ctx, ch.ID, msgKey(last))
	if conv == "" {
		conv, _ = m.store.Channels().Thread(ctx, ch.ID, "msg:"+chatID+":"+last)
	}
	c, err := m.store.Chat().GetConversation(ctx, conv)
	if conv == "" || err != nil {
		return msg
	}
	if err := m.store.Channels().SetThread(ctx, ch.ID, chatID+"#"+c.AgentID+"#"+m.keep(ctx, ch.ID, chatID), conv); err != nil {
		return msg
	}
	return "Đã bật hội thoại, tiếp tục từ câu trả lời gần nhất của mình: từ giờ không cần tag, mình nhớ những gì đã nói. Gửi /close-conversation để kết thúc."
}

// stopKeep ends it; the next /create-conversation starts afresh.
func (m *Manager) stopKeep(ctx context.Context, ch storage.Channel, chatID string) string {
	var last string
	_, _ = m.store.Settings().Get(ctx, lastKey(ch.ID, chatID), &last)
	_ = m.store.Settings().Set(ctx, closedKey(ch.ID, chatID), last)
	return m.setKeep(ctx, ch.ID, chatID, false)
}

// saveFiles downloads what was sent with a message into the office's
// attachments (images, PDFs, text; the chat's limits). It returns their ids,
// and why the others were left out.
func (m *Manager) saveFiles(ctx context.Context, ch storage.Channel, files []InFile, by string) (ids, skipped []string) {
	store := m.engine.Attachments()
	for i, f := range files {
		if i >= attach.MaxPerSend {
			skipped = append(skipped, f.Name+" (tối đa "+strconv.Itoa(attach.MaxPerSend)+" file)")
			continue
		}
		if f.Size > attach.MaxSize {
			skipped = append(skipped, f.Name+": "+attach.ErrTooBig.Error())
			continue
		}
		data, err := f.Fetch(ctx)
		if err == nil {
			var meta attach.Meta
			if meta, err = store.Save(ch.ProjectID, by, f.Name, data); err == nil {
				ids = append(ids, meta.ID)
				continue
			}
		}
		skipped = append(skipped, f.Name+": "+err.Error())
	}
	return ids, skipped
}
