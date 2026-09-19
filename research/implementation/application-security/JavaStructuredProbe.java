// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0
import datadog.trace.api.GlobalTracer;
import datadog.trace.api.Trace;
import datadog.trace.api.interceptor.MutableSpan;
import datadog.trace.api.interceptor.TraceInterceptor;
import java.util.Collection;
import java.util.Map;

/** Synthetic encoder regression probe only; this does not generate a real WAF or IAST event. */
public class JavaStructuredProbe {
  @Trace(operationName = "appsec-structured-contract", resourceName = "appsec-structured-contract")
  public static void work() {}

  public static void main(String[] args) throws Exception {
    if (!GlobalTracer.get().addTraceInterceptor(new TraceInterceptor() {
      public int priority() { return 1001; }
      public Collection<? extends MutableSpan> onTraceComplete(Collection<? extends MutableSpan> spans) {
        for (MutableSpan span : spans) {
          if (!"appsec-structured-contract".contentEquals(span.getOperationName())) continue;
          span.setTag("poc.scalar", "retained");
          try {
            span.getClass().getMethod("setMetaStruct", String.class, Object.class)
                .invoke(span, "poc.structured", Map.of("marker", "must-survive"));
            Map<?, ?> metadata = (Map<?, ?>) span.getClass().getMethod("getMetaStruct").invoke(span);
            if (!metadata.containsKey("poc.structured")) throw new IllegalStateException("Metadata was not set");
            System.out.println("STRUCTURED_METADATA_SET");
          } catch (ReflectiveOperationException error) {
            throw new IllegalStateException(error);
          }
        }
        return spans;
      }
    })) throw new IllegalStateException("Datadog tracer is not installed");
    work();
    Thread.sleep(4000);
  }
}
