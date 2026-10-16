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

export interface AutomationConfig {
  every_minutes?: number, cron?: string, timezone?: string, auth?: string, auth_name?: string
  // telegram | discord (ADR-049): the channel, which messages (a keyword; none = any), within a scope
  channel_id?: string, keywords?: string[], scope?: string
  // …or a custom slash command of the bot, and the text typed after it ("" = none)
  command?: string, command_description?: string, command_arg?: string
  skill?: string // the command calls this project skill
  workflow?: string // action workflow: the workflow it runs, the prompt being its input (ADR-109)
  reply_mode?: '' | 'answer' | 'steps' // what the bot shows of a run ('' = the bot's own)
  own_setup?: boolean // a bot's command set up on its own; false = the bot's default setup
  pull_request?: boolean // a GitHub/Bitbucket PR webhook: runs on a PR opened or updated, with {{diff}}
  notify_channel_id?: string, notify_chat_id?: string // what a run answers goes to this bot's chat too
  tags?: string[] // put on the chat of each run
  ends_at?: string | null // a schedule turns itself off then (as a Burn); none = runs until turned off
  goal?: AutomationGoal | null // chat/workflow: runs until it is reached (ADR-132)
}
// AutomationGoal: after each turn the check (exit 0) and/or the judge; not reached, the chat goes on
export interface AutomationGoal { text?: string, check?: string, judge?: boolean, max_rounds?: number }
export interface AutomationLimits {
  max_parallel?: number // runs at once (ADR-082)
  max_minutes?: number // how long one run may take (0 = no limit)
  max_runs_per_hour?: number
  daily_cost_usd?: number
  disable_after_failures?: number
  debounce_seconds?: number
  debounce_key?: string
  debounce_max_seconds?: number
}
export interface AutomationScript { lang: 'bash' | 'node' | 'python', body: string, timeout_s?: number, burn_start?: boolean } // burn_start: its @@quest lines start a stopped Burn (ADR-132)
export interface AutomationEscalate { when: 'never' | 'failure' | 'signal', action: 'chat' | 'task', agent_id: string, prompt: string } // kept for old ones: never runs (ADR-057)
export interface Automation {
  id: string
  project_id: string
  name: string
  enabled: boolean
  source: 'schedule' | 'webhook' | 'telegram' | 'discord'
  config: AutomationConfig
  action: 'chat' | 'script' | 'workflow'
  agent_id: string
  prompt: string
  edit_mode: 'worktree' | 'direct'
  model_tier?: '' | 'strong' | 'balanced' | 'fast' // '' = the agent's own
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
  // telegram | discord: the bot it listens to (ADR-049), and how that bot is doing
  bot?: { has_token: boolean, allow: string[], refusal: string }
  bot_status?: { kind: 'telegram' | 'discord', bot_name: string, enabled: boolean, last_error: string, last_message_at: string | null, shared: number, state?: BotState }
  last_job: Job | null
  created_at: string
  version?: string // what an edit is made from (409 when changed since)
  // ADR-074: permission override
  permission_mode?: 'agent' | 'override'
  override_full_access?: boolean
  override_admin_by?: string // who turned full access on (the server stamps it)
  override_extra_dirs?: string[]
}

// A bot's settings as a form edits them (the token only when a new one is pasted).
export interface BotDraft { token?: string, allow: string[], refusal: string, approvers?: string[], approval?: 'ask' | 'direct' | 'admin', header?: string, reply_mode?: '' | 'steps', defaults?: CommandSetup, version?: string }

// What a bot's command does (not when it runs): the bot keeps one as its
// commands' default; a command with own_setup has its own.
export interface CommandSetup {
  action: Automation['action'], agent_id: string, prompt: string, script: AutomationScript, tags: string[], limits: AutomationLimits
  permission_mode?: Automation['permission_mode'], override_full_access?: boolean, override_extra_dirs?: string[]
}
export function setupOf(d: AutomationDraft): CommandSetup {
  return JSON.parse(JSON.stringify({
    action: d.action, agent_id: d.agent_id, prompt: d.prompt, script: d.script, tags: d.config.tags ?? [], limits: d.limits,
    permission_mode: d.permission_mode ?? 'agent', override_full_access: d.override_full_access ?? false, override_extra_dirs: d.override_extra_dirs ?? []
  }))
}
// applySetup puts a setup into a command's draft (a copy: the two never share)
export function applySetup(d: AutomationDraft, s: CommandSetup) {
  const c = JSON.parse(JSON.stringify(s)) as CommandSetup
  d.action = c.action
  d.agent_id = c.agent_id
  d.prompt = c.prompt
  d.script = { ...d.script, ...c.script }
  d.config.tags = c.tags ?? []
  d.limits = { ...d.limits, ...c.limits }
  d.permission_mode = c.permission_mode ?? 'agent'
  d.override_full_access = c.override_full_access ?? false
  d.override_extra_dirs = c.override_extra_dirs ?? []
}
export const sameSetup = (a: CommandSetup, b: CommandSetup) => JSON.stringify(a) === JSON.stringify(b)

