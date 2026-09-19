// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Research experiment: real SDK source components + unmodified http_forwarder.
// Optional Python SDK run installs a local probe into a generated fixture only.
// Does not implement RC or contact Datadog.
'use strict'

const assert = require('node:assert/strict')
const http = require('node:http')
const fs = require('node:fs')
const os = require('node:os')
const path = require('node:path')
const vm = require('node:vm')
const { randomUUID } = require('node:crypto')
const { createRequire } = require('node:module')
const { spawn, execFileSync } = require('node:child_process')
const { once } = require('node:events')

const sdk = process.env.DD_TRACE_JS_SOURCE || path.join(os.homedir(), 'dd/dd-trace-js')
const binary = process.env.FORWARDER_BINARY || path.resolve(__dirname, '../../../shared/forwarder/forwarder')
const pinned = 'd62655e12494634bb53f8c0cd440f087b004ca63'
assert.equal(execFileSync('git', ['-C', sdk, 'rev-parse', 'HEAD'], { encoding: 'utf8' }).trim(), pinned)

function source (relative, stubs = {}) {
  const file = path.join(sdk, 'packages/dd-trace/src', relative)
  const module = { exports: {} }
  const nativeRequire = createRequire(file)
  const req = name => Object.hasOwn(stubs, name) ? stubs[name] : nativeRequire(name)
  vm.runInThisContext(`(function(require,module,exports){${fs.readFileSync(file, 'utf8')}\n})`, { filename: file })(req, module, module.exports)
  return module.exports
}

async function listen (handler) {
  const server = http.createServer(handler)
  server.listen(0, '127.0.0.1')
  await once(server, 'listening')
  return { server, url: `http://127.0.0.1:${server.address().port}` }
}

async function body (stream) {
  const chunks = []
  for await (const chunk of stream) chunks.push(Buffer.from(chunk))
  return Buffer.concat(chunks)
}

async function request (url, method = 'GET', headers = {}, payload = '') {
  return new Promise((resolve, reject) => {
    const req = http.request(url, { method, headers }, async res => {
      try { resolve({ status: res.statusCode, headers: res.headers, body: await body(res) }) } catch (err) { reject(err) }
    })
    req.on('error', reject)
    req.end(payload)
  })
}

async function startForwarder (target, dir) {
  const reservation = await listen((_req, res) => res.end())
  const port = reservation.server.address().port
  await new Promise(resolve => reservation.server.close(resolve))
  const config = path.join(dir, `${port}.yaml`)
  fs.writeFileSync(config, `ingress:\n  endpoint: 127.0.0.1:${port}\n  compression_algorithms: []\negress:\n  endpoint: ${target}/api/v2/debugger\n  headers:\n    DD-API-KEY: research-only\n  timeout: 5s\n`)
  const child = spawn(binary, ['--config', config], { stdio: ['ignore', 'pipe', 'pipe'] })
  let output = ''
  child.stdout.on('data', chunk => { output += chunk })
  child.stderr.on('data', chunk => { output += chunk })
  for (let n = 0; n < 100; n++) {
    if (output.includes('READY')) return { child, url: `http://127.0.0.1:${port}` }
    if (child.exitCode !== null) throw new Error(`Forwarder exited: ${output}`)
    await new Promise(resolve => setTimeout(resolve, 50))
  }
  child.kill()
  throw new Error(`Forwarder did not start: ${output}`)
}

