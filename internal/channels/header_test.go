package channels_test

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/channels"
	"bitbucket.org/senprints/agent-office/internal/chat"
	"bitbucket.org/senprints/agent-office/internal/llm"
	"bitbucket.org/senprints/agent-office/internal/orgmodel"
	"bitbucket.org/senprints/agent-office/internal/provider"
	"bitbucket.org/senprints/agent-office/internal/secrets"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/storage/sqlite"
	"bitbucket.org/senprints/agent-office/internal/trigger"
	"bitbucket.org/senprints/agent-office/internal/usage"
)

// The line on top of a bot's answer: its template, the parts it has, off.
func TestHeaderLine(t *testing.T) {
	for _, c := range []struct{ tpl, kind, agent, project, branch, want string }{
		{"", "discord", "Trợ lý", "shop", "main", "-# Trợ lý · shop · main"},
		{"", "telegram", "Trợ lý", "shop", "main", "Trợ lý · shop · main"},
		{"", "discord", "", "shop", "", "-# shop"}, // a script's answer: no agent; not a git folder
		{"{project} ({branch})", "discord", "A", "shop", "dev", "-# shop (dev)"},
		{"-", "discord", "A", "shop", "dev", ""},
	} {
		if got := channels.HeaderLine(c.tpl, c.kind, c.agent, c.project, c.branch); got != c.want {
			t.Errorf("%q %s: %q, want %q", c.tpl, c.kind, got, c.want)
		}
	}
}

// A bot's answer starts with who answered, in which project and branch; a
// follow-up of another agent names that agent instead of signing the text.
func TestReplyHeader(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	st, _ := sqlite.Open(filepath.Join(tmp, "o.db"))
	defer st.Close()
	st.Migrate(ctx)
	box, _ := secrets.Load(filepath.Join(tmp, "k"))
	provs := provider.NewService(st, box, llm.Options{})
	engine := chat.NewEngine(st, provs, usage.New(st, time.UTC))
	org := orgmodel.NewService(st)
	org.SeedBuiltins(ctx)
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", "-b", "feature-x", dir).CombinedOutput(); err != nil {
		t.Skip("git:", string(out))
	}
	project, _ := st.Repos().Create(ctx, storage.Repo{Name: "shop", Path: dir})
	solo, _ := st.OrgModels().GetTemplateByKey(ctx, "solo")
	org.ApplyToRepo(ctx, project.ID, solo.ID, false)
	agents, _ := engine.Agents(ctx, project.ID)
	st.Agents().Create(ctx, storage.Agent{OrgModelID: agents[0].OrgModelID, Key: "tester", Name: "Tester", Tier: "worker", ReportsTo: []string{agents[0].Key}, ModelTier: "fast"})
	bot := &fakeBot{in: make(chan channels.Incoming, 4), sent: map[string][]string{}}
	ch, _ := st.Channels().Create(ctx, storage.Channel{ProjectID: project.ID, Kind: "discord", Name: "Dev", Enabled: true})
	m := channels.NewManager(st, engine, trigger.New(st, chatExec{engine}), func(storage.Channel) (channels.Adapter, error) { return bot, nil })
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	m.Start(runCtx)
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if c, _ := st.Channels().Get(ctx, ch.ID); c.BotName != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the bot did not start")
		}
	}
	conv, _ := engine.StartConversationPurpose(ctx, project.ID, "", "channel")
	payload, _ := json.Marshal(trigger.ChannelPayload{ChannelID: ch.ID, ChatID: "42", ConversationID: conv.ID})
	m.Reply(ctx, storage.Job{ID: "job_1", Payload: string(payload)}, "Đã xem xong", nil, false)
	got := bot.wait(t, "42", 1)
	if got[0] != "-# "+conv.AgentName+" · shop · feature-x\nĐã xem xong" {
		t.Fatalf("answer = %q", got[0])
	}
	m.Reply(ctx, storage.Job{ID: "job_1", Payload: string(payload)}, "Tester:\nTest đạt", nil, false)
	got = bot.wait(t, "42", 2)
	if got[1] != "-# Tester · shop · feature-x\nTest đạt" {
		t.Fatalf("follow-up = %q", got[1])
	}
	ch.Header = "-"
	st.Channels().Update(ctx, ch)
	m.Reply(ctx, storage.Job{ID: "job_1", Payload: string(payload)}, "Xong rồi", nil, false)
	if got = bot.wait(t, "42", 3); got[2] != "Xong rồi" || strings.Contains(got[2], "-#") {
		t.Fatalf("off = %q", got[2])
	}
}

