// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0
'use strict'

// Load the Datadog SDK before the instrumented core HTTP module.
if (process.env.OTEL_TRACES_EXPORTER !== 'otlp' || !process.env.OTEL_EXPORTER_OTLP_TRACES_ENDPOINT) {
  throw new Error('Set OTEL_TRACES_EXPORTER=otlp and OTEL_EXPORTER_OTLP_TRACES_ENDPOINT')
}
if (process.env.DD_TRACE_AGENT_PROTOCOL_VERSION) throw new Error('Native protocol override disables OTLP')
const tracer = require(process.env.DDOT_JS_SDK || 'dd-trace').init({
  service: 'ddot-appsec-node',
  env: 'ddot-poc',
  appsec: { enabled: process.env.DD_APPSEC_ENABLED === 'true' },
  profiling: false,
  runtimeMetrics: false,
  remoteConfig: { enabled: false },
  telemetry: { enabled: false },
})
if (tracer._tracer._exporter.constructor.name !== 'OtlpHttpTraceExporter') {
  throw new Error('The Datadog SDK did not select OTLP export')
}
const http = require('node:http')
const server = http.createServer((request, response) => {
  response.writeHead(200, { 'Content-Type': 'application/json' })
  response.end(JSON.stringify({ message: 'Harmless local WAF fixture' }))
})

function request (userAgent) {
  return new Promise((resolve, reject) => {
    http.get({ hostname: '127.0.0.1', port: server.address().port, path: '/fixture',
      headers: { 'User-Agent': userAgent } }, response => {
      response.resume()
      response.on('end', () => {
        console.log(JSON.stringify({ fixture: userAgent, status: response.statusCode,
          appsec_enabled: process.env.DD_APPSEC_ENABLED }))
        resolve()
      })
    }).on('error', reject)
  })
}

server.listen(0, '127.0.0.1', async () => {
  try {
    await request('ddot-benign-fixture')
    await request('dd-test-scanner-log')
    server.close(() => setTimeout(() => process.exit(0), 3000))
  } catch (error) {
    console.error(error)
    process.exit(1)
  }
})
