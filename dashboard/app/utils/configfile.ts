// A config file (properties, ini, toml, yaml, json, xml) as a list of keys
// and values, edited line by line: a value is changed in place, so every
// other byte of the file (order, indentation, comments) stays as it was.
// Nested keys read as a path ("db.host"). Like a .env (./dotenv.ts), a key
// turned off is its line commented out and the comment lines right above it
// are its note; json has no comments, so there only values change.
import { parseEnv, writeEnv, envChanged, newEnvVar, type EnvDoc, type EnvVar } from './dotenv.ts'

export type ConfigFormat = 'env' | 'properties' | 'ini' | 'toml' | 'yaml' | 'json' | 'xml'
// what the person can do to a key, per format
export interface ConfigCaps { rename: boolean, add: boolean, remove: boolean, toggle: boolean, notes: boolean }
export const configCaps: Record<ConfigFormat, ConfigCaps> = {
  env: { rename: true, add: true, remove: true, toggle: true, notes: true },
  properties: { rename: true, add: true, remove: true, toggle: true, notes: true },
  ini: { rename: false, add: false, remove: true, toggle: true, notes: true },
  toml: { rename: false, add: false, remove: true, toggle: true, notes: true },
  yaml: { rename: false, add: false, remove: true, toggle: true, notes: true },
  json: { rename: false, add: false, remove: false, toggle: false, notes: false },
  xml: { rename: false, add: false, remove: true, toggle: true, notes: true }
}

// a key of a line-based file: the line is before + value + after
export interface ConfigVar extends EnvVar {
  before: string
  after: string
  indent: string
  sep: string // properties: what stands between key and value ("=", ": ")
}
export interface ConfigLine { kind: 'line', raw: string }
export type ConfigEntry = ConfigVar | ConfigLine
export interface ConfigDoc { format: ConfigFormat, entries: (ConfigEntry | EnvVar | ConfigLine)[], eol: string, finalEol: boolean, env?: EnvDoc }

// a line's key and where its value sits; null: not a key
interface Hit { key: string, local: string, indent: string, before: string, value: string, quote: '' | '"' | '\'', after: string, sep?: string }
interface Format {
  comment: (line: string, indent: string) => string // turn a line off
  uncomment: (line: string) => string | null // the line inside a comment (null: not a comment)
  note: (text: string, indent: string) => string
  noteText: (line: string) => string
  // reads a line in its context; push/pop the path as the format nests
  scan: (line: string, ctx: Ctx) => Hit | null
}
// inBlock: inside a multi-line string or comment, whose lines are no keys
interface Ctx { path: { indent: number, key: string }[], section: string, skipIndent: number, inBlock: boolean }