// slowBot takes its time to connect, as Discord does.
type slowBot struct {
	fakeBot
	ready chan struct{}
}

func (b *slowBot) Run(ctx context.Context, onReady func(string), onMessage func(channels.Incoming)) error {
	select {
	case <-b.ready:
	case <-ctx.Done():
		return nil
	}
	return b.fakeBot.Run(ctx, onReady, onMessage)
}

// A bot being connected says so (the dashboard follows it), then runs.
func TestBotState(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	st, _ := sqlite.Open(filepath.Join(tmp, "o.db"))
	defer st.Close()
	st.Migrate(ctx)
	box, _ := secrets.Load(filepath.Join(tmp, "k"))
	engine := chat.NewEngine(st, provider.NewService(st, box, llm.Options{}), usage.New(st, time.UTC))
	project, _ := st.Repos().Create(ctx, storage.Repo{Name: "shop", Path: t.TempDir()})
	ch, _ := st.Channels().Create(ctx, storage.Channel{ProjectID: project.ID, Kind: "discord", Name: "Dev", Enabled: true})
	off, _ := st.Channels().Create(ctx, storage.Channel{ProjectID: project.ID, Kind: "discord", Name: "Off"})
	bot := &slowBot{fakeBot: fakeBot{in: make(chan channels.Incoming), sent: map[string][]string{}}, ready: make(chan struct{})}
	m := channels.NewManager(st, engine, trigger.New(st, chatExec{engine}), func(storage.Channel) (channels.Adapter, error) { return bot, nil })
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	m.Start(runCtx)
	for deadline := time.Now().Add(2 * time.Second); m.State(ch.ID) != "connecting"; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("state = %q, want connecting", m.State(ch.ID))
		}
	}
	close(bot.ready)
	for deadline := time.Now().Add(2 * time.Second); m.State(ch.ID) != "running"; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("state = %q, want running", m.State(ch.ID))
		}
	}
	if s := m.State(off.ID); s != "" {
		t.Fatalf("an off bot: %q", s)
	}
}

// A thread made from a message the bot answered goes on with that message's
// conversation, and the conversation links to the thread on Discord.
func TestThreadFromAnswer(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	st, _ := sqlite.Open(filepath.Join(tmp, "o.db"))
	defer st.Close()
	st.Migrate(ctx)
	box, _ := secrets.Load(filepath.Join(tmp, "k"))
	engine := chat.NewEngine(st, provider.NewService(st, box, llm.Options{}), usage.New(st, time.UTC))
	org := orgmodel.NewService(st)
	org.SeedBuiltins(ctx)
	project, _ := st.Repos().Create(ctx, storage.Repo{Name: "shop", Path: t.TempDir()})
	solo, _ := st.OrgModels().GetTemplateByKey(ctx, "solo")
	org.ApplyToRepo(ctx, project.ID, solo.ID, false)
	bot := &fakeBot{in: make(chan channels.Incoming, 4), sent: map[string][]string{}}
	ch, _ := st.Channels().Create(ctx, storage.Channel{ProjectID: project.ID, Kind: "discord", Name: "Dev", Enabled: true, Allow: []string{"*"}, Header: "-"})
	m := channels.NewManager(st, engine, trigger.New(st, chatExec{engine}), func(storage.Channel) (channels.Adapter, error) { return bot, nil })
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	m.Start(runCtx)
	for deadline := time.Now().Add(5 * time.Second); m.State(ch.ID) != "running"; time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the bot did not start")
		}
	}
	conv, _ := engine.StartConversationPurpose(ctx, project.ID, "", "channel")
	payload, _ := json.Marshal(trigger.ChannelPayload{ChannelID: ch.ID, ChatID: "c2", ConversationID: conv.ID})
	m.Reply(ctx, storage.Job{ID: "job_1", Payload: string(payload)}, "Đây là kết quả", nil, false)
	bot.wait(t, "c2", 1) // the answer is message "1" of c2
	bot.in <- channels.Incoming{ChatID: "1", GuildID: "g", ThreadOf: "1"}
	for deadline := time.Now().Add(3 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if id, _ := st.Channels().Thread(ctx, ch.ID, "in:1"); id == conv.ID {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the thread is not the conversation's")
		}
	}
	if got := channels.ConversationLink(ctx, st, conv.ID); got != "https://discord.com/channels/g/1" {
		t.Fatalf("link = %q", got)
	}
	// an answer remembered before threads were (only its reply key): found through the thread's channel
	old, _ := engine.StartConversationPurpose(ctx, project.ID, "", "channel")
	st.Channels().SetThread(ctx, ch.ID, "msg:c2:77", old.ID)
	bot.in <- channels.Incoming{ChatID: "77", GuildID: "g", ThreadOf: "77", ParentID: "c2"}
	for deadline := time.Now().Add(3 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if id, _ := st.Channels().Thread(ctx, ch.ID, "in:77"); id == old.ID {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("an old answer's thread is not its conversation's")
		}
	}
}

