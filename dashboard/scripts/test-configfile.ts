import assert from 'node:assert/strict'
import { parseConfig, writeConfig, newConfigVar, type ConfigFormat } from '../app/utils/configfile.ts'
import type { EnvVar } from '../app/utils/dotenv.ts'

const keys = (text: string, f: ConfigFormat) => parseConfig(text, f).entries.filter((e): e is EnvVar => e.kind === 'var').map(v => [v.key, v.value, v.on, v.note])
// edit the variable named key, return the file
function edit(text: string, f: ConfigFormat, key: string, change: Partial<EnvVar>) {
  const doc = parseConfig(text, f)
  const v = doc.entries.find((e): e is EnvVar => e.kind === 'var' && e.key === key)
  assert.ok(v, `${key} in ${f}`)
  Object.assign(v, change)
  return writeConfig(doc)
}

const samples: Record<ConfigFormat, string> = {
  env: 'A=1\n# off\n# B=2\n',
  properties: '# db\ndb.url = jdbc:x\n! old\n#db.user=sa\napp.name: Shop\n',
  ini: '; top\n[server]\nhost = 0.0.0.0 ; all\nport=80\n# debug = true\n[db]\nname = "shop"\n',
  toml: 'title = "x"\n[server]\nport = 8080\nhosts = [\n  "a",\n]\ndesc = """\nport = 1\n"""\n# old = 2\n',
  yaml: '# app\nserver:\n  # the port\n  port: 8080\n  host: "0.0.0.0" # all\n  # tls: true\nlog: |\n  level: x\ndb:\n  - name: a\n    url: u\n',
  json: '{\n  "name": "shop",\n  "server": {\n    "port": 80,\n    "tags": ["a", "b"],\n    "ssl": {}\n  },\n  "list": [\n    { "x": 1 },\n    "y"\n  ],\n  "debug": false\n}\n',
  xml: '<?xml version="1.0"?>\n<configuration>\n  <appSettings>\n    <!-- the key -->\n    <add key="ApiKey" value="abc" />\n    <!-- <add key="Old" value="1" /> -->\n  </appSettings>\n  <!--\n  <add key="Hidden" value="2" />\n  -->\n  <db>\n    <host>localhost</host>\n  </db>\n</configuration>\n'
}

// untouched files are written back byte for byte (also with CRLF)
for (const [f, text] of Object.entries(samples) as [ConfigFormat, string][]) {
  assert.equal(writeConfig(parseConfig(text, f)), text, f)
  const crlf = text.replace(/\n/g, '\r\n')
  assert.equal(writeConfig(parseConfig(crlf, f)), crlf, `${f} crlf`)
}

assert.deepEqual(keys(samples.properties, 'properties'), [['db.url', 'jdbc:x', true, 'db'], ['db.user', 'sa', false, 'old'], ['app.name', 'Shop', true, '']])
assert.deepEqual(keys(samples.ini, 'ini'), [['server.host', '0.0.0.0', true, ''], ['server.port', '80', true, ''], ['server.debug', 'true', false, ''], ['db.name', 'shop', true, '']])
assert.deepEqual(keys(samples.toml, 'toml'), [['title', 'x', true, ''], ['server.port', '8080', true, ''], ['server.old', '2', false, '']])
assert.deepEqual(keys(samples.yaml, 'yaml'), [['server.port', '8080', true, 'the port'], ['server.host', '0.0.0.0', true, ''], ['server.tls', 'true', false, ''], ['db.name', 'a', true, ''], ['db.url', 'u', true, '']])
assert.deepEqual(keys(samples.json, 'json'), [['name', 'shop', true, ''], ['server.port', '80', true, ''], ['debug', 'false', true, '']])
assert.deepEqual(keys(samples.xml, 'xml'), [['ApiKey', 'abc', true, 'the key'], ['Old', '1', false, ''], ['configuration.db.host', 'localhost', true, '']])

// a value changes in place, quotes and trailing comments kept
assert.equal(edit(samples.yaml, 'yaml', 'server.host', { value: '127.0.0.1' }).split('\n')[4], '  host: "127.0.0.1" # all')
assert.equal(edit(samples.ini, 'ini', 'server.host', { value: 'x' }).split('\n')[2], 'host = x ; all')
assert.equal(edit(samples.json, 'json', 'server.port', { value: '8080' }).split('\n')[3], '    "port": 8080,')
assert.equal(edit(samples.json, 'json', 'debug', { value: 'maybe' }).split('\n')[11], '  "debug": "maybe"')
assert.equal(edit(samples.xml, 'xml', 'configuration.db.host', { value: 'db' }).split('\n')[11], '    <host>db</host>')
assert.equal(edit(samples.properties, 'properties', 'app.name', { key: 'app.title' }).split('\n')[4], 'app.title: Shop')

// off = commented out in the format's own way, on again = uncommented; notes too
assert.equal(edit(samples.yaml, 'yaml', 'server.port', { on: false, note: 'off for now' }).split('\n').slice(2, 4).join('\n'), '  # off for now\n  # port: 8080')
assert.equal(edit(samples.yaml, 'yaml', 'server.tls', { on: true }).split('\n')[5], '  tls: true')
assert.equal(edit(samples.xml, 'xml', 'ApiKey', { on: false }).split('\n')[4], '    <!-- <add key="ApiKey" value="abc" /> -->')
assert.equal(edit(samples.xml, 'xml', 'Old', { on: true }).split('\n')[5], '    <add key="Old" value="1" />')
assert.equal(edit(samples.ini, 'ini', 'server.port', { on: false }).split('\n')[3], '# port=80')

// a new properties key
{
  const doc = parseConfig('a=1\n', 'properties')
  doc.entries.push(Object.assign(newConfigVar('properties'), { key: 'b', value: '2' }))
  assert.equal(writeConfig(doc), 'a=1\nb=2\n')
}

console.log('configfile ok')
