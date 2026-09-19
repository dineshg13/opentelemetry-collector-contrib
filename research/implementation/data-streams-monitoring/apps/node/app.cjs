// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0
'use strict'
if (process.env.OTEL_TRACES_EXPORTER !== 'otlp' || process.env.DD_TRACE_AGENT_PROTOCOL_VERSION) {
  throw new Error('Enable OTLP and omit DD_TRACE_AGENT_PROTOCOL_VERSION')
}
const tracer = require('dd-trace').init()
if (tracer._tracer._exporter.constructor.name !== 'OtlpHttpTraceExporter') {
  throw new Error('Datadog SDK did not select the OTLP exporter')
}
async function main () {
  for (let i = 0; i < Number(process.env.WORKLOAD_ITERATIONS || 3); i++) {
    const carrier = {}
    tracer.trace('dsm.produce', span => {
      span.setTag('messaging.system', 'kafka')
      span.setTag('messaging.destination.name', 'poc-orders')
      span.setTag('messaging.operation.name', 'send')
      tracer.dataStreamsCheckpointer.setProduceCheckpoint('kafka', 'poc-orders', carrier)
    })
    await new Promise(resolve => setTimeout(resolve, 50))
    tracer.trace('dsm.consume', span => {
      span.setTag('messaging.system', 'kafka')
      span.setTag('messaging.destination.name', 'poc-orders')
      span.setTag('messaging.operation.name', 'process')
      tracer.dataStreamsCheckpointer.setConsumeCheckpoint('kafka', 'poc-orders', carrier)
      tracer.dataStreamsCheckpointer.trackTransaction('poc-transaction', 'consumed')
    })
    console.log(JSON.stringify({ sdk: 'dd-trace', version: require('dd-trace/package.json').version,
      sequence: i, propagation: carrier, workload: 'manual-in-process-checkpoints' }))
  }
  // Internal SDK flush hook only; encoding and sending remain entirely SDK-owned.
  tracer._tracer._dataStreamsProcessor.onInterval()
  await new Promise(resolve => setTimeout(resolve, 3000))
}
main().then(() => process.exit(0), error => { console.error(error); process.exit(1) })
