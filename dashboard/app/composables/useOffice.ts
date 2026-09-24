export type ModelTier = 'strong' | 'balanced' | 'fast'
export type AgentTier = 'lead' | 'manager' | 'worker'

export interface Provider {
  id: string
  name: string
  kind: string
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

export interface Permissions {
  read_only: boolean
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
