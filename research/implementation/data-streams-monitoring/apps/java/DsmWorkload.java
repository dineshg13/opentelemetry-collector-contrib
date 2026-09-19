// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0
import datadog.trace.api.GlobalTracer;
import datadog.trace.api.Trace;
import datadog.trace.api.experimental.DataStreamsCheckpointer;
import datadog.trace.api.experimental.DataStreamsContextCarrier;
import java.util.HashMap;
import java.util.Map;
import java.util.Set;

/** Manual checkpoint APIs and Datadog annotated spans, with ordinary traces in OTLP. */
public class DsmWorkload {
  @Trace(operationName = "dsm.produce", resourceName = "poc-orders")
  public static void produce(DataStreamsContextCarrier carrier) {
    DataStreamsCheckpointer.get().setProduceCheckpoint("kafka", "poc-orders", carrier);
    System.out.println("{\"operation\":\"produce\",\"trace_id\":\"" + GlobalTracer.get().getTraceId() + "\"}");
  }

  @Trace(operationName = "dsm.consume", resourceName = "poc-orders")
  public static void consume(DataStreamsContextCarrier carrier) {
    DataStreamsCheckpointer.get().setConsumeCheckpoint("kafka", "poc-orders", carrier);
    DataStreamsCheckpointer.get().trackTransaction("poc-transaction", "consumed");
  }

  public static void main(String[] args) throws Exception {
    if (!"otlp".equals(System.getenv("OTEL_TRACES_EXPORTER")))
      throw new IllegalArgumentException("OTEL_TRACES_EXPORTER=otlp is required");
    Thread.sleep(1500); // SDK discovery runs asynchronously before DSM can report.
    int iterations = Integer.parseInt(System.getenv().getOrDefault("WORKLOAD_ITERATIONS", "3"));
    for (int i = 0; i < iterations; i++) {
      Map<String, Object> headers = new HashMap<>();
      DataStreamsContextCarrier carrier = new DataStreamsContextCarrier() {
        public Set<Map.Entry<String, Object>> entries() { return headers.entrySet(); }
        public void set(String key, String value) { headers.put(key, value); }
      };
      produce(carrier);
      Thread.sleep(50);
      consume(carrier);
      Thread.sleep(100);
    }
    Thread.sleep(1500); // Drain the SDK aggregation queue; the SDK shutdown hook flushes it.
    System.out.println("{\"sdk\":\"dd-java-agent\",\"version\":\"1.66.0\",\"workload\":\"manual-in-process-checkpoints\"}");
  }
}
