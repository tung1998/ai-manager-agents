// What a watch box can show. Each widget names its
// icon, whether it needs a project, and its settings: the box's ⚙ form is made
// from these, the widget reads them with their defaults.
export type WidgetKind = 'chat' | 'process' | 'container' | 'monitor' | 'stat' | 'jobs' | 'chart' | 'incidents' | 'approvals' | 'system'
  | 'budget' | 'lights' | 'note' | 'git' | 'controls' | 'automations' | 'breakdown' | 'limits' | 'preview' | 'consoles'
import type { SizeRule } from './watchGrid'

// a box's room in grid cells (a cell is about 90px square): the size it starts
// at and how small/large it can be made. Small ones are figures, large ones
// pages and consoles.
export type WidgetGroup = 'sm' | 'md' | 'lg'

// source: a list loaded from the box's project (its processes, services, monitors)
export interface WidgetField {
  key: string
  type: 'switch' | 'select' | 'text'
  options?: (string | number)[]
  source?: 'processes' | 'services' | 'monitors'
}
export interface WidgetDef {
  kind: WidgetKind
  icon: string
  // required: nothing to show without one; optional: '' = every project
  project: 'required' | 'optional' | 'none'
  admin?: boolean
  grid: SizeRule
  fields: WidgetField[]
  defaults: Record<string, unknown>
}

export const widgetDefs: WidgetDef[] = [
  { kind: 'chat', grid: { w: 4, h: 6, minW: 4, minH: 5, maxW: 12, maxH: 12 }, icon: 'i-lucide-messages-square', project: 'required', fields: [], defaults: {} },
  {
    kind: 'process', grid: { w: 6, h: 5, minW: 2, minH: 1, maxW: 12, maxH: 10 }, icon: 'i-lucide-square-terminal', project: 'required',
    fields: [{ key: 'processId', type: 'select', source: 'processes' }, { key: 'console', type: 'switch' }, { key: 'controls', type: 'switch' }, { key: 'metrics', type: 'switch' }],
    defaults: { processId: '', console: true, controls: true, metrics: true }
  },
  {
    kind: 'container', grid: { w: 4, h: 4, minW: 2, minH: 1, maxW: 12, maxH: 10 }, icon: 'i-lucide-container', project: 'required',
    fields: [{ key: 'service', type: 'select', source: 'services' }, { key: 'console', type: 'switch' }, { key: 'controls', type: 'switch' }, { key: 'metrics', type: 'switch' }],
    defaults: { service: '', console: true, controls: true, metrics: true }
  },
  {
    kind: 'monitor', grid: { w: 3, h: 3, minW: 2, minH: 1, maxW: 6, maxH: 8 }, icon: 'i-lucide-heart-pulse', project: 'required',
    fields: [{ key: 'monitorId', type: 'select', source: 'monitors' }, { key: 'trend', type: 'switch' }],
    defaults: { monitorId: '', trend: true }
  },
  {
    kind: 'stat', grid: { w: 2, h: 2, minW: 1, minH: 1, maxW: 4, maxH: 4 }, icon: 'i-lucide-hash', project: 'optional',
    fields: [{ key: 'metric', type: 'select', options: ['running', 'pending', 'needs_input', 'failed_24h', 'cost_24h', 'incidents'] }, { key: 'trend', type: 'switch' }],
    defaults: { metric: 'running', trend: true }
  },
  {
    kind: 'jobs', grid: { w: 4, h: 4, minW: 3, minH: 2, maxW: 8, maxH: 10 }, icon: 'i-lucide-list-checks', project: 'optional',
    fields: [{ key: 'status', type: 'select', options: ['', 'running', 'failed', 'done', 'needs_input'] }, { key: 'limit', type: 'select', options: [10, 20, 50] }],
    defaults: { status: '', limit: 20 }
  },
  {
    kind: 'chart', grid: { w: 4, h: 3, minW: 2, minH: 2, maxW: 12, maxH: 6 }, icon: 'i-lucide-chart-column', project: 'optional',
    fields: [{ key: 'measure', type: 'select', options: ['jobs', 'cost'] }, { key: 'days', type: 'select', options: [7, 14, 30] }],
    defaults: { measure: 'jobs', days: 14 }
  },
  {
    kind: 'incidents', grid: { w: 4, h: 4, minW: 2, minH: 2, maxW: 8, maxH: 10 }, icon: 'i-lucide-siren', project: 'optional',
    fields: [{ key: 'severity', type: 'select', options: ['', 'error'] }],
    defaults: { severity: '' }
  },
  { kind: 'approvals', grid: { w: 4, h: 3, minW: 3, minH: 2, maxW: 8, maxH: 10 }, icon: 'i-lucide-stamp', project: 'optional', fields: [], defaults: {} },
  {
    kind: 'system', grid: { w: 3, h: 3, minW: 2, minH: 2, maxW: 6, maxH: 6 }, icon: 'i-lucide-cpu', project: 'none', admin: true,
    fields: [{ key: 'cores', type: 'switch' }, { key: 'disk', type: 'switch' }, { key: 'net', type: 'switch' }],
    defaults: { cores: false, disk: true, net: true }
  },
  { kind: 'budget', grid: { w: 2, h: 2, minW: 1, minH: 1, maxW: 3, maxH: 3 }, icon: 'i-lucide-wallet', project: 'optional', fields: [], defaults: {} },
  { kind: 'lights', grid: { w: 2, h: 2, minW: 1, minH: 1, maxW: 6, maxH: 4 }, icon: 'i-lucide-lightbulb', project: 'required', fields: [{ key: 'labels', type: 'switch' }], defaults: { labels: true } },
  { kind: 'note', grid: { w: 2, h: 2, minW: 1, minH: 1, maxW: 6, maxH: 6 }, icon: 'i-lucide-sticky-note', project: 'none', fields: [], defaults: { text: '' } },
  { kind: 'git', grid: { w: 2, h: 2, minW: 2, minH: 1, maxW: 4, maxH: 6 }, icon: 'i-lucide-git-branch', project: 'required', fields: [{ key: 'files', type: 'switch' }], defaults: { files: true } },
  { kind: 'controls', grid: { w: 4, h: 3, minW: 3, minH: 2, maxW: 6, maxH: 8 }, icon: 'i-lucide-sliders-horizontal', project: 'required', admin: true, fields: [{ key: 'services', type: 'switch' }], defaults: { services: true } },
  { kind: 'automations', grid: { w: 4, h: 3, minW: 3, minH: 2, maxW: 6, maxH: 8 }, icon: 'i-lucide-alarm-clock', project: 'required', fields: [{ key: 'disabled', type: 'switch' }], defaults: { disabled: false } },
  {
    kind: 'breakdown', grid: { w: 3, h: 3, minW: 2, minH: 2, maxW: 6, maxH: 6 }, icon: 'i-lucide-chart-bar', project: 'none',
    fields: [{ key: 'by', type: 'select', options: ['project', 'model'] }, { key: 'days', type: 'select', options: [1, 7, 30] }],
    defaults: { by: 'project', days: 7 }
  },
  { kind: 'limits', grid: { w: 3, h: 3, minW: 2, minH: 2, maxW: 6, maxH: 6 }, icon: 'i-lucide-gauge', project: 'none', fields: [], defaults: {} },
  {
    kind: 'preview', grid: { w: 6, h: 6, minW: 3, minH: 3, maxW: 12, maxH: 12 }, icon: 'i-lucide-app-window', project: 'optional',
    fields: [{ key: 'processId', type: 'select', source: 'processes' }, { key: 'url', type: 'text' }, { key: 'reload', type: 'select', options: [0, 30, 60, 300] }],
    defaults: { processId: '', url: '', reload: 0 }
  },
  {
    kind: 'consoles', grid: { w: 8, h: 5, minW: 4, minH: 3, maxW: 12, maxH: 12 }, icon: 'i-lucide-panels-top-left', project: 'required',
    fields: [{ key: 'runningOnly', type: 'switch' }, { key: 'max', type: 'select', options: [2, 4, 6] }],
    defaults: { runningOnly: true, max: 4 }
  }
]

