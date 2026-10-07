// Workflows (ADR-098): how a project's agents work together, from a library
// of files; each project keeps its own copy with the agent of each role.

export type WorkflowAccess = 'analyze' | 'propose' | 'edit'

export interface WorkflowRole {
  key: string
  name: string
  hint?: string
  access: WorkflowAccess
  differ_from?: string[]
  workflow?: string // a sub-workflow fills it (ADR-102)
  prefer?: { tier?: string, family?: string } // the agent it wants (ADR-103)
}

// An input a caller gives a workflow, or an output it gives back (ADR-103).
export interface WorkflowField {
  key: string
  description?: string
  required: boolean
  type?: 'string' | 'number' | 'boolean' | 'list' | 'json'
}

export type WorkflowCallable = '' | 'chat' | 'sub'

export interface WorkflowGate {
  key: string
  name: string
  kind: 'approve' | 'check'
  command?: string
  required?: boolean
}

export interface WorkflowVote {
  roles?: string[]
  quorum: number
  veto?: string[]
}

export interface WorkflowLimits {
  rounds: number
  turns: number
  timeout: string
  budget_usd: number
  depth: number // how deep sub-workflows may go below a run
  concurrency: number // roles at once in a whole tree of runs
  idle?: string // a role working this long is noted
}

export interface WorkflowDef {
  key: string
  name: string
  description: string
  input?: string
  roles: WorkflowRole[]
  parallel?: string[][]
  limits: WorkflowLimits
  brief?: string[]
  gates?: WorkflowGate[]
  vote?: WorkflowVote | null
  strict?: boolean
  inputs?: WorkflowField[]
  outputs?: WorkflowField[]
  callable?: WorkflowCallable
  steps?: WorkflowStep[] // a graph the office runs in order (ADR-108); none: a coordinator decides
  body: string
}

// Steps (ADR-108): one node of a workflow's graph. Data goes from step to
// step by templates: {{input.key}}, {{steps.<id>.output}},
// {{steps.<id>.json.a.b}}, {{steps.<id>.status}}.
export type StepType = 'agent' | 'workflow' | 'code' | 'http' | 'condition' | 'approve' | 'check' | 'end'

export interface WorkflowStep {
  id: string
  type: StepType
  name?: string
  next?: string
  on_error?: '' | 'stop' | 'continue'
  role?: string // agent
  prompt?: string
  workflow?: string // workflow: a sub-workflow's key and its inputs
  inputs?: Record<string, string>
  lang?: 'bash' | 'node' | 'python' | '' // code
  script?: string
  timeout_s?: number
  method?: string // http
  url?: string
  headers?: Record<string, string>
  body?: string
  if?: string // condition
  then?: string
  else?: string // condition; approve/check: when it does not pass
  max_loops?: number
  note?: string // approve
  command?: string // check
  summary?: string // end
  outputs?: Record<string, string>
  position?: { x: number, y: number } // on the editor's canvas
}

// the step types, in the order the editor offers them
export const STEP_TYPES: { type: StepType, icon: string }[] = [
  { type: 'agent', icon: 'i-lucide-bot' },
  { type: 'workflow', icon: 'i-lucide-workflow' },
  { type: 'code', icon: 'i-lucide-code' },
  { type: 'http', icon: 'i-lucide-globe' },
  { type: 'condition', icon: 'i-lucide-split' },
  { type: 'approve', icon: 'i-lucide-user-check' },
  { type: 'check', icon: 'i-lucide-test-tube' },
  { type: 'end', icon: 'i-lucide-flag' }
]
export const stepIcon = (type: string) => STEP_TYPES.find(s => s.type === type)?.icon ?? 'i-lucide-square'

const STEP_KEY = /^[a-z0-9][a-z0-9-]{0,39}$/
export const validStepKey = (k: string) => STEP_KEY.test(k)

// what a step lacks (internal/workflow validateSteps): the fields to mark
export function stepMissing(s: WorkflowStep, def: WorkflowDef): string[] {
  const ids = new Set((def.steps ?? []).map(x => x.id))
  const bad: string[] = []
  const ref = (field: 'next' | 'then' | 'else', need: boolean) => {
    const to = s[field]
    if ((!to && need) || (to && !ids.has(to))) bad.push(field)
  }
  if (!validStepKey(s.id) || (def.steps ?? []).filter(x => x.id === s.id).length > 1) bad.push('id')
  switch (s.type) {
    case 'agent':
      if (!def.roles.some(r => r.key === s.role)) bad.push('role')
      if (!s.prompt?.trim()) bad.push('prompt')
      break
    case 'workflow':
      if (!validStepKey(s.workflow ?? '')) bad.push('workflow')
      break
    case 'code':
      if (!['bash', 'node', 'python'].includes(s.lang ?? '')) bad.push('lang')
      if (!s.script?.trim()) bad.push('script')
      break
    case 'http': {
      const u = s.url ?? ''
      if (!u.startsWith('http://') && !u.startsWith('https://') && !u.startsWith('{{')) bad.push('url')
      break
    }
    case 'condition':
      if (!s.if?.trim()) bad.push('if')
      ref('then', true)
      ref('else', true)
      break
    case 'approve':
      if (!s.note?.trim()) bad.push('note')
      ref('else', false)
      break
    case 'check':
      if (!s.command?.trim()) bad.push('command')
      ref('else', false)
      break
  }
  if (s.type !== 'end' && s.type !== 'condition') ref('next', true)
  return bad
}