const hashComment = (prefix = '#'): Pick<Format, 'comment' | 'uncomment' | 'note' | 'noteText'> => ({
  comment: (line, indent) => `${indent}${prefix} ${line.slice(indent.length)}`,
  uncomment: (line) => {
    const m = line.match(/^(\s*)[#;!]\s?(.*)$/)
    return m ? m[1]! + m[2]! : null
  },
  note: (text, indent) => (text.trim() ? `${indent}${prefix} ${text}` : `${indent}${prefix}`),
  noteText: line => line.replace(/^\s*[#;!]\s?/, '')
})

// a value with a trailing " # comment" (not inside quotes)
function splitValue(rest: string, commentRe: RegExp): { value: string, quote: Hit['quote'], after: string } {
  const q = rest[0]
  if (q === '"' || q === '\'') {
    for (let i = 1; i < rest.length; i++) {
      if (q === '"' && rest[i] === '\\') {
        i++
        continue
      }
      if (rest[i] === q) return { value: rest.slice(1, i), quote: q, after: rest.slice(i + 1) }
    }
  }
  const m = rest.match(commentRe)
  const end = m?.index ?? rest.length
  const value = rest.slice(0, end).trimEnd()
  return { value, quote: '', after: rest.slice(value.length) }
}

const properties: Format = {
  ...hashComment('#'),
  scan(line) {
    const m = line.match(/^(\s*)([^\s=:#!][^=:\s]*)(\s*[=:]\s*)(.*)$/)
    if (!m) return null
    return { key: m[2]!, local: m[2]!, indent: m[1]!, before: m[1]! + m[2]! + m[3]!, value: m[4]!, quote: '', after: '', sep: m[3]! }
  }
}

// ini and toml: [section] headers, key = value
const iniLike = (prefix: string): Format => ({
  ...hashComment(prefix),
  scan(line, ctx) {
    if (ctx.inBlock) {
      if (/("""|''')/.test(line)) ctx.inBlock = false
      return null
    }
    const h = line.match(/^\s*\[\[?\s*([^\]]+?)\s*\]\]?\s*([#;].*)?$/)
    if (h) {
      ctx.section = h[1]!.replace(/["']/g, '')
      return null
    }
    const m = line.match(/^(\s*)([A-Za-z0-9_.\-"' ]+?)(\s*[=:]\s*)(.*)$/)
    if (!m || /^\s*[#;]/.test(line)) return null
    const v = splitValue(m[4]!, /\s+[#;]/)
    // a multi-line array or string goes on below: edited as raw text only
    if (/^("""|''')/.test(m[4]!) && !/("""|''')\s*([#;].*)?$/.test(m[4]!.slice(3))) {
      ctx.inBlock = true
      return null
    }
    if (m[4]!.startsWith('[') && !/\]\s*([#;].*)?$/.test(m[4]!)) return null
    const local = m[2]!.replace(/["']/g, '').trim()
    return { key: ctx.section ? `${ctx.section}.${local}` : local, local, indent: m[1]!, before: m[1]! + m[2]! + m[3]!, ...v }
  }
})

const yaml: Format = {
  ...hashComment('#'),
  scan(line, ctx) {
    if (/^\s*(#|$)/.test(line) || /^\s*(---|\.\.\.)\s*$/.test(line)) return null
    const m = line.match(/^(\s*)(- +)?("[^"]*"|'[^']*'|[^\s:#][^:#]*?)\s*:(?:(\s+)(.*))?$/)
    const indent = (m?.[1] ?? line.match(/^\s*/)![0]).length + (m?.[2]?.length ?? 0)
    if (ctx.skipIndent >= 0) { // inside a block scalar (key: |)
      if (indent > ctx.skipIndent) return null
      ctx.skipIndent = -1
    }
    while (ctx.path.length && ctx.path[ctx.path.length - 1]!.indent >= indent) ctx.path.pop()
    if (!m) return null
    const local = m[3]!.replace(/^["']|["']$/g, '')
    const rest = m[5] ?? ''
    if (!rest || /^#/.test(rest) || /^&\S+\s*$/.test(rest)) { // a parent of what is below
      ctx.path.push({ indent, key: local })
      return null
    }
    if (/^[|>]/.test(rest)) {
      ctx.skipIndent = indent
      return null
    }
    const v = splitValue(rest, /\s+#/)
    const key = [...ctx.path.map(p => p.key), local].join('.')
    return { key, local, indent: m[1]!, before: line.slice(0, line.length - rest.length), ...v }
  }
}

const json: Format = {
  comment: line => line,
  uncomment: () => null,
  note: () => '',
  noteText: line => line,
  scan(line, ctx) {
    const t = line.trim()
    if (/^[}\]]/.test(t)) {
      ctx.path.pop()
      return null
    }
    const m = line.match(/^(\s*)"((?:[^"\\]|\\.)*)"\s*:\s*(.*?)\s*(,?)\s*$/)
    if (!m) {
      if (/[{[]$/.test(t)) ctx.path.push({ indent: 0, key: '' }) // an object or array opens with no key
      return null
    }
    const rest = m[3]!
    if (rest === '{' || rest === '[') {
      ctx.path.push({ indent: 0, key: m[2]! })
      return null
    }
    if (!/^("(?:[^"\\]|\\.)*"|-?\d[\d.eE+-]*|true|false|null)$/.test(rest)) return null
    const quoted = rest.startsWith('"')
    const key = [...ctx.path.map(p => p.key).filter(Boolean), m[2]!].join('.')
    const before = line.slice(0, line.lastIndexOf(rest, line.length - m[4]!.length))
    return { key, local: m[2]!, indent: m[1]!, before, value: quoted ? rest.slice(1, -1) : rest, quote: quoted ? '"' : '', after: line.slice(before.length + rest.length) }
  }
}

const xml: Format = {
  comment: (line, indent) => `${indent}<!-- ${line.slice(indent.length)} -->`,
  uncomment: (line) => {
    const m = line.match(/^(\s*)<!--\s?(.*?)\s?-->\s*$/)
    return m ? m[1]! + m[2]! : null
  },
  note: (text, indent) => `${indent}<!-- ${text} -->`,
  noteText: line => line.replace(/^\s*<!--\s?/, '').replace(/\s?-->\s*$/, ''),
  scan(line, ctx) {
    const t = line.trim()
    if (ctx.inBlock) {
      if (t.includes('-->')) ctx.inBlock = false
      return null
    }
    if (t.startsWith('<!--') && !t.includes('-->')) {
      ctx.inBlock = true
      return null
    }
    if (!t.startsWith('<') || /^<[?!]/.test(t)) return null
    // <add key="K" value="V" /> (appSettings and friends)
    const kv = line.match(/^(\s*)<[\w:.-]+\b[^>]*?\b(?:key|name)\s*=\s*"([^"]*)"[^>]*?\bvalue\s*=\s*"/)
    if (kv) {
      const start = kv[0].length
      const end = line.indexOf('"', start)
      if (end > 0) return { key: kv[2]!, local: kv[2]!, indent: kv[1]!, before: line.slice(0, start), value: line.slice(start, end), quote: '', after: line.slice(end) }
    }
    // <tag attrs>text</tag>
    const leaf = line.match(/^(\s*)<([\w:.-]+)(\s[^>]*)?>([^<]*)<\/\2>\s*$/)
    if (leaf) {
      const before = `${leaf[1]}<${leaf[2]}${leaf[3] ?? ''}>`
      const key = [...ctx.path.map(p => p.key), leaf[2]!].join('.')
      return { key, local: leaf[2]!, indent: leaf[1]!, before, value: leaf[4]!, quote: '', after: line.slice(before.length + leaf[4]!.length) }
    }
    // opening and closing tags on their own lines nest the path
    for (const tag of t.matchAll(/<(\/?)([\w:.-]+)[^>]*?(\/?)>/g)) {
      if (tag[1]) ctx.path.pop()
      else if (!tag[3]) ctx.path.push({ indent: 0, key: tag[2]! })
    }
    return null
  }
}

const formats: Record<Exclude<ConfigFormat, 'env'>, Format> = { properties, ini: iniLike('#'), toml: iniLike('#'), yaml, json, xml }

export function parseConfig(text: string, format: ConfigFormat): ConfigDoc {
  if (format === 'env') {
    const env = parseEnv(text)
    return { format, entries: env.entries, eol: env.eol, finalEol: env.finalEol, env }
  }
  const f = formats[format]
  const eol = text.includes('\r\n') ? '\r\n' : '\n'
  const finalEol = text === '' || text.endsWith('\n')
  const src = text.split(/\r?\n/)
  if (finalEol) src.pop()
  const ctx: Ctx = { path: [], section: '', skipIndent: -1, inBlock: false }
  const entries: ConfigEntry[] = []
  let notes: string[] = []
  const flush = () => {
    for (const raw of notes) entries.push({ kind: 'line', raw })
    notes = []
  }
  for (const line of src) {
    const inner = f.uncomment(line)
    // a commented-out key ("# port: 80", not "# Port: the one…"): read in a
    // copy of the context, so it nests nothing
    let hit = inner !== null && inner.trim() && !ctx.inBlock ? f.scan(inner, { path: [...ctx.path], section: ctx.section, skipIndent: -1, inBlock: false }) : null
    if (hit && !/^[\w.-]+$/.test(hit.local)) hit = null
    if (inner !== null && !hit) {
      notes.push(line)
      continue
    }
    const h = hit ?? f.scan(line, ctx)
    if (!h) {
      flush()
      entries.push({ kind: 'line', raw: line })
      continue
    }
    const v: ConfigVar = {
      kind: 'var', key: h.key, value: h.value, on: !hit, note: notes.map(f.noteText).join('\n'), quote: h.quote, exported: false, inline: '',
      raw: [...notes, line], before: h.before, after: h.after, indent: h.indent, sep: h.sep ?? '='
    }
    v.orig = { key: v.key, value: v.value, on: v.on, note: v.note }
    entries.push(v)
    notes = []
  }
  flush()
  return { format, entries, eol, finalEol }
}

function jsonLiteral(v: ConfigVar): string {
  if (v.quote || /^(-?\d+(\.\d+)?([eE][+-]?\d+)?|true|false|null)$/.test(v.value)) return v.quote + v.value + v.quote
  return `"${v.value.replace(/\\/g, '\\\\').replace(/"/g, '\\"')}"`
}

function writeVar(v: ConfigVar, format: Exclude<ConfigFormat, 'env'>): string[] {
  if (!envChanged(v)) return v.raw
  const f = formats[format]
  const before = format === 'properties' && v.orig?.key !== v.key ? v.indent + v.key.trim() + v.sep : v.before
  const value = format === 'json' ? jsonLiteral(v) : v.quote + v.value + v.quote
  const line = before + value + v.after
  const note = v.note.trim() ? v.note.split('\n').map(l => f.note(l, v.indent)) : []
  return [...note, v.on ? line : f.comment(line, v.indent)]
}

export function writeConfig(doc: ConfigDoc): string {
  if (doc.format === 'env') return writeEnv({ entries: doc.entries as EnvDoc['entries'], eol: doc.eol, finalEol: doc.finalEol })
  const format = doc.format
  const lines = doc.entries.flatMap(e => (e.kind === 'line' ? [e.raw] : writeVar(e as ConfigVar, format)))
  if (!lines.length) return ''
  return lines.join(doc.eol) + (doc.finalEol ? doc.eol : '')
}

export function newConfigVar(format: ConfigFormat): EnvVar {
  const v = newEnvVar()
  if (format === 'properties') Object.assign(v, { before: '', after: '', indent: '', sep: '=' })
  return v
}