// threadBot makes threads as Discord does: from a message, the thread's id is the message's.
type threadBot struct {
	fakeBot
	made chan [3]string // chat, from message, name
}

func (b *threadBot) MakeThread(_ context.Context, chatID, fromMsg, name string) (string, error) {
	b.made <- [3]string{chatID, fromMsg, name}
	if fromMsg != "" {
		return fromMsg, nil
	}
	return "t-new", nil
}

// /create-thread (a reply to the bot's answer) makes a thread from that
// answer: its conversation goes on there. Asked for only: nothing else makes one.
func TestCreateThreadCommand(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	st, _ := sqlite.Open(filepath.Join(tmp, "o.db"))
	defer st.Close()
	st.Migrate(ctx)
	box, _ := secrets.Load(filepath.Join(tmp, "k"))
	engine := chat.NewEngine(st, provider.NewService(st, box, llm.Options{}), usage.New(st, time.UTC))
	project, _ := st.Repos().Create(ctx, storage.Repo{Name: "shop", Path: t.TempDir()})
	org := orgmodel.NewService(st)
	org.SeedBuiltins(ctx)
	solo, _ := st.OrgModels().GetTemplateByKey(ctx, "solo")
	org.ApplyToRepo(ctx, project.ID, solo.ID, false)
	bot := &threadBot{fakeBot: fakeBot{in: make(chan channels.Incoming, 4), sent: map[string][]string{}}, made: make(chan [3]string, 2)}
	ch, _ := st.Channels().Create(ctx, storage.Channel{ProjectID: project.ID, Kind: "discord", Name: "Dev", Enabled: true, Allow: []string{"*"}, Header: "-"})
	m := channels.NewManager(st, engine, trigger.New(st, chatExec{engine}), func(storage.Channel) (channels.Adapter, error) { return bot, nil })
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	m.Start(runCtx)
	for deadline := time.Now().Add(5 * time.Second); m.State(ch.ID) != "running"; time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the bot did not start")
		}
	}
	conv, err := engine.StartConversationPurpose(ctx, project.ID, "", "channel")
	if err != nil || conv.ID == "" {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(trigger.ChannelPayload{ChannelID: ch.ID, ChatID: "c2", ConversationID: conv.ID})
	m.Reply(ctx, storage.Job{ID: "job_1", Payload: string(payload)}, "Kế hoạch migrate", nil, false)
	bot.wait(t, "c2", 1)                                                                   // message "1"
	for deadline := time.Now().Add(3 * time.Second); ; time.Sleep(10 * time.Millisecond) { // remembered once sent
		if id, _ := st.Channels().Thread(ctx, ch.ID, "thread:1"); id == conv.ID {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the answer is not remembered")
		}
	}
	bot.in <- channels.Incoming{ChatID: "c2", GuildID: "g", MessageID: "u9", UserID: "8", Text: "/create-thread Migrate admin", ReplyTo: "1", Addressed: true}
	select {
	case got := <-bot.made:
		if got != [3]string{"c2", "1", "Migrate admin"} {
			t.Fatalf("made = %v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no thread made")
	}
	if got := bot.wait(t, "1", 1); !strings.Contains(got[0], "thread") {
		t.Fatalf("in the thread = %q", got)
	}
	if id, _ := st.Channels().Thread(ctx, ch.ID, "in:1"); id != conv.ID {
		t.Fatalf("the thread's conversation = %q", id)
	}
	if got := channels.ConversationLink(ctx, st, conv.ID); got != "https://discord.com/channels/g/1" {
		t.Fatalf("link = %q", got)
	}
	var keep string // /create-thread starts as a conversation: no tag needed there
	if ok, _ := st.Settings().Get(ctx, "channel_keep/"+ch.ID+"/1", &keep); !ok || keep == "" {
		t.Fatal("the new thread is not a kept conversation")
	}
}

// /create-thread from the "/" menu (Discord sends no message it replies to):
// the thread grows from the bot's latest answer here, with its conversation.
func TestCreateThreadSlash(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	st, _ := sqlite.Open(filepath.Join(tmp, "o.db"))
	defer st.Close()
	st.Migrate(ctx)
	box, _ := secrets.Load(filepath.Join(tmp, "k"))
	engine := chat.NewEngine(st, provider.NewService(st, box, llm.Options{}), usage.New(st, time.UTC))
	project, _ := st.Repos().Create(ctx, storage.Repo{Name: "shop", Path: t.TempDir()})
	org := orgmodel.NewService(st)
	org.SeedBuiltins(ctx)
	solo, _ := st.OrgModels().GetTemplateByKey(ctx, "solo")
	org.ApplyToRepo(ctx, project.ID, solo.ID, false)
	bot := &threadBot{fakeBot: fakeBot{in: make(chan channels.Incoming, 4), sent: map[string][]string{}}, made: make(chan [3]string, 2)}
	ch, _ := st.Channels().Create(ctx, storage.Channel{ProjectID: project.ID, Kind: "discord", Name: "Dev", Enabled: true, Allow: []string{"*"}, Header: "-"})
	m := channels.NewManager(st, engine, trigger.New(st, chatExec{engine}), func(storage.Channel) (channels.Adapter, error) { return bot, nil })
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	m.Start(runCtx)
	for deadline := time.Now().Add(5 * time.Second); m.State(ch.ID) != "running"; time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the bot did not start")
		}
	}
	conv, err := engine.StartConversationPurpose(ctx, project.ID, "", "channel")
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(trigger.ChannelPayload{ChannelID: ch.ID, ChatID: "c2", ConversationID: conv.ID})
	m.Reply(ctx, storage.Job{ID: "job_1", Payload: string(payload)}, "Câu trả lời", nil, false)
	bot.wait(t, "c2", 1) // message "1"
	for deadline := time.Now().Add(3 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if id, _ := st.Channels().Thread(ctx, ch.ID, "thread:1"); id == conv.ID {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the answer is not remembered")
		}
	}
	responded := make(chan string, 1)
	bot.in <- channels.Incoming{ChatID: "c2", GuildID: "g", UserID: "8", Text: "/create-thread", Addressed: true,
		Respond: func(_ context.Context, text string) (string, error) { responded <- text; return "r1", nil }}
	select {
	case got := <-bot.made:
		if got[0] != "c2" || got[1] != "1" {
			t.Fatalf("made = %v, want from the latest answer", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no thread made")
	}
	<-responded
	if id, _ := st.Channels().Thread(ctx, ch.ID, "in:1"); id != conv.ID {
		t.Fatalf("the thread's conversation = %q", id)
	}
}

// In a thread of a conversation the bot still wants a tag, until
// /create-conversation there (that thread only), which keeps its conversation.
func TestThreadNeedsTag(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	st, _ := sqlite.Open(filepath.Join(tmp, "o.db"))
	defer st.Close()
	st.Migrate(ctx)
	box, _ := secrets.Load(filepath.Join(tmp, "k"))
	engine := chat.NewEngine(st, provider.NewService(st, box, llm.Options{}), usage.New(st, time.UTC))
	project, _ := st.Repos().Create(ctx, storage.Repo{Name: "shop", Path: t.TempDir()})
	org := orgmodel.NewService(st)
	org.SeedBuiltins(ctx)
	solo, _ := st.OrgModels().GetTemplateByKey(ctx, "solo")
	org.ApplyToRepo(ctx, project.ID, solo.ID, false)
	bot := &fakeBot{in: make(chan channels.Incoming, 4), sent: map[string][]string{}}
	ch, _ := st.Channels().Create(ctx, storage.Channel{ProjectID: project.ID, Kind: "discord", Name: "Dev", Enabled: true, Allow: []string{"*"}, Header: "-"})
	m := channels.NewManager(st, engine, trigger.New(st, chatExec{engine}), func(storage.Channel) (channels.Adapter, error) { return bot, nil })
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	m.Start(runCtx)
	for deadline := time.Now().Add(5 * time.Second); m.State(ch.ID) != "running"; time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the bot did not start")
		}
	}
	conv, _ := engine.StartConversationPurpose(ctx, project.ID, "", "channel")
	st.Channels().SetThread(ctx, ch.ID, "in:t1", conv.ID)                        // a thread of the conversation
	bot.in <- channels.Incoming{ChatID: "t1", UserID: "8", Text: "còn đó không"} // untagged: not for the bot
	bot.in <- channels.Incoming{ChatID: "t1", UserID: "8", Text: "/create-conversation", Addressed: true}
	got := bot.wait(t, "t1", 1)
	if len(got) != 1 || !strings.Contains(got[0], "hội thoại") {
		t.Fatalf("thread = %q (the untagged message was answered?)", got)
	}
	if id, _ := st.Channels().Thread(ctx, ch.ID, "in:t1"); id != conv.ID {
		t.Fatal("/create-conversation took the thread off its conversation")
	}
}

