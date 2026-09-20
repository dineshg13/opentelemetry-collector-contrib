// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0
import datadog.trace.api.GlobalTracer;
import datadog.trace.api.Trace;
import datadog.trace.api.interceptor.MutableSpan;
import datadog.trace.api.interceptor.TraceInterceptor;
import java.util.Collection;

/** Uses the Datadog agent and public Datadog APIs, not a separate OpenTelemetry SDK. */
public class LlmWorkload {
  @Trace(operationName = "chat deterministic-poc", resourceName = "chat deterministic-poc")
  public static void chat() {
    System.out.println("{\"language\":\"java\",\"trace_id\":\"" + GlobalTracer.get().getTraceId()
        + "\",\"span_id\":\"" + GlobalTracer.get().getSpanId() + "\"}");
  }

  public static void main(String[] args) throws Exception {
    if (!"otlp".equals(System.getenv("OTEL_TRACES_EXPORTER"))) {
      throw new IllegalArgumentException("OTEL_TRACES_EXPORTER=otlp is required");
    }
    if (!GlobalTracer.get().addTraceInterceptor(new TraceInterceptor() {
      public int priority() { return 1001; }
      public Collection<? extends MutableSpan> onTraceComplete(Collection<? extends MutableSpan> spans) {
        for (MutableSpan span : spans) {
          if (!"chat deterministic-poc".contentEquals(span.getOperationName())) continue;
          span.setTag("gen_ai.operation.name", "chat");
          span.setTag("gen_ai.provider.name", "custom");
          span.setTag("gen_ai.request.model", "deterministic-poc");
          span.setTag("gen_ai.response.model", "deterministic-poc");
          span.setTag("gen_ai.conversation.id", "ddot-llm-java");
          span.setTag("gen_ai.input.messages", "[{\"role\":\"user\",\"content\":\"Return the word ready\"}]");
          span.setTag("gen_ai.output.messages", "[{\"role\":\"assistant\",\"content\":\"ready\"}]");
          span.setTag("poc.workload", "deterministic-manual-instrumentation");
          span.setTag("gen_ai.usage.input_tokens", 4);
          span.setTag("gen_ai.usage.output_tokens", 1);
        }
        return spans;
      }
    })) throw new IllegalStateException("Datadog tracer is not installed");
    int iterations = args.length == 0 ? 3 : Integer.parseInt(args[0]);
    for (int i = 0; i < iterations; i++) { chat(); Thread.sleep(1000); }
    Thread.sleep(3000);
  }
}