// AutomationBody is what PATCH/POST take.
export interface AutomationBody {
  name: string, enabled: boolean, source: Automation['source'], action: Automation['action'], agent_id: string, prompt: string,
  edit_mode: Automation['edit_mode'], model_tier: NonNullable<Automation['model_tier']>, keep_context: boolean,
  config: AutomationConfig, limits: AutomationLimits, script: AutomationScript, escalate: AutomationEscalate, bot?: BotDraft, version?: string
  // ADR-074: permission override
  permission_mode?: 'agent' | 'override'
  override_full_access?: boolean
  override_extra_dirs?: string[]
}

export function automationBody(a: Pick<Automation, 'name' | 'enabled' | 'source' | 'config' | 'action' | 'agent_id' | 'prompt' | 'edit_mode' | 'model_tier' | 'keep_context' | 'limits' | 'script' | 'escalate' | 'permission_mode' | 'override_full_access' | 'override_extra_dirs'> & { bot?: BotDraft }): AutomationBody {
  const body: AutomationBody = {
    name: a.name, enabled: a.enabled, source: a.source, action: a.action, agent_id: a.agent_id, prompt: a.prompt,
    edit_mode: a.edit_mode, model_tier: '', keep_context: a.keep_context, config: a.config, limits: a.limits, script: a.script, escalate: a.escalate,
    permission_mode: a.permission_mode, override_full_access: a.override_full_access, override_extra_dirs: a.override_extra_dirs
  }
  if (isChannelSource(a.source) && a.bot) body.bot = { allow: a.bot.allow, refusal: a.bot.refusal, ...(a.bot.token ? { token: a.bot.token } : {}) }
  return body
}