// Every message in a thread is one conversation (the thread's), tagged or
// not kept: the first tag makes it, the next ones go on in it.
func TestThreadIsOneConversation(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	st, _ := sqlite.Open(filepath.Join(tmp, "o.db"))
	defer st.Close()
	st.Migrate(ctx)
	box, _ := secrets.Load(filepath.Join(tmp, "k"))
	engine := chat.NewEngine(st, provider.NewService(st, box, llm.Options{}), usage.New(st, time.UTC))
	project, _ := st.Repos().Create(ctx, storage.Repo{Name: "shop", Path: t.TempDir()})
	org := orgmodel.NewService(st)
	org.SeedBuiltins(ctx)
	solo, _ := st.OrgModels().GetTemplateByKey(ctx, "solo")
	org.ApplyToRepo(ctx, project.ID, solo.ID, false)
	ch, _ := st.Channels().Create(ctx, storage.Channel{ProjectID: project.ID, Kind: "discord", Name: "Dev", Enabled: true, Allow: []string{"*"}})
	m := channels.NewManager(st, engine, trigger.New(st, chatExec{engine}), nil)
	a, _ := m.ConversationFor(ctx, ch, channels.Incoming{ChatID: "t9", InThread: true, Text: "một", Addressed: true})
	b, _ := m.ConversationFor(ctx, ch, channels.Incoming{ChatID: "t9", InThread: true, Text: "hai", Addressed: true})
	c, _ := m.ConversationFor(ctx, ch, channels.Incoming{ChatID: "c2", Text: "ba", Addressed: true})
	if a == "" || a != b {
		t.Fatalf("two tags in one thread: %q %q", a, b)
	}
	if c == a {
		t.Fatal("a channel message went into the thread's conversation")
	}
}

