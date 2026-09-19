// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0
'use strict'

// Execute the pinned SDK's real writer classes without installing its runtime.
// Ancillary imports are explicit stubs; networking is performed by run.py.
// This validates request construction, not a full SDK integration.
const fs = require('node:fs')
const path = require('node:path')
const vm = require('node:vm')
const root = process.argv[2]
// run.py checks the repository commit before invoking this helper.
const commit = 'd62655e12494634bb53f8c0cd440f087b004ca63'
const writersDir = path.join(root, 'packages/dd-trace/src/llmobs/writers')
const constants = require(path.join(writersDir, '../constants/writers'))
const noop = () => {}
const telemetry = new Proxy({}, { get: () => noop })
const logger = { debug: noop, warn: noop, error: noop }
const cache = new Map()
globalThis[Symbol.for('dd-trace')] = { beforeExitHandlers: new Set() }

function load(name) {
  if (cache.has(name)) return cache.get(name)
  const filename = path.join(writersDir, name + '.js')
  const module = { exports: {} }
  function localRequire(id) {
    if (id.startsWith('node:')) return require(id)
    if (id === './base') return load('base')
    if (id === '../constants/writers') return constants
    if (id === '../constants/text') return { DROPPED_VALUE_TEXT: '[dropped]' }
    if (id === '../constants/tags') return { DROPPED_IO_COLLECTION_ERROR: 'dropped_io' }
    if (id === '../../log') return logger
    if (id === '../telemetry') return telemetry
    if (id === '../../config/helper') return { getEnvironmentVariable: () => undefined }
    if (id === '../../serverless') return { createServerlessDeliveryTracker: () => undefined }
    if (id === '../util') return { encodeUnicode: value => value } // ASCII fixtures only.
    if (id === './util') return { parseResponseAndLog: noop }
    if (id === '../../exporters/common/request') return () => { throw new Error('network unexpected') }
    if (id.endsWith('package.json')) return require(path.join(root, 'package.json'))
    throw new Error('Unexpected dependency: ' + id)
  }
  vm.runInThisContext(`(function(require,module,exports){${fs.readFileSync(filename, 'utf8')}\n})`,
    { filename })(localRequire, module, module.exports)
  cache.set(name, module.exports)
  return module.exports
}

const fixtures = {
  spans: { trace_id: '1234', span_id: '5678', parent_id: 'undefined', name: 'research.llm',
    start_ns: 1700000000000000000, duration: 1000000, status: 'ok',
    meta: { 'span.kind': 'llm', model_name: 'test-model', model_provider: 'custom',
      input: { messages: [{ role: 'user', content: 'hello' }] },
      output: { messages: [{ role: 'assistant', content: 'world' }] } },
    metrics: { input_tokens: 1, output_tokens: 1 }, tags: ['ml_app:research'] },
  evaluations: { join_on: { span: { span_id: '5678', trace_id: '1234' } }, label: 'quality', metric_type: 'score',
    score_value: 1, ml_app: 'research', timestamp_ms: 1700000000000, event_kind: 'evaluation' }
}
const result = { sdk_commit: commit, evidence: 'real SDK writer classes; ancillary stubs; synthetic events', requests: [] }
for (const [name, event] of Object.entries(fixtures)) {
  const Writer = load(name)
  const writer = new Writer({ url: new URL('http://127.0.0.1:8126'), site: 'datadoghq.com' })
  writer.setAgentless(false)
  const options = writer._getOptions()
  result.requests.push({ kind: name, path: options.path, headers: options.headers,
    body: writer._encode(writer.makePayload([event])) })
  clearInterval(writer._periodic)
}
process.stdout.write(JSON.stringify(result, null, 2) + '\n')
