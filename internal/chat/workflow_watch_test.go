package chat

import (
	"context"

	"bitbucket.org/senprints/agent-office/internal/storage"
)

// blockingChatStore wraps a real ChatRepo but blocks ListMessages on started,
// until release is closed, to reproduce the window superviseOnce leaves
// unlocked between its first byConv check and the one that follows it.
type blockingChatStore struct {
	storage.ChatRepo
	started chan struct{}
	release chan struct{}
}

func (b *blockingChatStore) ListMessages(ctx context.Context, conversationID string) ([]storage.Message, error) {
	close(b.started)
	<-b.release
	return b.ChatRepo.ListMessages(ctx, conversationID)
}

type blockingStore struct {
	storage.Store
	chat *blockingChatStore
}

func (b blockingStore) Chat() storage.ChatRepo { return b.chat }