// a step renamed: every link to it and every {{steps.old.…}} follows
export function renameStep(steps: WorkflowStep[], from: string, to: string): WorkflowStep[] {
  const re = new RegExp(`\\{\\{(\\s*)steps\\.${from.replace(/[-]/g, '\\-')}\\.`, 'g')
  const tpl = (v: string) => v.replace(re, `{{$1steps.${to}.`)
  const map = (m?: Record<string, string>) => m && Object.fromEntries(Object.entries(m).map(([k, v]) => [k, tpl(v)]))
  return steps.map((s) => {
    const n: WorkflowStep = { ...s }
    if (n.id === from) n.id = to
    for (const f of ['next', 'then', 'else'] as const) if (n[f] === from) n[f] = to
    for (const f of ['prompt', 'script', 'url', 'body', 'if', 'note', 'command', 'summary'] as const) {
      if (n[f]) n[f] = tpl(n[f]!)
    }
    n.inputs = map(n.inputs)
    n.headers = map(n.headers)
    n.outputs = map(n.outputs)
    return n
  })
}

// A library workflow (GET /api/workflow-library).
export interface LibraryWorkflow {
  def: WorkflowDef
  source?: string
  builtin: boolean // shipped with the office
  modified: boolean // a shipped one, changed here
  error?: string
  updated_at: string
}

export interface RunRole {
  role: string
  name: string
  access: WorkflowAccess
  agent_id: string
  agent_name: string
  status: 'idle' | 'working' | 'done' | 'failed'
  rounds: number
  turns: number
  result?: string
  cost_usd: number
  workflow?: string // a sub-workflow fills it
  run_id?: string // its latest run
}

export interface RunGate {
  key: string
  name: string
  kind: 'approve' | 'check'
  required: boolean
  status: 'open' | 'waiting' | 'passed' | 'failed'
  action_id?: string
  detail?: string
}

// A run: it works in a chat of its own (conversation_id); the chat that
// called it (caller_conversation_id) shows only its input and output.
export interface WorkflowRun {
  id: string
  project_id: string
  conversation_id: string
  caller_conversation_id: string // '' = a run from before, held in conversation_id itself
  caller_title: string
  parent_run_id: string // the run whose role called it ('' = a chat did)
  depth: number
  workflow_id: string
  workflow_key: string
  workflow_name: string
  coordinator_id: string
  coordinator_name: string
  input: string
  status: 'running' | 'done' | 'failed' | 'stopped'
  turns: number
  max_turns: number
  cost_usd: number
  result: string
  error: string
  roles: RunRole[]
  gates: RunGate[]
  log: { at: string, text: string }[]
  outputs?: Record<string, string> // its declared outputs, by key
  started_at: string
  finished_at: string | null
}

// A project's workflow (GET /api/projects/{id}/workflows).
export interface ProjectWorkflow {
  id: string
  project_id: string
  key: string
  name: string
  description: string
  input: string
  enabled: boolean
  source: string
  bindings: Record<string, string> // role → agent id
  roles: WorkflowRole[]
  parallel: string[][]
  gates: WorkflowGate[]
  vote: WorkflowVote | null
  limits: WorkflowLimits
  brief: string[]
  inputs: WorkflowField[]
  outputs: WorkflowField[]
  callable: WorkflowCallable
  error?: string
  source_key: string // the library workflow it was copied from
  has_update: boolean // that one changed since
  updated_at: string
  last_run: WorkflowRun | null
}

export const accessIcon: Record<WorkflowAccess, string> = {
  analyze: 'i-lucide-eye',
  propose: 'i-lucide-message-square-diff',
  edit: 'i-lucide-pencil'
}

// what a fenced ```workflow block of an agent's answer holds (the whole file)
export function workflowBlock(text: string): string | null {
  const m = text.match(/```workflow\n([\s\S]*?)\n```/)
  return m ? m[1]! : null
}
