// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0
'use strict'

const fs = require('node:fs')
const tracing = process.env.DD_TRACE_ENABLED === 'true'
if (tracing && process.env.OTEL_TRACES_EXPORTER !== 'otlp') {
  throw new Error('This application only permits OTLP trace export')
}
const tracer = require('dd-trace').init()
const version = require('dd-trace/package.json').version

function profileHotLoop () {
  let checksum = 0
  for (let i = 0; i < 200000; i++) checksum += Math.sqrt(i)
  return checksum
}

let checksum = 0
const timer = setInterval(() => {
  const work = () => { for (let i = 0; i < 30; i++) checksum += profileHotLoop() }
  if (tracing) tracer.trace('profile.hot_loop', work)
  else work()
}, 50)

function stop () {
  clearInterval(timer)
  console.log(JSON.stringify({ complete: true, checksum_positive: checksum > 0 }))
  // The profiler owns periodic uploads. Allow pending requests to finish naturally.
  setTimeout(() => process.exit(0), 1000)
}
process.once('SIGTERM', stop)
process.once('SIGINT', stop)
const duration = Number(process.env.WORKLOAD_SECONDS || 0)
if (duration) setTimeout(stop, duration * 1000)
fs.writeFileSync('/tmp/ready', '')
console.log(JSON.stringify({ language: 'node', sdk: version, profiling: process.env.DD_PROFILING_ENABLED,
  otlp_tracing: tracing }))