// Deliberately bounded proposal model: snapshots/diagnostics/symbol upload routes,
// metadata and enablement. A real extension must reuse Agent/container tag/RC
// implementations. No fake RC capability is advertised.
function adapter (intake, state) {
  const routes = {
    '/debugger/v1/input': '/api/v2/logs',
    '/debugger/v2/input': '/api/v2/debugger',
    '/debugger/v1/diagnostics': '/api/v2/debugger',
    '/symdb/v1/input': '/api/v2/debugger',
  }
  return async (req, res) => {
    try {
      const url = new URL(req.url, 'http://adapter')
      if (url.pathname === '/info') {
        res.setHeader('Content-Type', 'application/json')
        return res.end(JSON.stringify({ endpoints: state.enabled ? Object.keys(routes) : [] }))
      }
      if (!state.enabled || !routes[url.pathname]) { res.writeHead(404); return res.end('disabled or unsupported') }
      const symbols = url.pathname.startsWith('/symdb/')
      const tags = ['host:research-host', 'default_env:research', 'agent_version:prototype', req.headers['x-datadog-additional-tags'], url.searchParams.get('ddtags')].filter(Boolean).join(',')
      const headers = { ...req.headers, 'dd-api-key': 'research-only', 'dd-request-id': randomUUID(), 'dd-evp-origin': symbols ? 'agent-symdb' : 'agent-debugger' }
      delete headers.host
      if (symbols) headers['x-datadog-additional-tags'] = tags
      else url.searchParams.set('ddtags', tags.slice(0, 4001))
      const result = await request(intake + routes[url.pathname] + url.search, req.method, headers, await body(req))
      res.writeHead(result.status, result.headers)
      res.end(result.body)
    } catch (err) { res.writeHead(502); res.end(err.message) }
  }
}

async function uploadUsingSDKComponents (url) {
  const outcomes = []
  const errors = []
  const log = { debug () {}, warn () {}, error (...args) { errors.push(args) } }
  const config = {
    url, agentless: false, service: 'live-debugging-research', env: 'research',
    runtimeId: 'research-runtime', inputPath: '/debugger/v2/input', version: '1',
    maxTotalPayloadSize: 1024 * 1024, queueMaxBytes: 1024 * 1024,
    dynamicInstrumentation: { uploadIntervalSeconds: 0.01 },
  }
  // This is an explicit transport stub, not dd-trace's common request transport.
  const transport = async (data, options, callback) => {
    try {
      const payload = typeof data === 'string' ? Buffer.from(data) : await body(data)
      const result = await request(new URL(options.path, options.url), options.method, options.headers, payload)
      outcomes.push({ path: options.path.split('?')[0], status: result.status })
      callback(result.status < 300 ? null : new Error(`HTTP ${result.status}`), result.body.toString(), result.status)
    } catch (err) { errors.push(err); callback(err) }
  }
  const guards = { eventDropped () {}, captureIncomplete () {} }
  const shared = {
    '../../exporters/common/request': transport,
    './config': config,
    './log': log,
    './guardrail-metrics': guards,
    '../guardrail-metrics': { DROPPED_REASON: { QUEUE_FULL: 0 }, INCOMPLETE_REASON: {}, EVENT_TYPE: { DIAGNOSTIC: 2 } },
    './snapshot-pruner': { pruneSnapshot () { throw new Error('Oversize path outside experiment') } },
  }
  const send = source('debugger/devtools_client/send.js', shared)
  const form = source('exporters/common/form-data.js', { '../../id': () => randomUUID() })
  const statuses = source('debugger/devtools_client/status.js', {
    ...shared,
    '../../exporters/common/form-data': form,
    '../../../../../vendor/dist/ttl-set': class extends Set { constructor () { super() } }, // TTL behavior untested
  })
  send('research snapshot', { name: 'fixture.js' }, undefined, { id: 'snapshot', probe: { id: 'probe', version: 1 } }, undefined, 0, 0)
  statuses.ackInstalled({ id: 'probe', version: 1 })
  for (let n = 0; n < 100; n++) {
    if (outcomes.length >= (config.inputPath === '/debugger/v2/input' ? 2 : 3)) return { outcomes, errors }
    await new Promise(resolve => setTimeout(resolve, 10))
  }
  throw new Error(`Timed out waiting for uploads: ${JSON.stringify({ outcomes, errors })}`)
}

