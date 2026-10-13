// A .env file as a list of variables to edit, written back with every line
// the person did not touch kept byte for byte. A variable that is turned off
// is the same line commented out ("# KEY=value"); the "# …" lines right above
// a variable are its note.

export interface EnvVar {
  kind: 'var'
  key: string
  value: string
  on: boolean
  note: string // the comment lines above it, without "# ", joined by \n
  quote: '' | '"' | '\''
  exported: boolean
  inline: string // " # comment" after the value, kept as is
  raw: string[] // the lines it came from (note + variable); [] when new
  orig?: { key: string, value: string, on: boolean, note: string }
}
// a line that is not a variable (blank, a comment not above one…)
export interface EnvLine { kind: 'line', raw: string }
export type EnvEntry = EnvVar | EnvLine
export interface EnvDoc { entries: EnvEntry[], eol: string, finalEol: boolean }

const varRe = /^(\s*)(#\s*)?(export\s+)?([A-Za-z_][A-Za-z0-9_.-]*)\s*=(.*)$/

// the value after "=": quoted (maybe over several lines) or bare up to " #"
function readValue(rest: string, more: () => string | undefined): { value: string, quote: EnvVar['quote'], inline: string, lines: string[] } | null {
  const s = rest.replace(/^\s+/, '')
  const q = s[0]
  if (q === '"' || q === '\'') {
    let body = s.slice(1)
    const lines: string[] = []
    for (;;) {
      let end = -1
      for (let i = 0; i < body.length; i++) {
        if (q === '"' && body[i] === '\\') {
          i++
          continue
        }
        if (body[i] === q) {
          end = i
          break
        }
      }
      if (end >= 0) {
        const all = [...lines, body.slice(0, end)].join('\n')
        return { value: all, quote: q, inline: body.slice(end + 1), lines }
      }
      const next = more()
      if (next === undefined) return null // never closed: not a variable
      lines.push(body)
      body = next
    }
  }
  const m = s.match(/^(.*?)(\s+#.*)?$/)!
  return { value: m[1]!.trimEnd(), quote: '', inline: m[2] ?? '', lines: [] }
}

export function parseEnv(text: string): EnvDoc {
  const eol = text.includes('\r\n') ? '\r\n' : '\n'
  const finalEol = text === '' || text.endsWith('\n')
  const src = text.split(/\r?\n/)
  if (finalEol) src.pop()
  const entries: EnvEntry[] = []
  let notes: string[] = [] // comment lines waiting for the variable below them
  const flush = () => {
    for (const raw of notes) entries.push({ kind: 'line', raw })
    notes = []
  }
  for (let i = 0; i < src.length; i++) {
    const line = src[i]!
    const m = line.match(varRe)
    if (m) {
      let j = i
      const v = readValue(m[5]!, () => (j + 1 < src.length ? src[++j] : undefined))
      // a commented-out multi-line value is too unsure to be a variable
      if (v && !(m[2] && v.lines.length)) {
        const note = notes.map(n => n.replace(/^\s*#\s?/, '')).join('\n')
        const e: EnvVar = {
          kind: 'var', key: m[4]!, value: v.value, on: !m[2], note, quote: v.quote, exported: !!m[3], inline: v.inline,
          raw: [...notes, ...src.slice(i, j + 1)]
        }
        e.orig = { key: e.key, value: e.value, on: e.on, note: e.note }
        entries.push(e)
        notes = []
        i = j
        continue
      }
    }
    if (/^\s*#/.test(line)) {
      notes.push(line)
      continue
    }
    flush()
    entries.push({ kind: 'line', raw: line })
  }
  flush()
  return { entries, eol, finalEol }
}

function quoteValue(v: EnvVar): string {
  let q = v.quote
  if (!q && (/[\s#"'\\]/.test(v.value) || v.value !== v.value.trim())) q = v.value.includes('"') ? '\'' : '"'
  if (q === '\'' && v.value.includes('\'')) q = '"'
  const value = q === '"' && v.quote !== '"' ? v.value.replace(/\\/g, '\\\\').replace(/"/g, '\\"') : v.value
  return q + value + q
}

export const envChanged = (v: EnvVar) => !v.orig || v.orig.key !== v.key || v.orig.value !== v.value || v.orig.on !== v.on || v.orig.note !== v.note

function writeVar(v: EnvVar): string[] {
  if (!envChanged(v)) return v.raw
  const note = v.note.trim() ? v.note.split('\n').map(l => (l.trim() ? `# ${l}` : '#')) : []
  const line = `${v.on ? '' : '# '}${v.exported ? 'export ' : ''}${v.key.trim()}=${quoteValue(v)}${v.inline}`
  return [...note, ...line.split('\n')]
}

export function writeEnv(doc: EnvDoc): string {
  const lines = doc.entries.flatMap(e => (e.kind === 'line' ? [e.raw] : writeVar(e)))
  if (!lines.length) return ''
  return lines.join(doc.eol) + (doc.finalEol ? doc.eol : '')
}

export const newEnvVar = (key = '', value = ''): EnvVar => ({ kind: 'var', key, value, on: true, note: '', quote: '', exported: false, inline: '', raw: [] })
