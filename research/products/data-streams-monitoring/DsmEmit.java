import datadog.trace.api.experimental.DataStreamsCheckpointer;
import datadog.trace.api.experimental.DataStreamsContextCarrier;
import datadog.trace.bootstrap.instrumentation.api.AgentSpan;
import datadog.trace.bootstrap.instrumentation.api.AgentScope;
import datadog.trace.bootstrap.instrumentation.api.AgentTracer;
import java.util.HashMap;
import java.util.Map;
import java.util.Set;

/** Real Java agent manual checkpoints; no Kafka client or synthesized payload. */
public class DsmEmit {
  public static void main(String[] args) throws Exception {
    Thread.sleep(1000); // Allow initial Agent /info discovery to complete.
    Map<String, Object> headers = new HashMap<>();
    DataStreamsContextCarrier carrier = new DataStreamsContextCarrier() {
      public Set<Map.Entry<String, Object>> entries() { return headers.entrySet(); }
      public void set(String key, String value) { headers.put(key, value); }
    };
    DataStreamsCheckpointer checkpointer = DataStreamsCheckpointer.get();
    AgentSpan produce = AgentTracer.startSpan("research", "produce");
    try (AgentScope scope = AgentTracer.activateSpan(produce)) {
      checkpointer.setProduceCheckpoint("kafka", "research-orders", carrier);
    } finally {
      produce.finish();
    }
    AgentSpan consume = AgentTracer.startSpan("research", "consume");
    try (AgentScope scope = AgentTracer.activateSpan(consume)) {
      checkpointer.setConsumeCheckpoint("kafka", "research-orders", carrier);
    } finally {
      consume.finish();
    }
    checkpointer.trackTransaction("research-transaction", "research-checkpoint");
    Thread.sleep(1000); // Allow the SDK's asynchronous aggregation queue to drain.
    System.out.println("manual checkpoints invoked; shutdown hook flushes SDK");
  }
}
