// Automations and jobs as the dashboard sees them (ADR-040).
export type JobStatus = 'pending' | 'running' | 'done' | 'failed' | 'cancelled' | 'skipped' | 'needs_input'

export interface Job {
  id: string
  project_id: string
  project_name: string
  kind: 'chat_turn' | 'task' | 'script'
  origin: 'user' | 'automation' | 'monitor' | 'retry'
  origin_id: string
  trigger: string
  created_by: string
  conversation_id: string
  message_id: string
  task_id: string
  status: JobStatus
  error: string
  error_code: string
  agent_id: string
  agent_name: string
  automation_name: string
  title: string
  cost_usd: number
  input_tokens: number
  output_tokens: number
  duration_ms: number
  created_at: string
  started_at: string | null
  finished_at: string | null
  exit_code: number | null
  parent_job_id: string
  output?: string
  payload?: string
}

export interface AutomationConfig { every_minutes?: number, cron?: string, timezone?: string, auth?: string, auth_name?: string }
export interface AutomationLimits {
  max_runs_per_hour?: number
  daily_cost_usd?: number
  disable_after_failures?: number
  debounce_seconds?: number
  debounce_key?: string
  debounce_max_seconds?: number
}
export interface AutomationScript { lang: 'bash' | 'node' | 'python', body: string, timeout_s?: number }
export interface AutomationEscalate { when: 'never' | 'failure' | 'signal', action: 'chat' | 'task', agent_id: string, prompt: string }
export interface Automation {
  id: string
  project_id: string
  name: string
  enabled: boolean
  source: 'schedule' | 'webhook'
  config: AutomationConfig
  action: 'chat' | 'task' | 'script'
  agent_id: string
  prompt: string
  edit_mode: 'worktree' | 'direct'
  keep_context: boolean
  limits: AutomationLimits
  script: AutomationScript
  escalate: AutomationEscalate
  failures: number
  disabled_code: string
  disabled_reason: string
  last_run_at: string | null
  next_run_at: string | null
  webhook_url?: string
  last_job: Job | null
  created_at: string
}

// automationBody is what PATCH/POST take (the fields a person edits).
export function automationBody(a: Pick<Automation, 'name' | 'enabled' | 'source' | 'config' | 'action' | 'agent_id' | 'prompt' | 'edit_mode' | 'keep_context' | 'limits' | 'script' | 'escalate'>) {
  return {
    name: a.name, enabled: a.enabled, source: a.source, action: a.action, agent_id: a.agent_id, prompt: a.prompt,
    edit_mode: a.edit_mode, keep_context: a.keep_context, config: a.config, limits: a.limits, script: a.script, escalate: a.escalate
  }
}

// scheduleText describes when an automation runs.
export function scheduleText(a: Pick<Automation, 'source' | 'config' | 'webhook_url'>, t: (k: 'auto.every' | 'auto.sourceWebhook', p?: Record<string, string | number>) => string) {
  if (a.source === 'webhook') return a.webhook_url ?? t('auto.sourceWebhook')
  if (a.config.cron) return `${a.config.cron}${a.config.timezone ? ` (${a.config.timezone})` : ''}`
  return t('auto.every', { n: a.config.every_minutes ?? 0 })
}

export const jobStatusMeta: Record<JobStatus, { color: 'info' | 'neutral' | 'success' | 'error' | 'warning', icon: string }> = {
  pending: { color: 'neutral', icon: 'i-lucide-clock' },
  running: { color: 'info', icon: 'i-lucide-loader' },
  done: { color: 'success', icon: 'i-lucide-circle-check' },
  failed: { color: 'error', icon: 'i-lucide-circle-x' },
  cancelled: { color: 'neutral', icon: 'i-lucide-circle-slash' },
  skipped: { color: 'neutral', icon: 'i-lucide-skip-forward' },
  needs_input: { color: 'warning', icon: 'i-lucide-message-circle-question' }
}

// ---- the draft the builder page edits (ADR-042) ----
export type AutomationDraft = Omit<Automation, 'id' | 'project_id' | 'failures' | 'disabled_code' | 'disabled_reason' | 'last_run_at' | 'next_run_at' | 'last_job' | 'created_at' | 'webhook_url'>

export function emptyDraft(): AutomationDraft {
  return {
    name: '', enabled: true, source: 'schedule',
    config: { every_minutes: 0, cron: '0 8 * * 1-5', timezone: Intl.DateTimeFormat().resolvedOptions().timeZone, auth: 'bearer', auth_name: '' },
    action: 'script', agent_id: '', prompt: '', edit_mode: 'worktree', keep_context: false,
    script: { lang: 'bash', body: '', timeout_s: 300 },
    escalate: { when: 'failure', action: 'chat', agent_id: '', prompt: '' },
    limits: { max_runs_per_hour: 0, daily_cost_usd: 0, disable_after_failures: 5, debounce_seconds: 0, debounce_key: '', debounce_max_seconds: 0 }
  }
}

export function draftFrom(a: Automation): AutomationDraft {
  const e = emptyDraft()
  const b = JSON.parse(JSON.stringify(automationBody(a))) as AutomationDraft
  return {
    ...e, ...b, config: { ...e.config, ...b.config }, limits: { ...e.limits, ...b.limits },
    script: { ...e.script, ...(b.script?.lang ? b.script : {}) }, escalate: { ...e.escalate, ...(b.escalate?.when ? b.escalate : {}) }
  }
}

const draftObjects = ['config', 'limits', 'script', 'escalate'] as const
const draftScalars = ['name', 'enabled', 'source', 'action', 'agent_id', 'prompt', 'edit_mode', 'keep_context'] as const
const allowed: Record<string, readonly string[]> = {
  source: ['schedule', 'webhook'], action: ['script', 'chat', 'task'], edit_mode: ['worktree', 'direct']
}

// mergeDraft applies an agent's partial draft: known fields of the right kind
// only (anything else is ignored). It returns the top-level keys it changed.
export function mergeDraft(d: AutomationDraft, patch: Record<string, unknown>): string[] {
  const changed: string[] = []
  for (const k of draftScalars) {
    const v = patch[k]
    if (v === undefined || typeof v !== typeof d[k]) continue
    if (allowed[k] && !allowed[k]!.includes(v as string)) continue
    ;(d as Record<string, unknown>)[k] = v
    changed.push(k)
  }
  for (const k of draftObjects) {
    const v = patch[k]
    if (!v || typeof v !== 'object' || Array.isArray(v)) continue
    const target = d[k] as Record<string, unknown>
    let any = false
    for (const [f, x] of Object.entries(v as Record<string, unknown>)) {
      if (x !== null && typeof x === 'object') continue
      target[f] = x
      any = true
    }
    if (any) changed.push(k)
  }
  return changed
}
