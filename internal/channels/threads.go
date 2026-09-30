package channels

import (
	"context"
	"strings"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

// Discord threads: a thread made from a message goes on with that message's
// conversation (the thread's id is the message's), and every message said in
// it is heard, as in a kept conversation. The conversation links to where it
// is on Discord: its first message, then its thread.

func msgKey(messageID string) string { return "thread:" + messageID } // a message → its conversation
func inKey(chatID string) string     { return "in:" + chatID }        // a thread → its conversation

const linkKey = "conv_link/"

// lastKey: the bot's latest answer in a chat (/create-thread from the "/" menu grows from it).
func lastKey(channelID, chatID string) string { return "channel_last/" + channelID + "/" + chatID }

// ConversationLink is where a conversation is on Discord ("" = nowhere).
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
	if convID == "" || msgID == "" || ch.Kind != "discord" {
		return
	}
	_ = m.store.Channels().SetThread(ctx, ch.ID, msgKey(msgID), convID)
	if ConversationLink(ctx, m.store, convID) == "" {
		_ = m.store.Settings().Set(ctx, linkKey+convID, discordURL(guild, chat, msgID))
	}
}

// threadMade binds a new thread to the conversation of the message it grew from.
func (m *Manager) threadMade(ctx context.Context, ch storage.Channel, in Incoming) {
	conv, err := m.store.Channels().Thread(ctx, ch.ID, msgKey(in.ThreadOf))
	if err != nil || conv == "" {
		return // a thread of a message office did not take part in
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
	} else {
		m.setKeep(ctx, ch.ID, thread, true) // a new thread: one conversation, all of it heard
	}
	_, _ = ad.Send(ctx, thread, "Đã mở thread: nhắn tiếp ở đây, không cần tag, mình nhớ những gì đã nói.")
	if in.Respond != nil {
		return "Đã tạo thread <#" + thread + ">."
	}
	return ""
}
