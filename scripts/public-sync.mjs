#!/usr/bin/env node
// Brings the public repo up to this one (scripts/export-public.mjs: no internal
// notes, the public module, checked for leaks), checks it (gofmt, build, vet,
// tests, i18n), then commits under the no-reply author:
//
//   node scripts/public-sync.mjs                     # amend the public repo's last commit
//   node scripts/public-sync.mjs --new ["message"]   # a commit of its own on main
//   … --push                                         # then push (amend: --force-with-lease)
//   … --to ../agent-office                           # the public repo (default: next to this one)
//
// Any step failing leaves the public repo as it was.
import { execFileSync } from 'node:child_process'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const argv = process.argv.slice(2)
const flag = (name) => argv.includes(`--${name}`)
const value = (name) => {
  const i = argv.indexOf(`--${name}`)
  return i >= 0 && argv[i + 1] && !argv[i + 1].startsWith('--') ? argv[i + 1] : undefined
}
const gitIn = (cwd, args) => execFileSync('git', args, { cwd, stdio: ['ignore', 'pipe', 'pipe'] }).toString().trim()

const here = dirname(fileURLToPath(import.meta.url))
const PRIV = dirname(gitIn(here, ['rev-parse', '--path-format=absolute', '--git-common-dir'])) // the main checkout, a worktree too
const PUB = resolve(value('to') ?? join(dirname(PRIV), 'agent-office'))
const NAME = 'tung1998'
const EMAIL = 'tung1998@users.noreply.github.com'
const NEW = flag('new')
const MESSAGE = value('new')
const PUSH = flag('push')

const run = (cmd, args, cwd, quiet = false) => {
  console.log(`$ ${cmd} ${args.join(' ')}`)
  const out = execFileSync(cmd, args, { cwd, stdio: ['ignore', 'pipe', 'pipe'] }).toString().trim()
  if (out && !quiet) console.log(out)
  return out
}
const git = (args, quiet) => run('git', args, PUB, quiet)

if (git(['status', '--porcelain'])) {
  console.error(`✗ ${PUB} has uncommitted changes`)
  process.exit(1)
}
if (git(['rev-parse', '--abbrev-ref', 'HEAD']) !== 'main') {
  console.error(`✗ ${PUB} is not on main`)
  process.exit(1)
}
const before = git(['rev-parse', 'HEAD'])

try {
  // 1. the clean copy → one new commit
  run('node', ['scripts/export-public.mjs', '--to', PUB, ...(MESSAGE ? ['--message', MESSAGE] : [])], PRIV)
  if (git(['rev-parse', 'HEAD']) === before) {
    console.log('✓ the public repo is already up to date')
    process.exit(0)
  }
  console.log(git(['show', '--stat', '--format=%h %s', 'HEAD'], true).split('\n').slice(0, 80).join('\n'))

  // 2. the module rename puts imports out of order: gofmt sorts them (no code changes)
  const bad = run('gofmt', ['-l', '.'], PUB)
  if (bad) run('gofmt', ['-w', ...bad.split('\n')], PUB)
  if (run('gofmt', ['-l', '.'], PUB)) throw new Error('gofmt still lists files')

  // 3. checks
  run('go', ['build', './...'], PUB)
  run('go', ['vet', './...'], PUB)
  run('go', ['test', './internal/channels/', './internal/api/', './internal/auth/', './internal/storage/...', './internal/trigger/'], PUB)
  run('node', ['dashboard/scripts/check-i18n.mjs'], PUB)

  // 4. --new: gofmt's fixes go into the new commit; else all into the last public one
  if (!NEW) git(['reset', '--soft', 'HEAD~1'])
  git(['add', '-A'])
  run('git', ['-c', `user.name=${NAME}`, '-c', `user.email=${EMAIL}`, 'commit', '--amend', '--no-edit', '--reset-author', '-q'], PUB)
  git(['log', '-1', '--format=%h %an <%ae> %s'])
} catch (e) {
  console.error('✗ ' + String(e.stderr || e.stdout || e.message).slice(0, 3000))
  execFileSync('git', ['reset', '--hard', '-q', before], { cwd: PUB })
  execFileSync('git', ['clean', '-fdq'], { cwd: PUB })
  console.error(`the public repo is back at ${before.slice(0, 7)}`)
  process.exit(1)
}

// 5. push: a new commit as is; an amended one replaces the remote's (refused if it moved meanwhile)
if (PUSH) git(['push', ...(NEW ? [] : ['--force-with-lease']), 'origin', 'main'])
git(['status', '--short', '--branch'])