// A kept thread (/create-conversation there, or /create-thread) keeps its
// conversation: being a thread does not start another one.
func TestKeptThreadKeepsItsConversation(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	st, _ := sqlite.Open(filepath.Join(tmp, "o.db"))
	defer st.Close()
	st.Migrate(ctx)
	box, _ := secrets.Load(filepath.Join(tmp, "k"))
	engine := chat.NewEngine(st, provider.NewService(st, box, llm.Options{}), usage.New(st, time.UTC))
	project, _ := st.Repos().Create(ctx, storage.Repo{Name: "shop", Path: t.TempDir()})
	org := orgmodel.NewService(st)
	org.SeedBuiltins(ctx)
	solo, _ := st.OrgModels().GetTemplateByKey(ctx, "solo")
	org.ApplyToRepo(ctx, project.ID, solo.ID, false)
	ch, _ := st.Channels().Create(ctx, storage.Channel{ProjectID: project.ID, Kind: "discord", Name: "Dev", Enabled: true, Allow: []string{"*"}})
	m := channels.NewManager(st, engine, trigger.New(st, chatExec{engine}), nil)
	st.Settings().Set(ctx, "channel_keep/"+ch.ID+"/t5", "gen1")
	kept, _ := m.ConversationFor(ctx, ch, channels.Incoming{ChatID: "t5", Addressed: true}) // before the bot knew it was a thread
	again, _ := m.ConversationFor(ctx, ch, channels.Incoming{ChatID: "t5", InThread: true, Addressed: true})
	if kept == "" || again != kept {
		t.Fatalf("kept %q, then %q", kept, again)
	}
}

