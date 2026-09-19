// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0
'use strict'

// This is the Datadog tracer's own OTLP writer, not a second OTel SDK.
if (process.env.OTEL_TRACES_EXPORTER !== 'otlp' || !process.env.OTEL_EXPORTER_OTLP_TRACES_ENDPOINT) {
  throw new Error('Set OTEL_TRACES_EXPORTER=otlp and OTEL_EXPORTER_OTLP_TRACES_ENDPOINT')
}
if (process.env.DD_TRACE_AGENT_PROTOCOL_VERSION) {
  throw new Error('DD_TRACE_AGENT_PROTOCOL_VERSION disables OTLP export')
}
const tracer = require(process.env.DDOT_JS_SDK || 'dd-trace').init({
  service: 'ddot-llm-node',
  env: 'ddot-poc',
  llmobs: { enabled: false },
  profiling: false,
  runtimeMetrics: false,
  remoteConfig: { enabled: false },
  telemetry: { enabled: false },
})
if (tracer._tracer._exporter.constructor.name !== 'OtlpHttpTraceExporter') {
  throw new Error('This dd-trace runtime did not select the OTLP exporter')
}
tracer.trace('chat deterministic-poc', span => {
  span.setTag('gen_ai.operation.name', 'chat')
  span.setTag('gen_ai.provider.name', 'custom')
  span.setTag('gen_ai.request.model', 'deterministic-poc')
  span.setTag('gen_ai.response.model', 'deterministic-poc')
  span.setTag('gen_ai.conversation.id', 'ddot-llm-node')
  span.setTag('gen_ai.input.messages', JSON.stringify([{ role: 'user', content: 'Return the word ready' }]))
  span.setTag('gen_ai.output.messages', JSON.stringify([{ role: 'assistant', content: 'ready' }]))
  span.setTag('gen_ai.usage.input_tokens', 4)
  span.setTag('gen_ai.usage.output_tokens', 1)
  span.setTag('poc.workload', 'deterministic-manual-instrumentation')
  console.log(JSON.stringify({ language: 'javascript', trace_id: span.context().toTraceId(), span_id: span.context().toSpanId() }))
})
// The SDK exporter sends immediately; give its asynchronous request time to complete.
setTimeout(() => process.exit(0), 3000)