async function installedPythonProbe (dir, url, captured) {
  if (!process.env.PYTHON_SDK_PATH) return
  const before = captured.length
  fs.writeFileSync(path.join(dir, 'workload.py'), 'def hot(value):\n    doubled = value * 2\n    return doubled\n')
  fs.writeFileSync(path.join(dir, 'app.py'), 'import ddtrace.auto\nimport ddtrace\nimport time\nimport workload\nprint("SDK_VERSION=" + ddtrace.__version__, flush=True)\nfor value in range(30):\n    workload.hot(value)\n    time.sleep(0.1)\ntime.sleep(1)\n')
  fs.writeFileSync(path.join(dir, 'probes.json'), JSON.stringify([{
    id: 'research-python-probe', version: 1, type: 'LOG_PROBE',
    where: { typeName: 'workload', methodName: 'hot' },
    captureSnapshot: true, template: 'research-python', segments: [{ str: 'research-python' }],
    sampling: { snapshotsPerSecond: 10 },
  }]))
  const child = spawn(process.env.PYTHON_BINARY || 'python3', [path.join(dir, 'app.py')], {
    cwd: dir, stdio: ['ignore', 'pipe', 'pipe'],
    env: { ...process.env, PYTHONPATH: process.env.PYTHON_SDK_PATH, PYTHONDONTWRITEBYTECODE: '1',
      DD_DYNAMIC_INSTRUMENTATION_ENABLED: 'true', DD_DYNAMIC_INSTRUMENTATION_PROBE_FILE: path.join(dir, 'probes.json'),
      DD_DYNAMIC_INSTRUMENTATION_UPLOAD_INTERVAL_SECONDS: '0.1', DD_TRACE_AGENT_URL: url,
      DD_SERVICE: 'live-debugging-python-research', DD_ENV: 'research', DD_VERSION: '1',
      DD_INSTRUMENTATION_TELEMETRY_ENABLED: 'false', DD_TRACE_ENABLED: 'false',
      DD_REMOTE_CONFIGURATION_ENABLED: 'false', DD_CODE_ORIGIN_FOR_SPANS_ENABLED: 'false',
      DD_SYMBOL_DATABASE_ENABLED: 'false', DD_API_KEY: '', DD_AGENTLESS_ENABLED: 'false',
    },
  })
  let output = ''
  child.stdout.on('data', data => { output += data })
  child.stderr.on('data', data => { output += data })
  const timeout = setTimeout(() => child.kill('SIGKILL'), 15000)
  const [code] = await once(child, 'exit')
  clearTimeout(timeout)
  assert.equal(code, 0, output)
  assert(output.includes('SDK_VERSION=4.13.0rc1'), output)
  const actual = captured.slice(before)
  const json = actual.filter(item => item.headers['content-type']?.startsWith('application/json')).flatMap(item => JSON.parse(item.body))
  const snapshots = json.filter(item => item.debugger?.snapshot?.probe?.id === 'research-python-probe')
  assert(snapshots.length > 0, `No real Python snapshots: ${output}`)
  assert(snapshots.some(item => item.debugger.snapshot.captures), 'Expected actual captured application state')
  assert(actual.some(item => item.body.includes('research-python-probe') && item.body.includes('INSTALLED')), 'Expected real installed diagnostic')
  console.log(`PASS: installed Python 4.13.0rc1 local probe captured ${snapshots.length} real snapshots and INSTALLED diagnostics through actual forwarder + adapter to mock intake (separate version from researched HEAD)`)
}

