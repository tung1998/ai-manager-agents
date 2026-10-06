#!/usr/bin/env node
// Cherry-picks commits of this repo into the public one, which has its own
// history (export-public.mjs would overwrite it): each commit becomes a patch
// without the internal notes, with the public module path and the no-reply
// author, checked for leaks, then applied with `git am -3`; build and vet run
// after. Any step failing leaves the public repo as it was.
//
//   node scripts/public-pick.mjs <commit>...          # e.g. f5bea5a, or a range a1b2c3..HEAD
//   … --push                                          # then push main
//   … --to ../agent-office                            # the public repo (default: next to this one)
import { execFileSync } from 'node:child_process'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const argv = process.argv.slice(2)
const value = (name) => {
  const i = argv.indexOf(`--${name}`)
  return i >= 0 ? argv[i + 1] : undefined
}
const PUSH = argv.includes('--push')
const to = value('to')
const revs = argv.filter((a, i) => !a.startsWith('--') && argv[i - 1] !== '--to')
if (!revs.length) {
  console.error('usage: node scripts/public-pick.mjs <commit|range>... [--push] [--to dir]')
  process.exit(1)
}

const sh = (cwd, args, input) => execFileSync('git', args, { cwd, input, stdio: [input ? 'pipe' : 'ignore', 'pipe', 'pipe'] }).toString().trim()
const here = dirname(fileURLToPath(import.meta.url))
const PRIV = dirname(sh(here, ['rev-parse', '--path-format=absolute', '--git-common-dir']))
const PUB = resolve(to ?? join(dirname(PRIV), 'agent-office'))

// keep in step with export-public.mjs
const PRIVATE_MODULE = 'bitbucket.org/senprints/agent-office'
const MODULE = 'github.com/tung1998/agent-office'
const PRIVATE_PAGES = 'https://tung1998.github.io/ai-manager-agents'
const PAGES = 'https://tung1998.github.io/agent-office'
const AUTHOR = 'tung1998 <tung1998@users.noreply.github.com>'
const EXCLUDE = ['docs/PLAN.md', 'docs/DECISIONS.md', 'docs/CLI.md', 'docs/superpowers', 'scripts/export-public.mjs', 'scripts/public-sync.mjs', 'scripts/public-pick.mjs']
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

// the commits, oldest first
const commits = revs.flatMap(r => r.includes('..')
  ? sh(PRIV, ['rev-list', '--reverse', '--no-merges', r]).split('\n').filter(Boolean)
  : [sh(PRIV, ['rev-parse', '--verify', `${r}^{commit}`])])

// one cleaned patch per commit
const patches = []
const hits = []
for (const c of commits) {
  const raw = sh(PRIV, ['format-patch', '-1', '--stdout', '--no-encode-email-headers', '--no-signature', c, '--', '.', ...EXCLUDE.map(p => `:(exclude)${p}`)])
  const subject = sh(PRIV, ['log', '-1', '--format=%h %s', c])
  if (!raw.includes('\ndiff --git ')) {
    console.log(`- ${subject}  (only internal files, skipped)`)
    continue
  }
  const patch = raw.replace(/^From: .*$/m, `From: ${AUTHOR}`)
    .split(PRIVATE_MODULE).join(MODULE).split(PRIVATE_PAGES).join(PAGES)
  // the message and the lines the commit adds; the context lines are already public
  patch.split('\n').forEach((line) => {
    if (line.startsWith('From ') || line.startsWith('-') || line.startsWith(' ')) return
    for (const [re, what] of FORBIDDEN) if (re.test(line)) hits.push(`${subject.slice(0, 7)}  [${what}]  ${line.trim().slice(0, 120)}`)
  })
  patches.push({ subject, patch: patch + '\n' })
}
if (hits.length) {
  console.error(`✗ ${hits.length} line(s) must not go public:\n  ` + hits.join('\n  '))
  process.exit(1)
}
if (!patches.length) {
  console.log('✓ nothing to pick')
  process.exit(0)
}

if (sh(PUB, ['status', '--porcelain'])) {
  console.error(`✗ ${PUB} has uncommitted changes`)
  process.exit(1)
}
if (sh(PUB, ['rev-parse', '--abbrev-ref', 'HEAD']) !== 'main') {
  console.error(`✗ ${PUB} is not on main`)
  process.exit(1)
}
const before = sh(PUB, ['rev-parse', 'HEAD'])
const name = 'tung1998', email = 'tung1998@users.noreply.github.com'

try {
  for (const { subject, patch } of patches) {
    console.log(`+ ${subject}`)
    sh(PUB, ['-c', `user.name=${name}`, '-c', `user.email=${email}`, 'am', '-3', '--committer-date-is-author-date', '--quiet'], patch)
  }
  // the module rename can put imports out of order
  const bad = execFileSync('gofmt', ['-l', '.'], { cwd: PUB }).toString().trim()
  if (bad) throw new Error(`gofmt lists:\n${bad}`)
  execFileSync('go', ['build', './...'], { cwd: PUB, stdio: 'inherit' })
  execFileSync('go', ['vet', './...'], { cwd: PUB, stdio: 'inherit' })
} catch (e) {
  console.error('✗ ' + String(e.stderr || e.stdout || e.message).slice(0, 3000))
  try { sh(PUB, ['am', '--abort']) } catch { /* no am in progress */ }
  sh(PUB, ['reset', '--hard', '-q', before])
  console.error(`the public repo is back at ${before.slice(0, 7)}`)
  process.exit(1)
}

console.log(sh(PUB, ['log', '--format=%h %an %s', `${before}..HEAD`]))
if (PUSH) execFileSync('git', ['push', 'origin', 'main'], { cwd: PUB, stdio: 'inherit' })