// the gallery's groups, by the room a widget starts with
export const widgetGroup = (d: WidgetDef): WidgetGroup => d.grid.w * d.grid.h <= 4 ? 'sm' : d.grid.w * d.grid.h <= 16 ? 'md' : 'lg'

// the sizes offered in a box's menu, those its widget allows
const PRESETS: [number, number][] = [[1, 1], [2, 1], [2, 2], [3, 2], [3, 3], [4, 2], [4, 3], [4, 4], [6, 3], [6, 4], [6, 6], [8, 5], [12, 6], [12, 8]]
export const sizePresets = (r: SizeRule) => PRESETS.filter(([w, h]) => w >= r.minW && w <= r.maxW && h >= r.minH && h <= r.maxH)

export const widgetDef = (k: WidgetKind) => widgetDefs.find(w => w.kind === k) ?? widgetDefs[0]!

// a box's settings with the widget's defaults under them
export function widgetConfig(k: WidgetKind, config?: Record<string, unknown>): Record<string, unknown> {
  return { ...widgetDef(k).defaults, ...config }
}

// how long something has been up, short (45s, 12m 3s, 5h 20m, 3d 4h)
export function fmtUptime(s: number) {
  s = Math.max(0, Math.floor(s))
  if (s < 60) return `${s}s`
  if (s < 3600) return `${Math.floor(s / 60)}m ${s % 60}s`
  if (s < 86400) return `${Math.floor(s / 3600)}h ${Math.floor((s % 3600) / 60)}m`
  return `${Math.floor(s / 86400)}d ${Math.floor((s % 86400) / 3600)}h`
}
