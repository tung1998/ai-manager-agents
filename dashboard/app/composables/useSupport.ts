// The support pack (ADR-171, ADR-173) and data collections (ADR-172): what
// the API returns.
export interface CollectionField {
  name: string
  label?: string
  type: 'text' | 'number' | 'bool' | 'date' | 'enum' | 'tags' | 'ref' | 'conversation' | 'url' | 'file'
  options?: string[]
  ref?: string
  required?: boolean
  unique?: boolean
  indexed?: boolean
}
export interface Collection { id: string, key: string, name: string, description: string, pack: string, fields: CollectionField[] }
export interface DataRecord { id: string, data: Record<string, unknown>, created_by: string, updated_by: string, created_at: string, updated_at: string }
export interface RecordChange { id: string, op: 'create' | 'update' | 'delete', data: Record<string, unknown>, by: string, at: string }

export interface SupportSource {
  id: string
  kind: 'api' | 'form' | 'bot' | 'crisp'
  config: Record<string, unknown>
  name: string
  automation_id: string
  mode: 'auto' | 'approve'
  rate_per_min: number
  enabled: boolean
  created_at: string
}
export type TicketStatus = 'new' | 'open' | 'waiting_customer' | 'waiting_approval' | 'resolved' | 'closed'
export interface SupportTicket {
  id: string
  source_id: string
  external_thread: string
  customer_id: string
  subject: string
  status: TicketStatus
  case_type: string
  tags: string[]
  sla_due_at: string | null
  sla_breached: boolean
  first_response_at: string | null
  created_at: string
  updated_at: string
}
export interface SupportEvent {
  id: string
  kind: 'customer_msg' | 'ai_draft' | 'sent' | 'note' | 'status'
  body: string
  actor: string
  reply_to?: string
  state?: '' | 'pending' | 'approved' | 'rejected' | 'superseded' | 'failed' | 'sent' | 'queued' | 'human'
  meta?: Record<string, unknown>
  created_at: string
}

// the colors of a ticket's status
export const ticketColor: Record<TicketStatus, 'neutral' | 'info' | 'warning' | 'success' | 'error'> = {
  new: 'info', open: 'error', waiting_customer: 'neutral', waiting_approval: 'warning', resolved: 'success', closed: 'neutral'
}

// a record's value as text (tags joined, true/false as ✓/–)
export function fieldText(v: unknown): string {
  if (v === undefined || v === null || v === '') return '—'
  if (Array.isArray(v)) return v.join(', ')
  if (typeof v === 'boolean') return v ? '✓' : '–'
  return String(v)
}

export interface CaseType { key: string, name: string, keywords: string[], description: string, mode: '' | 'auto' | 'approve' | 'human', sla_minutes: number }
export interface SupportSettings { cases: CaseType[], sla_minutes: number, human_words: string[] }
export interface SupportHook { id: string, name: string, kind: 'webhook' | 'automation', url: string, automation_id: string, events: string[], enabled: boolean }
export interface SupportDelivery { id: string, kind: 'reply' | 'hook', ref_id: string, ticket_id: string, event: string, status: 'pending' | 'sent' | 'failed', attempts: number, last_error: string, created_at: string, updated_at: string }
export interface SupportReport {
  days: number
  tickets: number
  by_status: Record<string, number>
  by_case: Record<string, number>
  answered: number
  first_response_minutes: number
  sla_breached: number
  escalated: number
  ai_auto: number
  ai_approved: number
  ai_edited: number
  by_person: number
  ai_failed: number
  unanswered: string[]
}
