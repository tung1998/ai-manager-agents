// Tests for mergeDraft (ADR-042): run with node --experimental-strip-types.
import assert from 'node:assert/strict'
import { draftFrom, emptyDraft, mergeDraft } from '../app/composables/useAutomations.ts'

// nested fields: known keys of the right type only
{
  const d = emptyDraft()
  const changed = mergeDraft(d, { script: { body: null, language: 'bash', lang: 'ruby' }, limits: { max_runs_per_hour: '5' }, escalate: { when: 'sometimes' } })
  assert.equal(d.script.body, '', 'null body ignored')
  assert.equal(d.script.lang, 'bash', 'unknown language ignored')
  assert.equal('language' in d.script, false, 'unknown key ignored')
  assert.equal(d.limits.max_runs_per_hour, 0, 'string for a number ignored')
  assert.equal(d.escalate.when, 'failure', 'unknown enum ignored')
  assert.deepEqual(changed, [], 'nothing changed')
}
// a good patch applies
{
  const d = emptyDraft()
  const changed = mergeDraft(d, { name: 'Đếm lỗi', script: { lang: 'python', body: 'print(1)' }, escalate: { when: 'signal' } })
  assert.equal(d.name, 'Đếm lỗi')
  assert.equal(d.script.lang, 'python')
  assert.equal(d.escalate.when, 'signal')
  assert.deepEqual(changed.sort(), ['escalate', 'name', 'script'])
}
// cron and every_minutes replace each other
{
  const d = emptyDraft()
  mergeDraft(d, { config: { every_minutes: 15 } })
  assert.equal(d.config.every_minutes, 15)
  assert.equal(d.config.cron, '', 'every N minutes clears the cron')
  mergeDraft(d, { config: { cron: '0 9 * * *' } })
  assert.equal(d.config.cron, '0 9 * * *')
  assert.equal(d.config.every_minutes, 0, 'a cron clears every N minutes')
}
// "message an agent" is gone: tasks go to the team or to one agent
{
  const d = emptyDraft()
  mergeDraft(d, { action: 'chat', escalate: { action: 'chat' } })
  assert.equal(d.action, 'script', 'chat action refused')
  assert.equal(d.escalate.action, 'task', 'escalation defaults to a task')
  const old = { ...emptyDraft(), id: 'aut_1', project_id: 'p', action: 'chat', agent_id: 'agt_1', escalate: { when: 'failure', action: 'chat', agent_id: 'agt_2', prompt: '' } }
  const conv = draftFrom(old as never)
  assert.equal(conv.action, 'task', 'an old chat automation opens as a task')
  assert.equal(conv.agent_id, 'agt_1', 'for the same agent')
  assert.equal(conv.escalate.action, 'task', 'its escalation too')
  assert.equal(conv.escalate.agent_id, 'agt_2')
}
console.log('mergeDraft: ok')
