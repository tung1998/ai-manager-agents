// Workflows (ADR-098): how a project's agents work together, from a library
// of files; each project keeps its own copy with the agent of each role.

export type WorkflowAccess = 'analyze' | 'propose' | 'edit'

export interface WorkflowRole {
  key: string
  name: string
  hint?: string
  access: WorkflowAccess
  differ_from?: string[]
}

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
  body: string
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

export interface WorkflowRun {
  id: string
  project_id: string
  conversation_id: string
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