async function main () {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'live-debugging-'))
  const servers = []
  const children = []
  const captured = []
  try {
    const intake = await listen(async (req, res) => {
      captured.push({ path: req.url, headers: req.headers, body: await body(req) })
      const accepted = ['/api/v2/logs', '/api/v2/debugger'].includes(req.url.split('?')[0])
      res.writeHead(accepted ? 202 : 404)
      res.end(accepted ? 'accepted by mock only' : 'unknown mock intake route')
    })
    servers.push(intake.server)
    const direct = await startForwarder(intake.url, dir)
    children.push(direct.child)
    const rejected = await uploadUsingSDKComponents(direct.url)
    assert.equal(rejected.outcomes.length, 3)
    assert(rejected.outcomes.every(item => item.status === 404))
    assert.deepEqual(new Set(rejected.outcomes.map(item => item.path)), new Set(['/debugger/v2/input', '/debugger/v1/diagnostics']))
    assert(captured.every(item => item.headers['dd-api-key'] === 'research-only'))
    assert(captured.every(item => item.headers['dd-evp-origin'] === undefined))
    console.log('PASS: current forwarder ignores endpoint path; real JS send component retries v2 404 on diagnostics; all three requests reach mock with wrong paths')

    captured.length = 0
    const state = { enabled: true }
    const bridge = await listen(adapter(intake.url, state))
    servers.push(bridge.server)
    const mapped = await startForwarder(bridge.url, dir)
    children.push(mapped.child)
    const accepted = await uploadUsingSDKComponents(mapped.url)
    assert(accepted.outcomes.every(item => item.status === 202))
    assert.equal(accepted.errors.length, 0)
    assert.equal(captured.length, 2)
    assert(captured.every(item => item.path.startsWith('/api/v2/debugger?')))
    assert(captured.every(item => item.headers['dd-evp-origin'] === 'agent-debugger'))
    assert.equal(new Set(captured.map(item => item.headers['dd-request-id'])).size, 2)
    const snapshot = captured.find(item => item.headers['content-type'].startsWith('application/json'))
    assert.equal(JSON.parse(snapshot.body)[0].debugger.snapshot.probe.id, 'probe')
    assert(new URL(snapshot.path, intake.url).searchParams.get('ddtags').includes('env:research'))
    const diagnostic = captured.find(item => item.headers['content-type'].startsWith('multipart/form-data'))
    assert(diagnostic.body.includes('name="event"; filename="event.json"'))
    assert(diagnostic.body.includes('"status":"INSTALLED"'))
    console.log('PASS: adapter maps actual JS snapshot and multipart diagnostic uploads; preserves payloads, adds metadata, origin and distinct request IDs')

    const symbolPayload = Buffer.from([0, 255, 13, 10, 1])
    assert.equal((await request(mapped.url + '/symdb/v1/input?ddtags=version:1', 'POST', { 'Content-Type': 'multipart/form-data; boundary=research', 'X-Datadog-Additional-Tags': 'runtime_id:fixture' }, symbolPayload)).status, 202)
    const symbol = captured.at(-1)
    assert.deepEqual(symbol.body, symbolPayload)
    assert.equal(symbol.headers['dd-evp-origin'], 'agent-symdb')
    assert(symbol.headers['x-datadog-additional-tags'].includes('runtime_id:fixture'))
    assert.equal((await request(mapped.url + '/debugger/v1/input', 'POST', { 'Content-Type': 'application/json' }, '[]')).status, 202)
    assert(captured.at(-1).path.startsWith('/api/v2/logs?'))
    const info = JSON.parse((await request(mapped.url + '/info')).body)
    assert.equal(info.endpoints.length, 4)
    assert(!info.endpoints.includes('/v0.7/config'))
    console.log('PASS: synthetic legacy and binary symbol bodies map correctly; discovery advertises four implemented paths and no RC')

    await installedPythonProbe(dir, mapped.url, captured)

    state.enabled = false
    const count = captured.length
    assert.deepEqual(JSON.parse((await request(mapped.url + '/info')).body).endpoints, [])
    for (const endpoint of [...info.endpoints, '/v0.7/config']) {
      assert.equal((await request(mapped.url + endpoint, 'POST', {}, '{}')).status, 404)
    }
    assert.equal(captured.length, count)
    console.log('PASS: disabled proposal model hides discovery and forwards no snapshot, diagnostic, symbol or RC requests')
    console.log('LIMIT: JS source component test; optional Python local probe only; no real Agent, signed RC, Java SDK or Datadog backend/UI validated')
  } finally {
    await Promise.all(children.map(child => { const stopped = once(child, 'exit'); child.kill('SIGTERM'); return stopped }))
    await Promise.all(servers.map(server => new Promise(resolve => server.close(resolve))))
    fs.rmSync(dir, { recursive: true, force: true })
  }
}

main().catch(err => { console.error(err); process.exitCode = 1 })
