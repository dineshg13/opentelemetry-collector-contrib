// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0
import io.opentelemetry.api.GlobalOpenTelemetry;
import io.opentelemetry.api.common.Attributes;
import io.opentelemetry.api.common.AttributeKey;
import io.opentelemetry.api.logs.Severity;
import io.opentelemetry.api.trace.Span;
import io.opentelemetry.context.Scope;

/** Only the OpenTelemetry API is added; the Datadog agent supplies its implementations. */
public class Signals {
  public static void main(String[] args) throws Exception {
    Span span = GlobalOpenTelemetry.getTracer("ddot-signals").spanBuilder("poc.sdk.operation").startSpan();
    try (Scope scope = span.makeCurrent()) {
      span.setAttribute("poc.language", "java");
      GlobalOpenTelemetry.getMeter("ddot-signals").counterBuilder("poc.sdk.counter").build()
          .add(3, Attributes.of(AttributeKey.stringKey("poc.language"), "java"));
      GlobalOpenTelemetry.get().getLogsBridge().get("ddot-signals").logRecordBuilder()
          .setBody("ddot-signals-java").setSeverity(Severity.INFO)
          .setAttribute(AttributeKey.stringKey("poc.language"), "java").emit();
      System.out.println("{\"language\":\"java\",\"trace_id\":\"" + span.getSpanContext().getTraceId()
          + "\",\"span_id\":\"" + span.getSpanContext().getSpanId() + "\"}");
    } finally {
      span.end();
    }
    Thread.sleep(4000);
    System.out.println("java: executed trace, log and counter API calls");
  }
}
