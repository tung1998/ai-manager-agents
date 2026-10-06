#!/usr/bin/env node
// Builds the public copy of agent-office from this (private) repo: the files
// as they are now (what git tracks or would add), without its history, without the internal notes, with the
// public module path, checked for anything that must not leave, and committed
// as one commit under a no-reply author. Nothing is pushed.
//
//   node scripts/export-public.mjs                       # → ../agent-office next to this repo
//   node scripts/export-public.mjs --to ~/code/agent-office --module github.com/me/agent-office
//
// Then, once: git -C ../agent-office remote add origin <github url>
//             git -C ../agent-office push -u origin main
import { execFileSync } from 'node:child_process'
import { cpSync, existsSync, mkdirSync, mkdtempSync, readdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, extname, join, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const arg = (name, fallback) => {
  const a = process.argv.find(x => x.startsWith(`--${name}=`))
  if (a) return a.slice(name.length + 3)
  const i = process.argv.indexOf(`--${name}`)
  return i > 0 && process.argv[i + 1] ? process.argv[i + 1] : fallback
}
const git = (args, cwd) => execFileSync('git', args, { cwd, stdio: ['ignore', 'pipe', 'pipe'] }).toString().trim()

const here = dirname(fileURLToPath(import.meta.url))
const repo = git(['rev-parse', '--show-toplevel'], here)
// a worktree of the repo exports next to the main checkout, not inside .office/worktrees
const mainRoot = dirname(git(['rev-parse', '--path-format=absolute', '--git-common-dir'], repo))

const TO = resolve(arg('to', join(dirname(mainRoot), 'agent-office')))
const MODULE = arg('module', 'github.com/tung1998/agent-office')
const PAGES = arg('pages', 'https://tung1998.github.io/agent-office')
const NAME = arg('name', 'tung1998')
const EMAIL = arg('email', 'tung1998@users.noreply.github.com')
const MESSAGE = arg('message', `agent-office ${git(['log', '-1', '--format=%cs'], repo)}`)
const FORCE = process.argv.includes('--force')

const PRIVATE_MODULE = 'bitbucket.org/senprints/agent-office'
const PRIVATE_PAGES = 'https://tung1998.github.io/ai-manager-agents'

// internal notes: plans, decisions, the early CLI draft
const EXCLUDE = ['docs/PLAN.md', 'docs/DECISIONS.md', 'docs/CLI.md', 'docs/superpowers', 'scripts/export-public.mjs', 'scripts/public-sync.mjs']

// what must never be in the public copy
const FORBIDDEN = [
  [/senprints/i, 'company name'],
  [/senhub|printora/i, 'company name'],
  [/storefront-v5|backend-apis|admin_v3|seller_v3/, 'internal repo'],
  [/tungbt|@senprints\./i, 'work account'],
  [/\/Users\/sen\b|sens-Mac/, 'local machine'],
  [/1554696300254199890/, 'real chat id'],
  [/sk-ant-api\d\d-[A-Za-z0-9_-]{30,}|sk-proj-[A-Za-z0-9_-]{20,}/, 'API key'],
  [/ghp_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{30,}|xox[bp]-\d+-/, 'token'],
  [/AKIA[0-9A-Z]{16}/, 'AWS key'],
  [/discord(app)?\.com\/api\/webhooks\/\d+\/[\w-]{20,}/, 'webhook'],
  [/\d{8,10}:AA[\w-]{30,}/, 'Telegram bot token'],
  [/BEGIN [A-Z ]*PRIVATE KEY/, 'private key']
]
const TEXT = new Set(['.go', '.mod', '.sum', '.md', '.mjs', '.js', '.ts', '.vue', '.json', '.yml', '.yaml', '.html', '.css', '.sql', '.sh', '.txt', '.toml', '.example', ''])
const isText = f => TEXT.has(extname(f)) || /(^|\/)(Makefile|Dockerfile[^/]*|\.gitignore|\.dockerignore|LICENSE)$/.test(f)

function walk(dir, out = []) {
  for (const e of readdirSync(dir, { withFileTypes: true })) {
    const p = join(dir, e.name)
    if (e.isDirectory()) walk(p, out)
    else out.push(p)
  }
  return out
}

// 1. the files git sees here (tracked and new, never the ignored ones: .env, .office, builds)
const stage = mkdtempSync(join(tmpdir(), 'agent-office-public-'))
const src = join(stage, 'src')
for (const rel of git(['ls-files', '--cached', '--others', '--exclude-standard'], repo).split('\n').filter(Boolean)) {
  const from = join(repo, rel)
  if (!existsSync(from)) continue // deleted, not yet committed
  mkdirSync(dirname(join(src, rel)), { recursive: true })
  cpSync(from, join(src, rel))
}

// 2. leave the internal notes out
for (const p of EXCLUDE) rmSync(join(src, p), { recursive: true, force: true })

// 3. public module path and site, then look for leaks
const hits = []
for (const file of walk(src)) {
  const rel = relative(src, file)
  if (!isText(rel)) continue
  const text = readFileSync(file, 'utf8')
  const out = text.split(PRIVATE_MODULE).join(MODULE).split(PRIVATE_PAGES).join(PAGES)
  if (out !== text) writeFileSync(file, out)
  out.split('\n').forEach((line, i) => {
    for (const [re, what] of FORBIDDEN) if (re.test(line)) hits.push(`${rel}:${i + 1}  [${what}]  ${line.trim().slice(0, 120)}`)
  })
}
if (hits.length) {
  console.error(`✗ ${hits.length} line(s) must not go public:\n  ` + hits.join('\n  '))
  if (!FORCE) {
    rmSync(stage, { recursive: true, force: true })
    process.exit(1)
  }
}

// 4. into the public repo: replace everything but its .git
if (!existsSync(TO)) mkdirSync(TO, { recursive: true })
if (!existsSync(join(TO, '.git'))) git(['init', '-q', '-b', 'main'], TO)
for (const e of readdirSync(TO)) if (e !== '.git') rmSync(join(TO, e), { recursive: true, force: true })
for (const e of readdirSync(src)) cpSync(join(src, e), join(TO, e), { recursive: true })
rmSync(stage, { recursive: true, force: true })

// 5. one commit under the public author
git(['add', '-A'], TO)
if (!git(['status', '--porcelain'], TO)) {
  console.log(`✓ ${TO} is already up to date`)
} else {
  execFileSync('git', ['-c', `user.name=${NAME}`, '-c', `user.email=${EMAIL}`, 'commit', '-q', '-m', MESSAGE], { cwd: TO })
  console.log(`✓ committed in ${TO}: ${git(['log', '-1', '--format=%h %an <%ae> %s'], TO)}`)
}
const files = git(['ls-files'], TO).split('\n').length
console.log(`  ${files} files · module ${MODULE} · site ${PAGES}`)
try {
  execFileSync('go', ['build', './...'], { cwd: TO, stdio: 'pipe' })
  console.log('  go build ./... ok')
} catch (e) {
  console.error('  go build ./... failed:\n' + String(e.stderr || e.message).slice(0, 2000))
  process.exitCode = 1
}
if (!git(['remote'], TO)) console.log(`\nnext: git -C ${TO} remote add origin <github url> && git -C ${TO} push -u origin main`)
