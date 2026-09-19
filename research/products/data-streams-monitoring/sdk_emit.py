"""Exercise the real Python SDK's public DSM checkpoints and actual HTTP writer."""

import json
import time

import ddtrace
from ddtrace.data_streams import set_consume_checkpoint, set_produce_checkpoint
from ddtrace.internal.datastreams import data_streams_processor


carrier = {}
produced = set_produce_checkpoint("kafka", "research-orders", carrier.__setitem__)
producer_hash = produced.hash if produced else None
consumed = set_consume_checkpoint("kafka", "research-orders", carrier.get)
processor = data_streams_processor()
if processor:
    # Broker-less offset observations: actual SDK accumulator and serialization,
    # not automatic Kafka instrumentation. Verify maximum-offset semantics.
    now = time.time()
    processor.track_kafka_produce("research-orders", 0, 12, now)
    processor.track_kafka_produce("research-orders", 0, 10, now)
    processor.track_kafka_commit("research-consumer", "research-orders", 0, 8, now)
    # Deterministic flush, using the SDK's real serializer, gzip and HTTP client.
    processor.periodic()

print(json.dumps({
    "sdk_version": ddtrace.__version__,
    "producer_hash": producer_hash,
    "consumer_hash": consumed.hash if consumed else None,
    "propagation": carrier,
    "enabled": processor is not None,
}))
