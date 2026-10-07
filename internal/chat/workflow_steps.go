package chat

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"bitbucket.org/senprints/agent-office/internal/actions"
	"bitbucket.org/senprints/agent-office/internal/actor"
	"bitbucket.org/senprints/agent-office/internal/perm"
	"bitbucket.org/senprints/agent-office/internal/storage"
	"bitbucket.org/senprints/agent-office/internal/trigger"
	"bitbucket.org/senprints/agent-office/internal/workflow"
)

// A graph of steps (ADR-108): the office runs the workflow's steps in
// order in the run's own chat, without a coordinator: an agent's turn, a
// sub-workflow, code, an HTTP request, a condition, a person's approval, a
// check command, the end. Each step's result is a message of that chat and
// data for the steps after it.

// stepAuthor signs what the runner itself posts in the run's chat.
const stepAuthor = "⚙ Quy trình" // i18n-ignore

// launch starts a prepared run in its own chat: a coordinator's first turn,
// or the runner of its steps.
func (e *Engine) launch(ctx context.Context, own storage.Conversation, run *wfRun, text, prompt, pageContext string, attachmentIDs []string) error {
	if !run.def.StepMode() {
		_, _, err := e.SendWithContext(withPrepared(ctx, &prepared{conv: own.ID, run: run, prompt: prompt}), own.ID, text, pageContext, attachmentIDs)
		return err
	}
	if _, err := e.store.Chat().AddMessage(ctx, storage.Message{ConversationID: own.ID, Role: "user", Content: text, Author: actor.From(ctx)}); err != nil {
		return err
	}
	if run.inputs == nil {
		run.inputs = inputsFrom(run.def, run.rec.Input)
	}
	e.beginRun(own, run, actor.From(ctx), ModelTierFrom(ctx), ceilingOf(ctx))
	sctx, stop := context.WithCancel(context.Background())
	e.wf.mu.Lock()
	run.stop = stop
	e.wf.mu.Unlock()
	go e.runSteps(sctx, own, run)
	return nil
}

// inputsFrom reads a chat's request as a workflow's inputs: a JSON object,
// or "key: value" lines for the keys it declares; the whole text is "text".
func inputsFrom(d workflow.Def, text string) map[string]string {
	out := map[string]string{"text": strings.TrimSpace(text)}
	var obj map[string]any
	if json.Unmarshal([]byte(strings.TrimSpace(text)), &obj) == nil {
		for k, v := range obj {
			if s, ok := v.(string); ok {
				out[k] = s
			} else {
				b, _ := json.Marshal(v)
				out[k] = string(b)
			}
		}
		return out
	}
	for _, line := range strings.Split(text, "\n") {
		k, v, ok := strings.Cut(line, ":")
		k = strings.TrimSpace(k)
		for _, f := range d.Inputs {
			if ok && f.Key == k {
				out[k] = strings.TrimSpace(v)
			}
		}
	}
	if len(d.Inputs) > 0 && out[d.Inputs[0].Key] == "" { // one declared input: the whole text is it
		out[d.Inputs[0].Key] = out["text"]
	}
	return out
}

// runSteps runs a graph of steps to its end (or until the run is stopped).
func (e *Engine) runSteps(ctx context.Context, own storage.Conversation, run *wfRun) {
	vars := workflow.Vars{Input: run.inputs, Steps: map[string]workflow.StepResult{}}
	visits := map[string]int{}
	cur := run.def.Steps[0].ID
	fail := func(why string) {
		if ctx.Err() == nil {
			e.note(own, "⚠️ "+why)
			e.finishRun(own.ID, run.rec.ID, storage.RunFailed, "", why)
		}
	}
	for n := 0; ; n++ {
		if ctx.Err() != nil {
			return
		}
		if n >= workflow.MaxStepsRun {
			fail(fmt.Sprintf("quá %d bước trong một lần chạy", workflow.MaxStepsRun))
			return
		}
		s, ok := run.def.Step(cur)
		if !ok {
			fail("không có bước " + cur)
			return
		}
		visits[cur]++
		if most := cmp.Or(s.MaxLoops, workflow.DefaultMaxLoops); visits[cur] > most+1 { // a way back is a loop: bounded
			fail(fmt.Sprintf("bước %s lặp quá %d lần (max_loops)", workflow.StepLabel(s), most))
			return
		}
		e.wf.mu.Lock()
		run.logf("Bước %s", workflow.StepLabel(s))
		rec := run.rec
		e.wf.mu.Unlock()
		e.saveRun(rec)
		if s.Type == workflow.StepEnd {
			e.endSteps(own, run, s, vars)
			return
		}
		if s.Type == workflow.StepCondition {
			next := s.Else
			if workflow.Eval(s.If, vars) {
				next = s.Then
			}
			vars.Steps[s.ID] = workflow.NewResult(next, strconv.FormatBool(next == s.Then))
			cur = next
			continue
		}
		res, err := e.runStep(ctx, own, run, s, vars)
		if ctx.Err() != nil {
			return
		}
		vars.Steps[s.ID] = res
		switch {
		case err == nil:
			cur = s.Next
		case errors.Is(err, workflow.ErrStepFailed) && s.Else != "": // a check that failed, an approval refused
			cur = s.Else
		case s.OnError == "continue":
			e.note(own, fmt.Sprintf("Bước %s lỗi, đi tiếp: %s", workflow.StepLabel(s), err))
			cur = s.Next
		default:
			fail(fmt.Sprintf("bước %s: %s", workflow.StepLabel(s), err))
			return
		}
	}
}