// /create-conversation goes on with the latest answer's conversation (tagging,
// then keeping: nothing lost); after /close-conversation it starts afresh.
func TestCreateConversationContinues(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	st, _ := sqlite.Open(filepath.Join(tmp, "o.db"))
	defer st.Close()
	st.Migrate(ctx)
	box, _ := secrets.Load(filepath.Join(tmp, "k"))
	engine := chat.NewEngine(st, provider.NewService(st, box, llm.Options{}), usage.New(st, time.UTC))
	project, _ := st.Repos().Create(ctx, storage.Repo{Name: "shop", Path: t.TempDir()})
	org := orgmodel.NewService(st)
	org.SeedBuiltins(ctx)
	solo, _ := st.OrgModels().GetTemplateByKey(ctx, "solo")
	org.ApplyToRepo(ctx, project.ID, solo.ID, false)
	bot := &fakeBot{in: make(chan channels.Incoming, 4), sent: map[string][]string{}}
	ch, _ := st.Channels().Create(ctx, storage.Channel{ProjectID: project.ID, Kind: "discord", Name: "Dev", Enabled: true, Allow: []string{"*"}, Header: "-"})
	m := channels.NewManager(st, engine, trigger.New(st, chatExec{engine}), func(storage.Channel) (channels.Adapter, error) { return bot, nil })
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	m.Start(runCtx)
	for deadline := time.Now().Add(5 * time.Second); m.State(ch.ID) != "running"; time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the bot did not start")
		}
	}
	tagged, _ := m.ConversationFor(ctx, ch, channels.Incoming{ChatID: "c2", Addressed: true})
	payload, _ := json.Marshal(trigger.ChannelPayload{ChannelID: ch.ID, ChatID: "c2", ConversationID: tagged})
	m.Reply(ctx, storage.Job{ID: "job_1", Payload: string(payload)}, "Câu trả lời", nil, false)
	bot.wait(t, "c2", 1)
	for deadline := time.Now().Add(3 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		var last string
		if ok, _ := st.Settings().Get(ctx, "channel_last/"+ch.ID+"/c2", &last); ok && last != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the answer is not remembered")
		}
	}
	bot.in <- channels.Incoming{ChatID: "c2", UserID: "8", Text: "/create-conversation", Addressed: true}
	got := bot.wait(t, "c2", 2)
	if !strings.Contains(got[1], "tiếp tục") {
		t.Fatalf("create = %q", got[1])
	}
	if kept, _ := m.ConversationFor(ctx, ch, channels.Incoming{ChatID: "c2"}); kept != tagged {
		t.Fatalf("kept %q, want the tagged answer's %q", kept, tagged)
	}
	bot.in <- channels.Incoming{ChatID: "c2", UserID: "8", Text: "/close-conversation", Addressed: true}
	bot.wait(t, "c2", 3)
	bot.in <- channels.Incoming{ChatID: "c2", UserID: "8", Text: "/create-conversation", Addressed: true}
	bot.wait(t, "c2", 4)
	if fresh, _ := m.ConversationFor(ctx, ch, channels.Incoming{ChatID: "c2"}); fresh == tagged || fresh == "" {
		t.Fatalf("after close, a fresh one: %q", fresh)
	}
}
