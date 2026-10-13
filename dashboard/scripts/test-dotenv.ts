import assert from 'node:assert/strict'
import { parseEnv, writeEnv, newEnvVar, type EnvVar } from '../app/utils/dotenv.ts'

const vars = (text: string) => parseEnv(text).entries.filter((e): e is EnvVar => e.kind === 'var')

// untouched: written back byte for byte (CRLF, no final newline, odd spacing)
for (const text of ['A=1\n# note\nB = "x y" # why\n\n#off\n# C=3\n', 'A=1\r\nB=2', '', 'export K=\'v\'\nM="a\nb"\n']) {
  assert.equal(writeEnv(parseEnv(text)), text)
}

// variables, quotes, notes and turned-off lines
{
  const v = vars('# the port\nPORT=3000 # dev\n# OLD=1\nS="a b"\nexport E=\'x\'\nM="l1\nl2"\n')
  assert.deepEqual(v.map(x => [x.key, x.value, x.on, x.note]), [
    ['PORT', '3000', true, 'the port'], ['OLD', '1', false, ''], ['S', 'a b', true, ''], ['E', 'x', true, ''], ['M', 'l1\nl2', true, '']
  ])
  assert.equal(v[0]!.inline, ' # dev')
  assert.ok(v[3]!.exported)
}

// edits: turn off, change a value (quoted when needed), add a note, add a var
{
  const doc = parseEnv('A=1\nB=2\n\n# keep me\n')
  const [a, b] = doc.entries.filter((e): e is EnvVar => e.kind === 'var')
  a!.on = false
  b!.value = 'has space'
  b!.note = 'why\nand more'
  doc.entries.push(Object.assign(newEnvVar('NEW', 'x"y'), {}))
  assert.equal(writeEnv(doc), '# A=1\n# why\n# and more\nB="has space"\n\n# keep me\nNEW=\'x"y\'\n')
}

// turning a commented-out variable back on
{
  const doc = parseEnv('# DEBUG=true\n')
  ;(doc.entries[0] as EnvVar).on = true
  assert.equal(writeEnv(doc), 'DEBUG=true\n')
}

console.log('dotenv ok')