// post puts a step's result in the run's chat.
func (e *Engine) post(own storage.Conversation, s workflow.Step, text string) {
	_, _ = e.store.Chat().AddMessage(context.Background(), storage.Message{ConversationID: own.ID, Role: "assistant", Author: stepAuthor,
		Content: "**" + workflow.StepLabel(s) + "**\n\n" + text})
}

func fence(s string) string {
	s = truncate(strings.TrimSpace(s), 4000)
	if s == "" {
		return "_(trống)_" // i18n-ignore
	}
	return "```\n" + strings.ReplaceAll(s, "```", "'''") + "\n```"
}

// runStep runs one step but a condition or the end.
func (e *Engine) runStep(ctx context.Context, own storage.Conversation, run *wfRun, s workflow.Step, vars workflow.Vars) (workflow.StepResult, error) {
	project, err := e.store.Repos().Get(ctx, run.rec.ProjectID)
	if err != nil {
		return workflow.StepResult{}, err
	}
	switch s.Type {
	case workflow.StepAgent:
		out, err := e.stepAgent(ctx, own, project, run, s, workflow.Render(s.Prompt, vars))
		return workflow.NewResult(out, ""), err
	case workflow.StepWorkflow:
		inputs := map[string]string{}
		for k, v := range s.Inputs {
			inputs[k] = workflow.Render(v, vars)
		}
		rec, err := e.stepFlow(ctx, own, project, run, s, inputs)
		r := workflow.NewResult(strings.TrimSpace(rec.Result), rec.Status)
		if len(rec.Outputs) > 0 {
			r.JSON = rec.Outputs
			b, _ := json.Marshal(rec.Outputs)
			if r.Output == "" {
				r.Output = string(b)
			}
		}
		return r, err
	case workflow.StepCode:
		data, _ := json.Marshal(vars)
		env := []string{"OFFICE_RUN=" + run.rec.ID}
		for k, v := range vars.Input {
			env = append(env, "OFFICE_INPUT_"+strings.ToUpper(strings.ReplaceAll(k, "-", "_"))+"="+v)
		}
		out, code, timedOut, err := trigger.RunScript(ctx, project.Path, storage.AutomationScript{Lang: s.Lang, Body: s.Script, TimeoutS: s.TimeoutS}, env, string(data))
		e.post(own, s, fmt.Sprintf("Thoát mã %d\n%s", code, fence(out))) // i18n-ignore
		res := workflow.NewResult(strings.TrimSpace(out), strconv.Itoa(code))
		switch {
		case err != nil:
			return res, err
		case timedOut:
			return res, errors.New("hết thời gian")
		case code != 0:
			return res, fmt.Errorf("%w: thoát mã %d", workflow.ErrStepFailed, code)
		}
		return res, nil
	case workflow.StepHTTP:
		r := s
		r.URL, r.Body = workflow.Render(s.URL, vars), workflow.Render(s.Body, vars)
		r.Headers = map[string]string{}
		for k, v := range s.Headers {
			r.Headers[k] = workflow.Render(v, vars)
		}
		code, body, err := workflow.DoHTTP(ctx, r)
		e.post(own, s, fmt.Sprintf("%s %s → %d\n%s", cmp.Or(strings.ToUpper(r.Method), "GET"), r.URL, code, fence(body)))
		res := workflow.NewResult(body, strconv.Itoa(code))
		switch {
		case err != nil:
			return res, err
		case code >= 400:
			return res, fmt.Errorf("%w: HTTP %d", workflow.ErrStepFailed, code)
		}
		return res, nil
	case workflow.StepApprove:
		return e.stepDecide(ctx, own, run, s, "workflow_gate", run.def.Name+": "+workflow.StepLabel(s), workflow.Render(s.Note, vars))
	case workflow.StepCheck:
		return e.stepDecide(ctx, own, run, s, "run_command", workflow.Render(s.Command, vars), "Bước "+workflow.StepLabel(s)+" của quy trình "+run.def.Name)
	}
	return workflow.StepResult{}, fmt.Errorf("bước loại %q không chạy được", s.Type)
}

