package trigger_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/trigger"
)

// replies collects what the runner hands back to a channel (ADR-049).
type replies struct {
	mu  sync.Mutex
	got []reply
}

type reply struct {
	chat, text string
	err        error
	final      bool
}

func (rs *replies) hook(_ context.Context, origin storage.Job, text string, err error, final bool) {
	var p struct {
		ChatID string `json:"chat_id"`
	}
	_ = json.Unmarshal([]byte(origin.Payload), &p)
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.got = append(rs.got, reply{p.ChatID, text, err, final})
}

func channelPayload(conv string) string {
	raw, _ := json.Marshal(map[string]string{"message": "đơn 123 đâu rồi?", "user": "an", "user_id": "7", "chat_id": "42", "channel_id": "chn_1", "conversation_id": conv})
	return string(raw)
}

func runChannel(t *testing.T, r *trigger.Runner, st storage.Store, a storage.Automation, payload string) {
	t.Helper()
	if _, _, err := r.Enqueue(context.Background(), a, "telegram", payload, "", ""); err != nil {
		t.Fatal(err)
	}
	for range 3 { // the job, then an agent a script called in
		r.Tick(context.Background(), time.Now().UTC().Add(time.Second))
		r.Wait()
	}
}

// An agent answers in the outside chat's own conversation, and its answer
// goes back to that chat.
func TestChannelChatReplies(t *testing.T) {
	st, p := openStore(t)
	ex := &recExec{reply: "Đơn 123 đang giao"}
	r := trigger.New(st, ex)
	rs := &replies{}
	r.SetOnReply(rs.hook)
	a, _ := st.Automations().Create(context.Background(), storage.Automation{ProjectID: p.ID, Name: "Trả lời", Source: "telegram", Action: "chat", Enabled: true,
		Config: storage.AutomationConfig{ChannelID: "chn_1"}})
	runChannel(t, r, st, a, channelPayload("cnv_thread"))
	if len(ex.convs) != 1 || ex.convs[0] != "cnv_thread" || ex.prompts[0] != "đơn 123 đâu rồi?" {
		t.Fatalf("convs = %q prompts = %q", ex.convs, ex.prompts)
	}
	if len(rs.got) != 1 || rs.got[0] != (reply{"42", "Đơn 123 đang giao", nil, true}) {
		t.Fatalf("replies = %+v", rs.got)
	}
}

// A script's output is the answer (no AI); an agent it calls in answers after.
func TestChannelScriptReplies(t *testing.T) {
	st, p := openStore(t)
	ex := &recExec{reply: "Mình kiểm tra thêm rồi báo nhé"}
	r := trigger.New(st, ex)
	rs := &replies{}
	r.SetOnReply(rs.hook)
	a, _ := st.Automations().Create(context.Background(), storage.Automation{ProjectID: p.ID, Name: "Tra đơn", Source: "telegram", Action: "script", Enabled: true,
		Config:   storage.AutomationConfig{ChannelID: "chn_1"},
		Script:   storage.AutomationScript{Lang: "bash", Body: "echo 'Đơn 123: đang giao'; echo '@@agent: khách hỏi gấp'", TimeoutS: 30},
		Escalate: storage.AutomationEscalate{When: "signal", Action: "chat"}})
	runChannel(t, r, st, a, channelPayload(""))
	if len(rs.got) != 2 || rs.got[0] != (reply{"42", "Đơn 123: đang giao", nil, false}) || rs.got[1] != (reply{"42", "Mình kiểm tra thêm rồi báo nhé", nil, true}) {
		t.Fatalf("replies = %+v", rs.got)
	}
}

// A task's result goes back when the team is done.
func TestChannelTaskReplies(t *testing.T) {
	st, p := openStore(t)
	task, _ := st.Tasks().Create(context.Background(), storage.Task{ProjectID: p.ID, Title: "x", Goal: "x", Status: "done", Result: "Đã sửa lỗi thanh toán"})
	ex := &fakeExec{taskID: task.ID}
	r := trigger.New(st, ex)
	rs := &replies{}
	r.SetOnReply(rs.hook)
	a, _ := st.Automations().Create(context.Background(), storage.Automation{ProjectID: p.ID, Name: "Báo lỗi", Source: "telegram", Action: "task", Enabled: true,
		Config: storage.AutomationConfig{ChannelID: "chn_1"}})
	runChannel(t, r, st, a, channelPayload(""))
	if len(rs.got) != 1 || rs.got[0].text != "Đã sửa lỗi thanh toán" || !rs.got[0].final {
		t.Fatalf("replies = %+v", rs.got)
	}
	if len(ex.actors) != 1 || ex.actors[0] != "telegram:an" { // the person who wrote, not the automation
		t.Fatalf("task asked by %v", ex.actors)
	}
}

// /job: a message forced to be a task, whatever the rule's own action.
func TestChannelJobIsATask(t *testing.T) {
	st, p := openStore(t)
	ex := &fakeExec{}
	r := trigger.New(st, ex)
	a, _ := st.Automations().Create(context.Background(), storage.Automation{ProjectID: p.ID, Name: "Trả lời", Source: "telegram", Action: "chat", Enabled: true,
		Config: storage.AutomationConfig{ChannelID: "chn_1"}})
	raw, _ := json.Marshal(map[string]string{"message": "sửa lỗi thanh toán", "user": "an", "chat_id": "42", "channel_id": "chn_1", "action": "task"})
	runChannel(t, r, st, a, string(raw))
	if len(ex.tasks) != 1 || len(ex.chats) != 0 || !strings.Contains(ex.tasks[0], "sửa lỗi thanh toán") {
		t.Fatalf("tasks = %v chats = %v", ex.tasks, ex.chats)
	}
}