// scheduleText describes when an automation runs.
export function scheduleText(a: Pick<Automation, 'source' | 'config' | 'webhook_url' | 'bot_status'>, t: (k: 'auto.every' | 'auto.sourceWebhook' | 'auto.sourceChannel', p?: Record<string, string | number>) => string) {
  if (a.source === 'webhook') return a.webhook_url ?? t('auto.sourceWebhook')
  if (isChannelSource(a.source)) {
    const bot = a.bot_status?.bot_name ? `@${a.bot_status.bot_name}` : t('auto.sourceChannel')
    const which = a.config.command ? ` · /${a.config.command}` : a.config.keywords?.length ? ` · "${a.config.keywords.join('", "')}"` : ''
    return `${bot} · ${a.source === 'discord' ? 'Discord' : 'Telegram'}${which}`
  }
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

// isChannelSource: messages of a Telegram/Discord channel start it (ADR-049).
export const isChannelSource = (s: string) => s === 'telegram' || s === 'discord'

// ---- the draft the builder page edits (ADR-042) ----
export type AutomationDraft = Omit<Automation, 'id' | 'project_id' | 'failures' | 'disabled_code' | 'disabled_reason' | 'last_run_at' | 'next_run_at' | 'last_job' | 'created_at' | 'webhook_url' | 'bot' | 'bot_status'> & { bot: BotDraft }

// prReviewDraft: a GitHub/Bitbucket PR webhook, the agent reviews the diff and the chat hears it
export function prReviewDraft(t: (k: 'auto.prName' | 'auto.prPrompt') => string): AutomationDraft {
  const d = emptyDraft()
  d.name = t('auto.prName')
  d.source = 'webhook'
  d.action = 'chat'
  d.config.pull_request = true
  d.config.auth = 'query'
  d.config.auth_name = 'token'
  d.prompt = t('auto.prPrompt')
  d.limits.max_runs_per_hour = 20
  return d
}

// AutomationTemplate is the starter library shown when a person creates an
// automation. Templates deliberately produce a draft, rather than an API
// object: the person can review and change every trigger, prompt and limit
// before saving it.
export interface AutomationTemplate {
  key: string
  name: string
  description: string
  category: string
  icon: string
  make: () => AutomationDraft
}

type TemplatePatch = Partial<Pick<AutomationDraft, 'name' | 'source' | 'action' | 'prompt' | 'keep_context'>> & {
  config?: Partial<AutomationConfig>
  script?: Partial<AutomationScript>
  limits?: Partial<AutomationLimits>
}

function templateDraft(patch: TemplatePatch): AutomationDraft {
  const d = emptyDraft()
  Object.assign(d, patch)
  d.config = { ...d.config, ...(patch.config ?? {}) }
  d.script = { ...d.script, ...(patch.script ?? {}) }
  d.limits = { ...d.limits, ...(patch.limits ?? {}) }
  return d
}

const chatSchedule = (name: string, prompt: string, cron: string, config: Partial<AutomationConfig> = {}, limits: Partial<AutomationLimits> = {}) =>
  templateDraft({ name, source: 'schedule', action: 'chat', prompt, config: { cron, every_minutes: 0, ...config }, limits })
// limits: how a webhook's burst of events is grouped (debounce_*) and capped
const chatWebhook = (name: string, prompt: string, limits: Partial<AutomationLimits> = {}) =>
  templateDraft({ name, source: 'webhook', action: 'chat', prompt, config: { auth: 'bearer' }, limits })
const scriptSchedule = (name: string, body: string, cron: string, lang: AutomationScript['lang'] = 'bash') =>
  templateDraft({ name, source: 'schedule', action: 'script', script: { lang, body }, config: { cron, every_minutes: 0 } })

// Keep this list intentionally broad: it is a catalog of useful starting
// points, not a claim that every project uses the same tools or schedule.
export const automationTemplates: AutomationTemplate[] = [
  { key: 'pr-review', name: 'Review Pull Request', description: 'Review GitHub/Bitbucket changes for bugs, security and tests.', category: 'Engineering', icon: 'i-lucide-git-pull-request', make: () => prReviewDraft(k => k === 'auto.prName' ? 'Review Pull Request' : 'Review Pull Request #{{payload.number}} "{{payload.title}}" by {{payload.author}} ({{payload.source}} → {{payload.target}}), {{payload.url}}.\nRead the diff and related files. Report bugs, security risks, missing tests and maintainability issues with file:line, severity and a fix. Start with a one-sentence verdict.\n\n{{diff}}') },
  { key: 'release-notes', name: 'Release notes', description: 'Turn a release payload into concise customer-facing notes.', category: 'Engineering', icon: 'i-lucide-notebook-pen', make: () => chatWebhook('Release notes', 'Create release notes from {{payload}}. Group changes by feature, fix and breaking change. Write a short customer-friendly summary and a technical checklist.', { debounce_key: 'release.tag', debounce_seconds: 30, debounce_max_seconds: 300 }) },
  { key: 'incident-triage', name: 'Incident triage', description: 'Classify an alert and suggest the first safe response.', category: 'Engineering', icon: 'i-lucide-siren', make: () => chatWebhook('Incident triage', 'Triage this alert: {{payload}}. Classify severity, identify likely impact, list safe first checks, and name the owner to page. Do not claim a fix without evidence.', { debounce_key: 'alert_id', debounce_seconds: 15, debounce_max_seconds: 120, max_runs_per_hour: 30 }) },
  { key: 'dependency-watch', name: 'Dependency watch', description: 'Review dependency updates for risk before the workday starts.', category: 'Engineering', icon: 'i-lucide-package-search', make: () => chatSchedule('Dependency watch', 'Review the dependency update report in {{payload}}. Highlight security advisories, breaking changes, abandoned packages and recommended next steps.', '0 8 * * 1-5') },
  { key: 'uptime-check', name: 'Uptime check', description: 'Run a lightweight endpoint check without AI cost.', category: 'Engineering', icon: 'i-lucide-activity', make: () => scriptSchedule('Uptime check', 'curl -fsS --max-time 15 "${OFFICE_HEALTH_URL:-http://localhost:3000/health}" >/dev/null || { echo "health check failed"; exit 1; }', '*/5 * * * *') },
  { key: 'security-digest', name: 'Security digest', description: 'Summarize security findings for the team each morning.', category: 'Security', icon: 'i-lucide-shield-check', make: () => chatSchedule('Security digest', 'Summarize the security findings in {{payload}}. Sort by severity, affected asset, exploitability and owner. Call out items that need human verification.', '0 9 * * 1-5') },
  { key: 'access-review', name: 'Access review', description: 'Find stale or excessive access in a weekly export.', category: 'Security', icon: 'i-lucide-key-round', make: () => chatSchedule('Access review', 'Review this access export: {{payload}}. Identify inactive accounts, excessive privileges, shared accounts and missing owners. Return an actionable review list.', '0 10 * * 1') },
  { key: 'backup-report', name: 'Backup report', description: 'Check backup results and surface recovery risks.', category: 'Operations', icon: 'i-lucide-database-backup', make: () => chatSchedule('Backup report', 'Review backup results from {{payload}}. Report failures, age of the newest successful backup, coverage gaps and the next recovery test to run.', '0 7 * * *') },
  { key: 'cost-anomaly', name: 'Cloud cost anomaly', description: 'Explain unusual cloud spend and suggest owners.', category: 'Finance & Ops', icon: 'i-lucide-wallet-cards', make: () => chatSchedule('Cloud cost anomaly', 'Analyze this cloud cost snapshot: {{payload}}. Compare with the baseline, identify anomalies and likely drivers, then suggest safe savings actions with an owner.', '0 9 * * 1-5') },
  { key: 'daily-sales', name: 'Daily sales brief', description: 'Give sales a compact pipeline and revenue brief.', category: 'Sales', icon: 'i-lucide-chart-no-axes-combined', make: () => chatSchedule('Daily sales brief', 'Create a daily sales brief from {{payload}}: wins, losses, pipeline movement, stalled deals, forecast risks and three suggested follow-ups. Keep it concise.', '0 8 * * 1-5') },
  { key: 'lead-qualification', name: 'Lead qualification', description: 'Score and route an inbound lead from a webhook.', category: 'Sales', icon: 'i-lucide-user-round-check', make: () => chatWebhook('Lead qualification', 'Qualify this inbound lead: {{payload}}. Extract company, need, urgency, budget signals and next step. Give a score with reasons and flag missing information.', { debounce_key: 'lead.email', debounce_seconds: 20, debounce_max_seconds: 180 }) },
  { key: 'support-summary', name: 'Support queue summary', description: 'Summarize support volume, risks and repeated issues.', category: 'Customer', icon: 'i-lucide-headphones', make: () => chatSchedule('Support queue summary', 'Summarize the support queue in {{payload}}. Include volume, SLA risks, top themes, frustrated customers and a suggested escalation list.', '0 9 * * 1-5') },
  { key: 'review-reply', name: 'Customer review reply', description: 'Draft empathetic responses to new customer reviews.', category: 'Customer', icon: 'i-lucide-star', make: () => chatWebhook('Customer review reply', 'Draft a helpful, human response to this customer review: {{payload}}. Acknowledge specifics, avoid exposing private data, and suggest a private follow-up for unresolved issues.', { debounce_key: 'review.id', debounce_seconds: 60, debounce_max_seconds: 600 }) },
  { key: 'churn-risk', name: 'Customer churn watch', description: 'Spot accounts needing proactive attention.', category: 'Customer', icon: 'i-lucide-user-minus', make: () => chatSchedule('Customer churn watch', 'Analyze this customer health export: {{payload}}. Flag churn signals, explain the evidence, rank accounts by urgency and suggest a respectful intervention.', '0 8 * * 1') },
  { key: 'content-calendar', name: 'Content calendar', description: 'Turn topics and campaigns into a weekly publishing plan.', category: 'Marketing', icon: 'i-lucide-calendar-days', make: () => chatSchedule('Content calendar', 'Build a weekly content calendar from {{payload}}. Map each idea to audience, channel, format, hook, CTA, owner and a realistic publishing date.', '0 10 * * 1') },
  { key: 'campaign-report', name: 'Campaign report', description: 'Explain campaign performance and recommend experiments.', category: 'Marketing', icon: 'i-lucide-megaphone', make: () => chatSchedule('Campaign report', 'Analyze campaign metrics in {{payload}}. Compare against targets, explain meaningful changes, separate signal from noise and propose the next experiment.', '0 9 * * 1') },
  { key: 'seo-opportunities', name: 'SEO opportunity scan', description: 'Find practical SEO opportunities from analytics data.', category: 'Marketing', icon: 'i-lucide-search-check', make: () => chatSchedule('SEO opportunity scan', 'Review this search performance export: {{payload}}. Find pages with high potential, declining queries, content gaps and internal-link opportunities. Rank the work by impact.', '0 8 * * 1') },
  { key: 'invoice-check', name: 'Invoice check', description: 'Flag anomalies before invoices are approved.', category: 'Finance & Ops', icon: 'i-lucide-receipt-text', make: () => chatWebhook('Invoice check', 'Check this invoice payload: {{payload}}. Compare totals, tax, vendor, purchase order and duplicate signals. Flag anomalies and state what must be verified by a person.', { debounce_key: 'invoice.id', debounce_seconds: 30, debounce_max_seconds: 300 }) },
  { key: 'cashflow-brief', name: 'Cash-flow brief', description: 'Prepare a plain-language cash-flow snapshot.', category: 'Finance & Ops', icon: 'i-lucide-banknote', make: () => chatSchedule('Cash-flow brief', 'Prepare a cash-flow brief from {{payload}}. Show expected inflows, outflows, runway risks, assumptions and the questions finance should answer next.', '0 8 * * 1') },
  { key: 'meeting-followup', name: 'Meeting follow-up', description: 'Convert meeting notes into owners and next actions.', category: 'Productivity', icon: 'i-lucide-list-checks', make: () => chatWebhook('Meeting follow-up', 'Turn these meeting notes into a clear follow-up: {{payload}}. Separate decisions, open questions, action items with owners and dates, and risks.', { debounce_key: 'meeting.id', debounce_seconds: 30, debounce_max_seconds: 300 }) },
  { key: 'project-status', name: 'Project status brief', description: 'Create a weekly status update from project data.', category: 'Productivity', icon: 'i-lucide-kanban', make: () => chatSchedule('Project status brief', 'Write a weekly project status from {{payload}}: progress, completed work, next milestones, blockers, scope changes and decisions needed. Be factual and label uncertainty.', '0 16 * * 5') },
  { key: 'hr-onboarding', name: 'New-hire onboarding', description: 'Turn an HR event into a safe onboarding checklist.', category: 'People', icon: 'i-lucide-user-plus', make: () => chatWebhook('New-hire onboarding', 'Create an onboarding checklist from {{payload}}. Include role-specific first-week actions, equipment and access requests, introductions and a 30-day milestone. Never repeat sensitive personal data.', { debounce_key: 'employee.id', debounce_seconds: 60, debounce_max_seconds: 600 }) },
  { key: 'policy-reminder', name: 'Policy reminder', description: 'Send a weekly digest of policies needing acknowledgement.', category: 'People & Compliance', icon: 'i-lucide-file-check-2', make: () => chatSchedule('Policy reminder', 'Review policy acknowledgements in {{payload}}. List overdue items by owner, explain the deadline and draft a neutral reminder message.', '0 9 * * 1') },
  { key: 'inventory-alert', name: 'Inventory alert', description: 'Identify stock-out and overstock risks.', category: 'Commerce', icon: 'i-lucide-boxes', make: () => chatWebhook('Inventory alert', 'Analyze this inventory event: {{payload}}. Flag stock-out risk, overstock, lead-time issues and suggested reorder or promotion actions. Include confidence and assumptions.', { debounce_key: 'sku', debounce_seconds: 60, debounce_max_seconds: 900 }) },
  { key: 'logistics-exception', name: 'Delivery exception', description: 'Triage late or failed deliveries for operations.', category: 'Commerce', icon: 'i-lucide-truck', make: () => chatWebhook('Delivery exception', 'Triage this delivery exception: {{payload}}. Explain likely cause, customer impact, immediate action and an honest customer update. Do not promise a date without evidence.', { debounce_key: 'shipment.id', debounce_seconds: 30, debounce_max_seconds: 300 }) },
  { key: 'research-digest', name: 'Research digest', description: 'Turn a daily source feed into decisions and watch items.', category: 'Research', icon: 'i-lucide-newspaper', make: () => chatSchedule('Research digest', 'Summarize the source feed in {{payload}}. Separate facts, claims and speculation; group by theme; note source dates; end with implications and watch items.', '0 7 * * 1-5') },
  { key: 'it-ticket-triage', name: 'IT ticket triage', description: 'Classify, prioritize and route incoming IT requests.', category: 'IT Service', icon: 'i-lucide-ticket-check', make: () => chatWebhook('IT ticket triage', 'Triage this IT request: {{payload}}. Classify the service, urgency, impact, likely resolver group and missing information. Suggest a safe first response; do not close the ticket without human confirmation.', { debounce_key: 'ticket.id', debounce_seconds: 20, debounce_max_seconds: 180 }) },
  { key: 'fraud-signal-review', name: 'Fraud signal review', description: 'Investigate suspicious transactions with a human-review handoff.', category: 'Finance & Risk', icon: 'i-lucide-badge-alert', make: () => chatWebhook('Fraud signal review', 'Review this transaction signal: {{payload}}. Summarize evidence, compare plausible explanations, assign a risk level and recommend the next verification step. Never approve, decline or freeze an account automatically.', { debounce_key: 'transaction.id', debounce_seconds: 15, debounce_max_seconds: 120 }) },
  { key: 'data-quality-watch', name: 'Data quality watch', description: 'Detect freshness, completeness and schema problems in data feeds.', category: 'Data', icon: 'i-lucide-table-properties', make: () => chatSchedule('Data quality watch', 'Review this data-quality report: {{payload}}. Group failures by dataset and owner, distinguish freshness/completeness/schema issues, and rank the fixes by downstream impact.', '0 6 * * 1-5') },
  { key: 'knowledge-refresh', name: 'Knowledge base refresh', description: 'Find stale documentation and propose precise updates.', category: 'Knowledge', icon: 'i-lucide-book-open-check', make: () => chatSchedule('Knowledge base refresh', 'Review this knowledge-base usage and freshness report: {{payload}}. Find stale, duplicated or unanswered content, propose update owners and draft the highest-value changes with cited sources.', '0 10 * * 1') },
  { key: 'compliance-evidence', name: 'Compliance evidence pack', description: 'Collect missing evidence and prepare an audit-ready checklist.', category: 'Compliance', icon: 'i-lucide-clipboard-check', make: () => chatSchedule('Compliance evidence pack', 'Review the control evidence export in {{payload}}. List missing or expired evidence, map each gap to its control and owner, and draft a non-accusatory request. Never mark a control compliant without evidence.', '0 9 * * 1') },
  { key: 'ai-governance-review', name: 'AI governance review', description: 'Review agent changes for ownership, permissions and human controls.', category: 'AI Governance', icon: 'i-lucide-shield-ellipsis', make: () => chatWebhook('AI governance review', 'Review this AI system change: {{payload}}. Check purpose, data handling, permissions, auditability, fallback behavior, human approval gates and rollback plan. Flag risks and required owners before production.', { debounce_key: 'change.id', debounce_seconds: 30, debounce_max_seconds: 300 }) },
  { key: 'localization-review', name: 'Localization review', description: 'Check translated content for meaning, tone and product context.', category: 'Content', icon: 'i-lucide-languages', make: () => chatWebhook('Localization review', 'Review this localization payload: {{payload}}. Compare source and translation for meaning, tone, placeholders, cultural clarity and product terminology. Return only actionable corrections with examples.', { debounce_key: 'content.id', debounce_seconds: 30, debounce_max_seconds: 300 }) },
  { key: 'patient-message-routing', name: 'Patient message routing', description: 'Route healthcare messages without making clinical decisions.', category: 'Healthcare', icon: 'i-lucide-heart-pulse', make: () => chatWebhook('Patient message routing', 'Route this patient message: {{payload}}. Identify administrative intent, urgency signals and the right human queue. Do not diagnose, recommend treatment or repeat unnecessary personal health information.', { debounce_key: 'message.id', debounce_seconds: 20, debounce_max_seconds: 120 }) },
  { key: 'customer-routing', name: 'Customer case routing', description: 'Route customer cases by intent, urgency and expertise.', category: 'Customer Service', icon: 'i-lucide-route', make: () => chatWebhook('Customer case routing', 'Route this customer case: {{payload}}. Identify intent, urgency, sentiment, language, product area and best queue. Suggest a first response, but leave refunds, commitments and escalations to a person.', { debounce_key: 'case.id', debounce_seconds: 15, debounce_max_seconds: 120 }) },
  { key: 'case-summary', name: 'Case summary and next action', description: 'Turn a support thread into a handoff-ready summary.', category: 'Customer Service', icon: 'i-lucide-messages-square', make: () => chatWebhook('Case summary and next action', 'Summarize this customer case: {{payload}}. Capture the issue, timeline, customer goal, evidence, actions already tried, sentiment, unresolved questions and the safest next action. Keep private data minimal.', { debounce_key: 'case.id', debounce_seconds: 30, debounce_max_seconds: 300 }) },
  { key: 'agent-assist-quality', name: 'Support quality review', description: 'Sample support conversations against a consistent quality rubric.', category: 'Customer Service', icon: 'i-lucide-clipboard-check', make: () => chatSchedule('Support quality review', 'Review the sampled support conversations in {{payload}}. Score accuracy, empathy, policy adherence, resolution quality and escalation timing. Cite evidence, redact unnecessary personal data and recommend coaching themes.', '0 15 * * 5') },
  { key: 'ap-invoice-processing', name: 'AP invoice exception', description: 'Extract invoice fields and route exceptions before payment.', category: 'Finance & Risk', icon: 'i-lucide-file-check-2', make: () => chatWebhook('AP invoice exception', 'Review this accounts-payable invoice: {{payload}}. Extract vendor, totals, tax, currency, purchase order and due date; flag duplicates or mismatches; route exceptions to the right owner. Never approve or pay automatically.', { debounce_key: 'invoice.id', debounce_seconds: 30, debounce_max_seconds: 300 }) },
  { key: 'contract-clause-review', name: 'Contract clause review', description: 'Surface unusual or missing clauses for legal review.', category: 'Legal', icon: 'i-lucide-scale', make: () => chatWebhook('Contract clause review', 'Review this contract payload: {{payload}}. Compare important clauses against the supplied playbook, flag unusual obligations, missing protections, renewal/termination dates and data risks. Do not provide legal advice or approve the contract.', { debounce_key: 'contract.id', debounce_seconds: 30, debounce_max_seconds: 600 }) },
  { key: 'vendor-risk-monitor', name: 'Vendor risk monitor', description: 'Track supplier changes, incidents and expiring documents.', category: 'Procurement', icon: 'i-lucide-building-2', make: () => chatSchedule('Vendor risk monitor', 'Review this vendor risk export: {{payload}}. Flag security incidents, expiring certificates, concentration risk, SLA deterioration and missing owners. Rank follow-ups and state the evidence for each flag.', '0 9 * * 1') },
  { key: 'demand-forecast', name: 'Demand forecast brief', description: 'Explain demand changes and inventory planning risks.', category: 'Supply Chain', icon: 'i-lucide-chart-spline', make: () => chatSchedule('Demand forecast brief', 'Analyze this demand and inventory snapshot: {{payload}}. Explain trend and seasonality signals, stock-out or excess risk, forecast uncertainty and the assumptions a planner should validate before acting.', '0 7 * * 1') },
  { key: 'predictive-maintenance', name: 'Maintenance anomaly triage', description: 'Triage equipment signals without issuing unsafe commands.', category: 'Manufacturing', icon: 'i-lucide-wrench', make: () => chatWebhook('Maintenance anomaly triage', 'Triage this equipment signal: {{payload}}. Summarize the anomaly, possible causes, affected asset, evidence quality and recommended inspection priority. Do not control equipment or declare it safe.', { debounce_key: 'asset.id', debounce_seconds: 20, debounce_max_seconds: 180 }) },
  { key: 'product-feedback', name: 'Product feedback synthesis', description: 'Turn feedback into themes, opportunities and evidence.', category: 'Product', icon: 'i-lucide-lightbulb', make: () => chatSchedule('Product feedback synthesis', 'Synthesize the product feedback in {{payload}}. Group themes, quantify frequency when possible, separate requests from pain points, identify affected segments and propose experiments with evidence and open questions.', '0 10 * * 1') },
  { key: 'agent-outcome-eval', name: 'Agent outcome evaluation', description: 'Monitor agent quality, drift, cost and human overrides.', category: 'AI Governance', icon: 'i-lucide-gauge', make: () => chatSchedule('Agent outcome evaluation', 'Evaluate the agent run report in {{payload}}. Track success criteria, failure modes, escalation rate, human overrides, latency, cost and quality drift. Recommend one measurable improvement and one control to verify.', '0 11 * * 1') }
]

// Featured means “recommended starting point based on current demand signals”
// (not a fabricated user rating). The UI renders these with a star.
export const featuredTemplateKeys = new Set([
  'pr-review', 'incident-triage', 'security-digest', 'daily-sales', 'lead-qualification',
  'support-summary', 'cost-anomaly', 'it-ticket-triage', 'fraud-signal-review',
  'ai-governance-review', 'knowledge-refresh', 'customer-routing', 'ap-invoice-processing',
  'product-feedback', 'agent-outcome-eval'
])

export function emptyDraft(): AutomationDraft {
  return {
    name: '', enabled: true, source: 'schedule',
    config: { every_minutes: 0, cron: '0 8 * * 1-5', timezone: Intl.DateTimeFormat().resolvedOptions().timeZone, auth: 'bearer', auth_name: '', channel_id: '', keywords: [], scope: '', command: '', command_description: '', command_arg: '', skill: '', reply_mode: '', own_setup: false, pull_request: false, notify_channel_id: '', notify_chat_id: '', tags: [] },
    bot: { token: '', allow: [], refusal: '' },
    action: 'script', agent_id: '', prompt: '', edit_mode: 'worktree', model_tier: '', keep_context: false,
    script: { lang: 'bash', body: '', timeout_s: 300 },
    escalate: { when: 'never', action: 'chat', agent_id: '', prompt: '' }, // a script calls no agent in (ADR-057)
    limits: { max_parallel: 1, max_minutes: 0, max_runs_per_hour: 0, daily_cost_usd: 0, disable_after_failures: 5, debounce_seconds: 0, debounce_key: '', debounce_max_seconds: 0 },
    // ADR-074: "agent" follows the agent's own permissions; only "override" needs these
    permission_mode: 'agent', override_full_access: false, override_admin_by: '', override_extra_dirs: []
  }
}

export function draftFrom(a: Automation): AutomationDraft {
  const e = emptyDraft()
  const b = JSON.parse(JSON.stringify(automationBody(a))) as AutomationDraft
  const d: AutomationDraft = {
    ...e, ...b, config: { ...e.config, ...b.config }, limits: { ...e.limits, ...b.limits },
    bot: { token: '', allow: [...(a.bot?.allow ?? [])], refusal: a.bot?.refusal ?? '' },
    script: { ...e.script, ...(b.script?.lang ? b.script : {}) }, escalate: { ...e.escalate, ...(b.escalate?.when ? b.escalate : {}) }
  }
  if ((d.action as string) === 'task') d.action = 'chat' // Giao Việc is gone (ADR-057): the agent gets the message
  return d
}

const draftObjects = ['config', 'limits', 'script', 'escalate', 'bot'] as const
const draftScalars = ['name', 'enabled', 'source', 'action', 'agent_id', 'prompt', 'edit_mode', 'keep_context'] as const
const allowed: Record<string, readonly string[]> = {
  source: ['schedule', 'webhook', 'telegram', 'discord'], action: ['script', 'chat'], edit_mode: ['worktree', 'direct'], model_tier: ['', 'strong', 'balanced', 'fast'],
  'script.lang': ['bash', 'node', 'python'], 'escalate.when': ['never'],
  'config.auth': ['bearer', 'header', 'query']
}
const fits = (key: string, v: unknown) => !allowed[key] || allowed[key]!.includes(v as string)

// mergeDraft applies an agent's partial draft: only fields the draft has, of
// the same type and an allowed value (anything else is ignored, so a bad
// block cannot break the form or a later Save). A schedule is either a cron
// or every N minutes: setting one clears the other. It returns the top-level
// keys it changed.
export function mergeDraft(d: AutomationDraft, patch: Record<string, unknown>): string[] {
  const changed: string[] = []
  for (const k of draftScalars) {
    const v = k === 'action' && patch[k] === 'task' ? 'chat' : patch[k] // Giao Việc is gone (ADR-057): the agent gets the message
    if (v === undefined || typeof v !== typeof d[k] || !fits(k, v)) continue
    ;(d as Record<string, unknown>)[k] = v
    changed.push(k)
  }
  for (const k of draftObjects) {
    const v = patch[k]
    if (!v || typeof v !== 'object' || Array.isArray(v)) continue
    const target = d[k] as Record<string, unknown>
    let any = false
    for (const [f, x] of Object.entries(v as Record<string, unknown>)) {
      if (!(f in target) || x === null || typeof x !== typeof target[f] || !fits(`${k}.${f}`, x)) continue
      if (k === 'bot' && f === 'token') continue // a person pastes it; never from the chat
      target[f] = x
      any = true
    }
    if (k === 'config' && any) {
      const c = v as Record<string, unknown>
      if (typeof c.every_minutes === 'number' && c.every_minutes > 0 && c.cron === undefined) d.config.cron = ''
      if (typeof c.cron === 'string' && c.cron.trim() && c.every_minutes === undefined) d.config.every_minutes = 0
    }
    if (any) changed.push(k)
  }
  return changed
}