// stepAgent runs an agent's turn for a step and waits for its answer.
func (e *Engine) stepAgent(ctx context.Context, own storage.Conversation, project storage.Repo, run *wfRun, s workflow.Step, prompt string) (string, error) {
	d, _ := run.def.Role(s.Role)
	a, err := e.pickAgent(ctx, run, s.Role, "")
	if err != nil {
		return "", err
	}
	level := accessLevel(d.Access)
	if run.ceiling != "" {
		level = perm.Min(level, run.ceiling)
	}
	key := run.rec.ID + "#" + s.ID
	ch := make(chan string, 1)
	e.wf.mu.Lock()
	e.wf.waiting[key] = ch
	if r := run.role(s.Role); r != nil {
		r.Status, r.AgentID, r.AgentName = "working", a.ID, a.Name
		r.Turns++
	}
	run.rec.Turns++
	rec := run.rec
	e.wf.mu.Unlock()
	e.saveRun(rec)
	done := func(status string) {
		e.wf.mu.Lock()
		delete(e.wf.waiting, key)
		if r := run.role(s.Role); r != nil {
			r.Status = status
		}
		rec := run.rec
		e.wf.mu.Unlock()
		e.saveRun(rec)
	}
	t, why := e.startTurn(own, project, turnSpec{agent: a, background: true, actor: run.rec.Actor, tier: run.tier, ceiling: level, limit: time.Until(run.deadline),
		title: run.def.Name + " → " + a.Name + " (" + workflow.StepLabel(s) + ")", prompt: prompt + "\n\n" + workflow.AccessNote(d.Access),
		wfRun: run.rec.ID, wfStep: s.ID})
	if t == nil {
		done("failed")
		return "", errors.New(cmp.Or(why, "chat của lần chạy đang bận"))
	}
	select {
	case out := <-ch:
		if msg, bad := strings.CutPrefix(out, "\x00"); bad {
			done("failed")
			return "", errors.New(msg)
		}
		done("done")
		return out, nil
	case <-ctx.Done():
		done("failed")
		return "", ctx.Err()
	}
}

// stepFlow runs a sub-workflow for a step and waits for it to end.
func (e *Engine) stepFlow(ctx context.Context, own storage.Conversation, project storage.Repo, run *wfRun, s workflow.Step, inputs map[string]string) (storage.WorkflowRun, error) {
	if run.depthLeft <= 0 {
		return storage.WorkflowRun{}, errors.New("đã tới giới hạn lồng quy trình con (limits.depth)")
	}
	w, err := e.store.Workflows().GetByKey(ctx, project.ID, s.Workflow)
	if err != nil {
		return storage.WorkflowRun{}, fmt.Errorf("project chưa cài quy trình /%s", s.Workflow)
	}
	def, err := workflow.Parse(w.Source)
	if err != nil {
		return storage.WorkflowRun{}, err
	}
	if def.Callable == workflow.CallableChat {
		return storage.WorkflowRun{}, fmt.Errorf("quy trình /%s chỉ chạy từ chat", def.Key)
	}
	input := ""
	if len(def.Inputs) > 0 {
		if input, err = def.RenderInputs(inputs); err != nil {
			return storage.WorkflowRun{}, err
		}
	} else {
		input = cmp.Or(inputs["text"], fmt.Sprint(inputs))
	}
	child, prompt, err := e.newRun(ctx, project.ID, w, def, run.coord, input)
	if err != nil {
		return storage.WorkflowRun{}, err
	}
	child.inputs = inputs
	child.depthLeft, child.until = min(run.depthLeft-1, def.Limits.Depth), run.deadline
	child.parent = run
	child.rec.CallerConversationID, child.rec.ParentRunID, child.rec.Depth = own.ID, run.rec.ID, run.rec.Depth+1
	sub, err := e.store.Chat().CreateConversation(ctx, storage.Conversation{ProjectID: project.ID, AgentID: run.coord.ID, AgentName: run.coord.Name, CreatedBy: run.rec.Actor,
		Purpose: RunPurpose, Title: truncate(def.Name+": "+oneLine(input), 80), Mode: own.Mode, EditMode: own.EditMode, Effort: own.Effort})
	if err != nil {
		return storage.WorkflowRun{}, err
	}
	sctx := actor.With(context.Background(), run.rec.Actor)
	if run.tier != "" {
		sctx = WithModelTier(sctx, run.tier)
	}
	if run.ceiling != "" {
		sctx = WithCeiling(sctx, run.ceiling)
	}
	if err := e.launch(sctx, sub, child, "/"+def.Key+" "+input, prompt, "", nil); err != nil {
		_ = e.store.Chat().DeleteConversation(ctx, sub.ID)
		return storage.WorkflowRun{}, err
	}
	e.wf.mu.Lock()
	run.logf("%s: chạy quy trình con /%s", workflow.StepLabel(s), def.Key)
	e.wf.mu.Unlock()
	select {
	case <-child.done:
	case <-ctx.Done():
		e.StopAll(sub.ID)
		return storage.WorkflowRun{}, ctx.Err()
	}
	rec, err := e.store.WorkflowRuns().Get(context.Background(), child.rec.ID)
	if err != nil {
		rec = child.rec
	}
	e.post(own, s, fmt.Sprintf("Quy trình con /%s: %s\n\n%s", rec.WorkflowKey, rec.Status, strings.TrimSpace(rec.Result)+outputsText(rec))) // i18n-ignore
	if rec.Status != storage.RunDone {
		return rec, fmt.Errorf("%w: quy trình con /%s %s %s", workflow.ErrStepFailed, rec.WorkflowKey, rec.Status, rec.Error)
	}
	return rec, nil
}

