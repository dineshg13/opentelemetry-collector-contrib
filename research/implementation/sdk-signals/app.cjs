// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0
'use strict'
const tracer = require('dd-trace').init({
  profiling: false,
  runtimeMetrics: false,
  remoteConfig: { enabled: false },
  telemetry: { enabled: false },
})
const { metrics, trace, context } = require('@opentelemetry/api')
const { logs } = require('@opentelemetry/api-logs')
const counter = metrics.getMeter('ddot-signals').createCounter('poc.sdk.counter')
tracer.trace('poc.sdk.operation', span => {
  span.setTag('poc.language', 'javascript')
  counter.add(3, { 'poc.language': 'javascript' })
  logs.getLogger('ddot-signals').emit({ body: 'ddot-signals-javascript', severityNumber: 9,
    attributes: { 'poc.language': 'javascript' } })
  const ids = trace.getSpan(context.active())?.spanContext()
  console.log(JSON.stringify({ language: 'javascript', trace_id: ids?.traceId, span_id: ids?.spanId }))
})
console.log(JSON.stringify({ logger_provider: logs.getLoggerProvider().constructor.name,
  meter_provider: metrics.getMeterProvider().constructor.name }))
logs.getLoggerProvider().forceFlush?.()
setTimeout(() => {
  console.log('javascript: executed trace, log and counter API calls')
  process.exit(0)
}, 3000)
