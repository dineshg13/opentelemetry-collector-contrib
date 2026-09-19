// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0
// Executes pinned SDK modules with explicitly stubbed network/encoder/base classes.
// This is a source-component test, NOT a full JavaScript tracer/runtime test.
'use strict'

const assert = require('node:assert/strict')
const fs = require('node:fs')
const path = require('node:path')
const vm = require('node:vm')
const source = process.argv[2] || '/home/bits/dd/dd-trace-js'
const base = path.join(source, 'packages/dd-trace/src/ci-visibility/exporters')

function load (filename, dependencies) {
  const module = { exports: {} }
  vm.runInNewContext(fs.readFileSync(filename, 'utf8'), {
    module, exports: module.exports, AbortController, Date, Buffer,
    require (name) {
      assert.ok(Object.hasOwn(dependencies, name), `Unspecified stub: ${name}`)
      return dependencies[name]
    },
  }, { filename })
  return module.exports
}

class StubWriter {
  constructor (options) { this.options = options }
}
class StubExporter {
  constructor () { this._url = new URL('http://127.0.0.1:8126') }
  _resolveCanUseCiVisProtocol (value) { this.protocol = value }
  exportUncodedTraces () {}
  exportUncodedCoverages () {}
  resetUncodedTraces () {}
}

const cases = [
  { endpoints: [], protocol: false, gzip: false },
  { endpoints: ['/evp_proxy/v2/'], protocol: true, gzip: false, prefix: '/evp_proxy/v2' },
  { endpoints: ['/evp_proxy/v3/'], protocol: true, gzip: false, prefix: '/evp_proxy/v2' },
  { endpoints: ['/evp_proxy/v2/', '/evp_proxy/v4/'], protocol: true, gzip: true, prefix: '/evp_proxy/v4' },
]
for (const test of cases) {
  const Exporter = load(path.join(base, 'agent-proxy/index.js'), {
    '../../../exporters/agent/writer': StubWriter,
    '../agentless/writer': StubWriter,
    '../agentless/coverage-writer': StubWriter,
    '../ci-visibility-exporter': StubExporter,
    '../request': () => {},
    '../../../agent/info': { fetchAgentInfo: (url, callback) => callback(null, { endpoints: test.endpoints }) },
    '../../../debugger/constants': { DEBUGGER_INPUT_V1: '/debugger/v1/input' },
  })
  const exporter = new Exporter({ testOptimization: {} })
  assert.equal(exporter.protocol, test.protocol)
  assert.equal(exporter._isGzipCompatible, test.gzip)
  assert.equal(exporter.evpProxyPrefix, test.prefix)
}

const captured = []
const telemetry = new Proxy({}, { get: (_, key) => key.endsWith('Metric') ? () => {} : key })
const common = {
  '../../../config': () => ({ DD_API_KEY: 'sdk-agentless-key' }),
  '../../../evp_proxy/constants': { EVP_SUBDOMAIN_HEADER_NAME: 'X-Datadog-EVP-Subdomain' },
  '../../../evp_proxy/path': { joinEVPProxyPath: (prefix, suffix) => prefix + suffix },
  '../../../exporters/common/util': { safeJSONStringify: JSON.stringify },
  '../../../log': { debug () {}, error () {} },
  '../../../ci-visibility/telemetry': telemetry,
  '../../../exporters/common/writer': StubWriter,
  '../agents': { getAgent: () => ({}) },
  '../request': (data, options, callback) => { captured.push({ data, options }); callback(null, '{}', 200) },
  './request-tracker': class {
    send (request, data, options, callback) { request(data, options, callback) }
  },
}
const EventWriter = load(path.join(base, 'agentless/writer.js'), {
  ...common, '../../../encode/agentless-ci-visibility': { AgentlessCiVisibilityEncoder: class {} },
})
const CoverageWriter = load(path.join(base, 'agentless/coverage-writer.js'), {
  ...common, '../../../encode/coverage-ci-visibility': { CoverageCIVisibilityEncoder: class {} },
})
new EventWriter({ url: 'http://fixture', evpProxyPrefix: '/evp_proxy/v4' })
  ._sendPayload(Buffer.from('opaque-msgpack-fixture'), null, () => {})
new CoverageWriter({ url: 'http://fixture', evpProxyPrefix: '/evp_proxy/v2' })
  ._sendPayload({ getHeaders: () => ({ 'Content-Type': 'multipart/form-data; boundary=fixture' }), size: () => 1 }, null, () => {})
assert.equal(captured[0].options.path, '/evp_proxy/v4/api/v2/citestcycle')
assert.equal(captured[0].options.headers['Content-Type'], 'application/msgpack')
assert.equal(captured[0].options.headers['X-Datadog-EVP-Subdomain'], 'citestcycle-intake')
assert.equal(captured[1].options.path, '/evp_proxy/v2/api/v2/citestcov')
assert.equal(captured[1].options.headers['X-Datadog-EVP-Subdomain'], 'citestcov-intake')
assert.ok(captured.every(({ options }) => !Object.hasOwn(options.headers, 'dd-api-key')))
console.log(JSON.stringify({ discovery_cases: cases.length, source_writer_cases: captured.length,
  network_and_encoders_stubbed: true, full_sdk_runtime: false }, null, 2))