// stepDecide opens a card (an approval, a check command) and waits until
// it is decided; a command the project lets run goes at once.
func (e *Engine) stepDecide(ctx context.Context, own storage.Conversation, run *wfRun, s workflow.Step, kind, target, reason string) (workflow.StepResult, error) {
	if e.acts == nil {
		return workflow.StepResult{}, errors.New("office không mở thẻ duyệt được ở đây")
	}
	sc := actions.Scope{ProjectID: run.rec.ProjectID, ConversationID: own.ID, Agent: stepAuthor}
	if kind == "run_command" { // what the coordinator's agent may run without asking, in this chat's way
		policy := perm.LoadPolicy(ctx, e.store, run.rec.ProjectID)
		acc := perm.Resolve(run.coord, cmp.Or(run.ceiling, own.Mode), policy)
		sc.Level, sc.Access, sc.Agent = acc.Level, acc, run.coord.Name
	}
	a, err := e.acts.Propose(ctx, sc, kind, target, reason)
	if err != nil {
		return workflow.StepResult{}, err
	}
	for a.Status == "pending" {
		select {
		case <-ctx.Done():
			return workflow.StepResult{}, ctx.Err()
		case <-time.After(2 * time.Second):
		}
		if a, err = e.store.Actions().Get(ctx, a.ID); err != nil {
			return workflow.StepResult{}, err
		}
	}
	res := workflow.NewResult(a.Detail, "passed")
	if a.Status != "done" {
		res.Status = "failed"
		e.post(own, s, "Không qua: "+a.Status+"\n"+fence(a.Detail)) // i18n-ignore
		return res, fmt.Errorf("%w: %s", workflow.ErrStepFailed, a.Status)
	}
	if kind == "run_command" {
		e.post(own, s, "Đạt\n"+fence(a.Detail)) // i18n-ignore
	}
	return res, nil
}

// endSteps ends a graph of steps with its summary and outputs.
func (e *Engine) endSteps(own storage.Conversation, run *wfRun, s workflow.Step, vars workflow.Vars) {
	summary := strings.TrimSpace(workflow.Render(s.Summary, vars))
	if summary == "" { // the last step's output
		for i := len(run.def.Steps) - 1; i >= 0 && summary == ""; i-- {
			summary = strings.TrimSpace(vars.Steps[run.def.Steps[i].ID].Output)
		}
	}
	outputs := map[string]string{}
	for k, v := range s.Outputs {
		outputs[k] = strings.TrimSpace(workflow.Render(v, vars))
	}
	if err := run.def.CheckOutputs(outputs); err != nil {
		e.note(own, "⚠️ "+err.Error())
		e.finishRun(own.ID, run.rec.ID, storage.RunFailed, summary, err.Error())
		return
	}
	e.wf.mu.Lock()
	run.rec.Outputs = outputs
	e.wf.mu.Unlock()
	e.finishRun(own.ID, run.rec.ID, storage.RunDone, cmp.Or(summary, "Xong."), "")
}
