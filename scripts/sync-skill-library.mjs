#!/usr/bin/env node
// Downloads antigravity-awesome-skills (code MIT, content CC BY 4.0) and
// copies its skills into the office's machine library (<office>/library/skills),
// not into the repo. Each SKILL.md gets `category` and `tags` from the
// catalog (data/catalog.json) in its frontmatter, so the library page can
// filter by them. A skill of the same name that is not from a catalog (no
// `source:` in its frontmatter) is the person's own: left alone.
//
//   node scripts/sync-skill-library.mjs                  # latest main, the office of cwd
//   node scripts/sync-skill-library.mjs --sha=<commit> --dir=<office>/library/skills
import { cpSync, existsSync, mkdtempSync, readFileSync, readdirSync, rmSync, writeFileSync } from 'node:fs'
import { execFileSync } from 'node:child_process'
import { homedir, tmpdir } from 'node:os'
import { dirname, join, resolve } from 'node:path'

const REPO = 'https://github.com/sickn33/antigravity-awesome-skills'
const arg = name => process.argv.find(a => a.startsWith(`--${name}=`))?.slice(name.length + 3)

// the office home as the server finds it: OFFICE_HOME, a .office above cwd, ~/.agent-office
function officeHome() {
  if (process.env.OFFICE_HOME) return process.env.OFFICE_HOME
  for (let d = process.cwd(); ; d = dirname(d)) {
    if (existsSync(join(d, '.office', 'office.db'))) return join(d, '.office')
    if (dirname(d) === d) return join(homedir(), '.agent-office')
  }
}
const dest = resolve(arg('dir') ?? join(officeHome(), 'library', 'skills'))

const src = mkdtempSync(join(tmpdir(), 'aas-'))
try {
  const sha = arg('sha')
  execFileSync('git', ['clone', '-q', ...(sha ? [] : ['--depth', '1']), REPO, src], { stdio: 'inherit' })
  if (sha) execFileSync('git', ['checkout', '-q', sha], { cwd: src })
  const head = execFileSync('git', ['rev-parse', '--short', 'HEAD'], { cwd: src }).toString().trim()
  const catalog = new Map(JSON.parse(readFileSync(join(src, 'data/catalog.json'), 'utf8')).skills.map(s => [s.id, s]))

  let copied = 0, kept = 0
  for (const name of readdirSync(join(src, 'skills')).sort()) {
    const from = join(src, 'skills', name)
    if (!/^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/.test(name) || !existsSync(join(from, 'SKILL.md'))) continue
    const to = join(dest, name)
    if (existsSync(join(to, 'SKILL.md')) && !/^source:/m.test(frontmatter(readFileSync(join(to, 'SKILL.md'), 'utf8')))) {
      kept++
      continue
    }
    rmSync(to, { recursive: true, force: true })
    cpSync(from, to, { recursive: true, dereference: true }) // a few skills are symlinks to another
    const meta = catalog.get(name)
    if (meta) writeFileSync(join(to, 'SKILL.md'), withMeta(readFileSync(join(to, 'SKILL.md'), 'utf8'), meta))
    copied++
  }
  console.log(`${head}: ${copied} skills → ${dest}${kept ? `, ${kept} of the same name kept (not from a catalog)` : ''}`)
} finally {
  rmSync(src, { recursive: true, force: true })
}

function frontmatter(md) {
  return md.match(/^---\s*\n([\s\S]*?)\n---/)?.[1] ?? ''
}

// withMeta sets `category` and `tags: [a, b]` in the frontmatter, dropping
// the ones there (tags may be a YAML block list).
function withMeta(md, meta) {
  const fm = frontmatter(md)
  if (!fm && !md.startsWith('---')) return md
  const lines = []
  let skipping = false
  for (const line of fm.split('\n')) {
    if (/^(category|tags):/.test(line)) { skipping = true; continue }
    if (skipping && /^(\s+\S|-(\s|$))/.test(line)) continue
    skipping = false
    lines.push(line)
  }
  const clean = s => String(s).replace(/[\[\],:#"']/g, '').trim()
  const tags = [...new Set((meta.tags ?? []).map(clean).filter(Boolean))]
  if (meta.category) lines.push(`category: ${clean(meta.category)}`)
  if (tags.length) lines.push(`tags: [${tags.join(', ')}]`)
  return md.replace(/^---\s*\n[\s\S]*?\n---/, `---\n${lines.join('\n')}\n---`)
}
