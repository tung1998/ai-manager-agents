package channels

import (
	"context"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

// Discord threads: a thread made from a message goes on with that message's
// conversation (the thread's id is the message's), and every message said in
// it is heard, as in a kept conversation. The conversation links to where it
// is on Discord: its first message, then its thread.

func msgKey(messageID string) string { return "thread:" + messageID } // a message → its conversation
func inKey(chatID string) string     { return "in:" + chatID }        // a thread → its conversation

const linkKey = "conv_link/"

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
