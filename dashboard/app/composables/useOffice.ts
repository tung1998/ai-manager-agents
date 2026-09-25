export type ModelTier = 'strong' | 'balanced' | 'fast'
export type AgentTier = 'lead' | 'manager' | 'worker'

export interface Provider {
  id: string
  name: string
  kind: string
  preset: string
  base_url: string
  has_api_key: boolean
  api_key_hint: string
  api_key_env: string
  tier_models: Partial<Record<ModelTier, string>>
  models: string[]
  is_default: boolean
  enabled: boolean
  status: 'unknown' | 'ok' | 'error'
  status_detail: string
  checked_at: string | null
}

export interface ProviderKind {
  kind: string
  label: string
  description: string
  common: boolean
  detected?: { installed?: boolean, version?: string, env_key?: string }
  needs_key: boolean
  is_cli: boolean
  base_url_hint: string
  tier_models: Partial<Record<ModelTier, string>>
}

export interface ProviderPreset {
  id: string
  name: string
  group: 'gateway' | 'global' | 'china' | 'local'
  base_url: string
  key_url?: string
  key_env?: string
  need_key: boolean
  note?: string
}
export interface ProviderStat {
  provider_id: string
  calls: number
  errors: number
  input_tokens: number
  output_tokens: number
  cost_usd: number
  avg_ms: number
  last_used_at: string | null
  top_model: string
  days: { day: string, calls: number, cost_usd: number }[]
}
export type PermLevel = 'read' | 'propose' | 'check' | 'edit' | 'operate'
// nested packages: each includes the ones before it
export const permLevels: { level: PermLevel, label: string, description: string, icon: string }[] = [
  { level: 'read', label: 'Chỉ đọc', description: 'Đọc code, log, trạng thái vận hành', icon: 'i-lucide-eye' },
  { level: 'propose', label: 'Đề xuất', description: 'Đề xuất sửa code và thao tác, người duyệt mới làm', icon: 'i-lucide-message-square-diff' },
  { level: 'check', label: 'Tự kiểm tra', description: 'Tự chạy lệnh kiểm tra được phép (test, typecheck, lint, build)', icon: 'i-lucide-flask-conical' },
  { level: 'edit', label: 'Tự sửa code', description: 'Tự áp diff áp được sạch, trừ file cấm', icon: 'i-lucide-pencil' },
  { level: 'operate', label: 'Vận hành', description: 'Tự chạy lại tiến trình và container được phép', icon: 'i-lucide-server-cog' }
]
export const permRank = (l?: string) => Math.max(0, permLevels.findIndex(x => x.level === l))
export const permOf = (l?: string) => permLevels[permRank(l)]!
export function agentLevel(p: Permissions): PermLevel {
  if (p.caps) return p.caps.reduce<PermLevel>((m, id) => { const c = permCaps.find(x => x.id === id); return c && permRank(c.min) > permRank(m) ? c.min : m }, 'read')
  if (p.level && permLevels.some(x => x.level === p.level)) return p.level
  return p.read_only ? 'read' : 'propose'
}

// single capabilities; a package is a preset of them (same list as internal/perm)
export type PermGroup = 'code' | 'commands' | 'git' | 'ops'
export interface PermCap { id: string, group: PermGroup, label: string, description: string, min: PermLevel, icon: string }
export const permCaps: PermCap[] = [
  { id: 'propose', group: 'code', label: 'Đề xuất', description: 'Đưa diff và đề xuất thao tác, người duyệt mới làm', min: 'propose', icon: 'i-lucide-message-square-diff' },
  { id: 'code.apply', group: 'code', label: 'Tự áp diff', description: 'Diff áp được sạch được áp ngay, trừ file cấm', min: 'edit', icon: 'i-lucide-pencil' },
  { id: 'commands.run', group: 'commands', label: 'Tự chạy lệnh', description: 'Chạy ngay các lệnh được chọn; lệnh khác phải đề xuất', min: 'check', icon: 'i-lucide-square-terminal' },
  { id: 'git.commit', group: 'git', label: 'Tự commit', description: 'Commit các file đã sửa với message rõ ràng', min: 'edit', icon: 'i-lucide-git-commit-horizontal' },
  { id: 'git.branch', group: 'git', label: 'Tự tạo nhánh', description: 'Tạo và chuyển sang nhánh mới', min: 'operate', icon: 'i-lucide-git-branch' },
  { id: 'ops.process', group: 'ops', label: 'Tự chạy lại tiến trình', description: 'Chạy, chạy lại, dừng tiến trình được phép', min: 'operate', icon: 'i-lucide-rotate-cw' },
  { id: 'ops.container', group: 'ops', label: 'Tự điều khiển container', description: 'Bật, chạy lại, dừng container được phép', min: 'operate', icon: 'i-lucide-container' }
]
export const permGroups: { id: PermGroup, label: string, icon: string }[] = [
  { id: 'code', label: 'Code', icon: 'i-lucide-code' },
  { id: 'commands', label: 'Lệnh', icon: 'i-lucide-square-terminal' },
  { id: 'git', label: 'Git', icon: 'i-lucide-git-fork' },
  { id: 'ops', label: 'Vận hành', icon: 'i-lucide-server-cog' }
]
export const presetCaps = (l: PermLevel) => permCaps.filter(c => permRank(c.min) <= permRank(l)).map(c => c.id)
export const agentCaps = (p: Permissions) => p.caps ?? presetCaps(agentLevel(p))

export interface CommandPack { id: string, label: string, icon?: string, commands: string[], custom?: boolean }

export interface Permissions {
  level?: PermLevel
  read_only: boolean
  caps?: string[] | null
  commands?: string[] | null
  tools?: string[]
  requires_approval?: boolean
}

export interface Agent {
  id: string
  org_model_id: string
  key: string
  name: string
  tier: AgentTier
  role: string
  description: string
  reports_to: string[]
  provider_id: string
  model_tier: ModelTier
  llm_model: string
  instructions: string
  permissions: Permissions
  sort: number
}

export interface Governance {
  mode: string
  quorum?: number
  veto?: string[]
  notes?: string
}

export interface OrgModel {
  id: string
  repo_id: string
  source_template_id: string
  key: string
  name: string
  description: string
  kind: 'solo' | 'team' | 'council' | 'custom'
  governance: Governance
  builtin: boolean
  is_template: boolean
  agent_count: number
  tiers: Partial<Record<AgentTier, number>>
  agents?: Agent[]
  updated_at: string
}

export interface Project {
  id: string
  name: string
  path: string // '' for a machine-wide helper
  scope: 'folder' | 'machine'
  git_remote: string
  description: string
  exists: boolean
  model: OrgModel | null
  created_at: string
}

export const tierLabel: Record<AgentTier, string> = { lead: 'Lead', manager: 'Manager', worker: 'Worker' }
export const modelTierLabel: Record<ModelTier, string> = { strong: 'Mạnh', balanced: 'Cân bằng', fast: 'Nhanh' }
export const kindLabel: Record<string, string> = { solo: 'Solo', team: 'Team', council: 'Hội đồng', custom: 'Tùy chỉnh' }
export const kindIcon: Record<string, string> = {
  solo: 'i-lucide-user',
  team: 'i-lucide-users',
  council: 'i-lucide-landmark',
  custom: 'i-lucide-shapes'
}
export const governanceLabel: Record<string, string> = {
  single: 'Một agent tự quyết',
  hierarchy: 'Phân cấp: lead chốt',
  council: 'Hội đồng: biểu quyết theo quorum'
}
