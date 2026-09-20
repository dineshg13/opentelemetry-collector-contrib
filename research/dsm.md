# DSM — Low-Level Implementation (message formats + semantics)

> Part 2 of 3. Assumes [[01-mental-map]]. **Agent-repo** claims carry verified
> `file:line` refs (`~/go/src/github.com/DataDog/datadog-agent`). **dd-trace-go**
> internals are **code-verified this session** against `internal/datastreams/*`
> (labelled 🟢). Items from public docs or other tracers are labelled 🟡
> (not code-verified here).

---

## 1. Instrumentation & the checkpoint flow

DSM is driven from the tracer's messaging integrations. At each produce/consume the
integration calls a checkpoint, which (a) computes the new pathway hash, (b) records
latency into local aggregates, and (c) injects/extracts the context on the message.

Auto-instrumentation covers Kafka, RabbitMQ, SQS, SNS, Kinesis, Pub/Sub, Azure
Service Bus, IBM MQ (coverage varies by language 🟡). **Manual API** 🟡 (for
unsupported clients — the context must travel _with the message_):

```
 Java    DataStreamsCheckpointer.get().setProduceCheckpoint(queueType, name, carrier)
                                        .setConsumeCheckpoint(queueType, name, carrier)
 Node    tracer.dataStreamsCheckpointer.setProduceCheckpoint(queueType, name, carrier)
 Python  set_produce_checkpoint(queue_type, name, setter)   # setter(key,val)
         set_consume_checkpoint(queue_type, name, getter)   # getter(key)->val
 Ruby    Datadog::DataStreams.set_produce_checkpoint(queue_type, name, &block)
```

- `queueType` — messaging system (`kafka`, `rabbitmq`, `sqs`, `sns`, `kinesis`,
  `servicebus`, …). Recognized strings unlock system-specific metrics.
- `name` — queue/topic/subscription name.
- `carrier` — a read (`entries()`/getter) + write (`set(k,v)`/setter) adapter over
  the message headers, so the tracer can inject/extract the pathway context.
- The **consume** checkpoint has a dual role: it links the message to the pathway
  _and_ primes this service so anything it produces next continues the chain.
- Requires **Agent v7.34.0+** 🟡.

🟢 In dd-trace-go the internal entry point is `SetCheckpoint` /
`SetCheckpointWithParams` in `internal/datastreams/processor.go`: it reads the parent
pathway from `context.Context`, computes the child hash via `hash_cache.go`, pushes a
`statsPoint`, and returns a context carrying the new child `Pathway`.

---

## 2. Pathway hashing 🟢 (`pathway.go`, `hash_cache.go`)

Two FNV-1 64-bit hashes. First, the **node hash** — the identity of this checkpoint:

```go
func nodeHash(service, env string, edgeTags, processTags []string, containerTagsHash string) uint64 {
    h := fnv.New64()
    sort.Strings(edgeTags)
    h.Write([]byte(service)); h.Write([]byte(env))
    for _, t := range edgeTags {
        if isWellFormedEdgeTag(t) { h.Write([]byte(t)) }  // allow-list only
    }
    for _, t := range processTags { h.Write([]byte(t)) }
    if containerTagsHash != "" { h.Write([]byte(containerTagsHash)) }
    return h.Sum64()
}
```

Edge-tag **allow-list** (only these keys feed the hash; others are carried as
metadata but don't change identity): `event_type, exchange, group,
kafka_cluster_id, topic, type, direction, segment_name`.

Then the **pathway hash** chains this node onto the parent pathway:

```go
func pathwayHash(nodeHash, parentHash uint64) uint64 {
    b := make([]byte, 16)
    binary.LittleEndian.PutUint64(b,   nodeHash)     // bytes 0..7  nodeHash (LE)
    binary.LittleEndian.PutUint64(b[8:], parentHash) // bytes 8..15 parentHash (LE)
    h := fnv.New64(); h.Write(b); return h.Sum64()
}
```

`pathwayHash = FNV(LE(nodeHash) ‖ LE(parentHash))`; a root/origin checkpoint uses
`parentHash = 0`. Results are memoized in `hashCache`.

**Worked example — why chaining works:**

```
  Service A produces to topic T:
    nodeHash_A   = FNV("orders","prod", ["direction:out","topic:T","type:kafka"], …)
    pathwayHash_A = FNV(LE(nodeHash_A) ‖ LE(0))            # parent = 0 (origin)
        │  injected into the Kafka message headers
        ▼
  Service B consumes from topic T (group g):
    nodeHash_B   = FNV("billing","prod",
                       ["direction:in","topic:T","type:kafka","group:g"], …)
    pathwayHash_B = FNV(LE(nodeHash_B) ‖ LE(pathwayHash_A))  # parent = A's hash
```

Because B folds A's hash into its own, `pathwayHash_B` is a fingerprint of the
_entire_ route `A → T → B`, not just B. Any producer/consumer pair that reproduces
the same topology computes the same hash — deterministically. Subtlety confirmed by
cross-checking Go vs Java (§3): a given node is always hashed by _its own_ tracer, so
the `nodeHash` internals (tag ordering, extra inputs) may differ per language without
breaking anything — what crosses the wire is the already-computed pathway hash, used
opaquely as the `parentHash` of the next hop. This is what lets the backend group
millions of messages into a handful of named pathways and draw the topology map.

---

## 3. Context wire format 🟢 (`propagator.go`)

The pathway context rides in a message header. **Both dd-trace-go and dd-trace-java
use only the base64 key `dd-pathway-ctx-base64`** 🟢 (Go: `PropagationKeyBase64`;
Java: `PathwayContext.PROPAGATION_KEY_BASE64`). My earlier claim that a plain-binary
`dd-pathway-ctx` key is used by "other tracers" was **not confirmed** — the two
languages checked are base64-only; treat a binary variant as unverified. (Java also
defines a `_datadog` carrier key for non-header transports.)

`Pathway.Encode()` byte layout:

```
  offset  bytes         field                    encoding
  ──────────────────────────────────────────────────────────────────────────
  [0..7]  8             pathway hash             little-endian uint64
  [8..]   variable      pathwayStart (ms epoch)  signed varint64  (zig-zag)
  [..]    variable      edgeStart    (ms epoch)  signed varint64  (zig-zag)
  ──────────────────────────────────────────────────────────────────────────
  then:   base64.StdEncoding over the whole buffer → header value
```

```go
func (p Pathway) Encode() []byte {
    data := make([]byte, 8, 20)
    binary.LittleEndian.PutUint64(data, p.hash)
    encoding.EncodeVarint64(&data, p.pathwayStart.UnixNano()/int64(time.Millisecond))
    encoding.EncodeVarint64(&data, p.edgeStart.UnixNano()/int64(time.Millisecond))
    return data
}
func (p Pathway) EncodeBase64() string { return base64.StdEncoding.EncodeToString(p.Encode()) }
```

Note the timestamps are in **milliseconds**, not nanoseconds. Inject/extract:
`InjectToBase64Carrier` → `carrier.Set("dd-pathway-ctx-base64", EncodeBase64())`;
`ExtractFromBase64Carrier` → read the header → `DecodeBase64` → `Decode`.

**Why two timestamps?** They are the entire basis of the two latencies:

```
  at a checkpoint observed at "now":
     edgeLatency    = now − edgeStart       (time on the last hop)
     pathwayLatency = now − pathwayStart    (time since the origin)
  after recording, the checkpoint re-stamps:
     edgeStart    := now         (this hop becomes the previous hop for the next)
     pathwayStart := unchanged   (origin is preserved for the whole journey)
```

`pathwayStart` is set once at the origin and copied forward untouched; `edgeStart`
is overwritten at every hop. That's how one propagated pair yields both an
end-to-end and a per-edge measurement at every downstream checkpoint.

### Cross-language contract 🟢 (dd-trace-go vs dd-trace-java, code-verified)

The wire format is what must agree across languages, since a Go producer's context
is decoded by (say) a Java consumer. **Confirmed identical** in both:

- header key `dd-pathway-ctx-base64` (base64-only, both);
- the 8-byte little-endian `uint64` hash prefix + two varint **millisecond** epoch
  timestamps (`pathwayStart`, `edgeStart`);
- `pathwayHash = FNV-1-64( LE(nodeHash) ‖ LE(parentHash) )`, `parentHash = 0` at
  origin;
- transport: `POST /v0.1/pipeline_stats`, **msgpack + gzip**, latency carried as
  **protobuf DDSketch** blobs in **seconds**.

**Differences that do _not_ break interop** (each node is always hashed by its own
tracer, and the pathway hash crosses the wire opaquely, so per-language node-hash
internals never need to match):

| aspect                                 | dd-trace-go                         | dd-trace-java                                                                                                  |
| -------------------------------------- | ----------------------------------- | -------------------------------------------------------------------------------------------------------------- |
| edge-tag order fed to `nodeHash`       | `sort.Strings(edgeTags)` (sorted)   | fixed declared order (bus, direction, exchange, topic, type, subscription, kafka_cluster_id) — **not** sorted  |
| extra `nodeHash` inputs                | `processTags` + `containerTagsHash` | `primaryTag`                                                                                                   |
| consecutive same-direction checkpoints | —                                   | **loop-protection**: rewinds `parentHash` to the last direction change, preventing pathway-cardinality blow-up |

**One nuance to double-check if you ever hand-decode the header:** the MCP reported
Java encoding the timestamps as **zig-zag signed** varints (`encodeSignedVarLong`)
and named Go's helper `EncodeVarint64`. Since cross-language propagation demonstrably
works in production, the on-wire integer encoding must be mutually decodable — i.e.
both are effectively the same signed/zig-zag varint. Flagging it as the single point
I would confirm byte-for-byte before writing an independent decoder.

---

## 4. Aggregation 🟢 (`processor.go`)

The tracer aggregates locally before sending — it never ships per-message data.

- **Bucket window:** `bucketDuration = 10 * time.Second`.
  `alignTs(ts, size) = ts − ts%size` truncates a timestamp to its bucket start.
- **Outer key:** `bucketKey{ serviceName string; btime int64 }`.
- **Inner grouping:** `bucket.points` is `map[uint64]statsGroup` keyed on the
  **pathway hash**. So the effective aggregation key is `(service, 10s-window,
pathway hash)` with `parentHash`/`edgeTags` stored as values.
- **Flush:** a bucket flushes only once its window is complete
  (`btime <= now − bucketDuration`).

Structs:

```go
statsPoint  { edgeTags []string; hash, parentHash uint64; timestamp int64;
              pathwayLatency, edgeLatency, payloadSize int64;
              serviceName string; processTags []string }        // one observation

statsGroup  { service string; edgeTags, processTags []string; hash, parentHash uint64;
              pathwayLatency, edgeLatency, payloadSize *ddsketch.DDSketch } // accumulator
```

**DDSketch** (the quantile store): one shared logarithmic mapping
`NewLogarithmicMappingWithGamma(1.015625, …)` — gamma chosen to match the Datadog
backend so sketches merge without precision loss. Each group holds three sketches:

```
  pathwayLatency.Add( max(ns/1e9, 0) )   // nanoseconds → SECONDS, clamped ≥ 0
  edgeLatency.Add(    max(ns/1e9, 0) )   // SECONDS
  payloadSize.Add(    float64(bytes)  )  // raw BYTES
```

Sketches are never merged client-side; each `bucket×hash` owns its own.

**The two clocks** (`current` vs `origin`, from `01` §4) — every `statsPoint` is
added to _both_ bucket maps in `add()`:

```
  currentBucketTime = alignTs(point.timestamp, 10s)                    → tsTypeCurrentBuckets
  originBucketTime  = alignTs(point.timestamp − point.pathwayLatency, 10s) → tsTypeOriginBuckets
```

Each exported `StatsPoint` carries its `TimestampType` (`"current"` or `"origin"`).

---

## 5. Backlog / consumer lag 🟢 (`processor.go`)

Lag is a _separate_ mechanism from latency — no context propagation, just offsets
the tracer observes locally on the Kafka client. Three offset types, each pushed via
a public entry point onto the same async queue:

```
  TrackKafkaProduceOffset(...)         → produceOffset        (per partitionKey)
  TrackKafkaCommitOffset(...)          → commitOffset         (per partitionConsumerKey)
  TrackKafkaHighWatermarkOffset(...)   → highWatermarkOffset  (per partitionKey)
```

`addKafkaOffset()` aligns the timestamp to the 10s bucket and stores the **latest**
offset (last-write-wins) keyed by `partitionKey{partition, topic, cluster}` or
`partitionConsumerKey{partition, group, topic, cluster}`.

At flush, each offset map entry becomes a `Backlog{ Tags []string; Value int64 }`
with tags assembled inline:

```
  produce:        [ partition:N, topic:X, type:kafka_produce (, kafka_cluster_id:…) ]
  commit:         [ consumer_group:G, partition:N, topic:X, type:kafka_commit ]
  high watermark: [ partition:N, topic:X, type:kafka_high_watermark ]
```

The **backend derives consumer lag = `high_watermark_offset − commit_offset`** for
matching partition/topic tags (in messages), and `data_streams.kafka.lag_seconds`
by combining with produce timing 🟡.

---

## 6. The payload — the DSM "message format" 🟢 (`payload.go`, `transport.go`)

Every ~10s the tracer flushes accumulated buckets into one `StatsPayload` and POSTs
it. This is the wire contract between tracer and Agent/backend.

**Top level:**

| `StatsPayload` field | type            | meaning                                             |
| -------------------- | --------------- | --------------------------------------------------- |
| `Env`                | string          | `DD_ENV`                                            |
| `Service`            | string          | producing service                                   |
| `Stats`              | `[]StatsBucket` | the time buckets                                    |
| `TracerVersion`      | string          |                                                     |
| `Lang`               | string          | `"go"` (hardcoded per tracer)                       |
| `Version`            | string          | service version (`DD_VERSION`)                      |
| `ProcessTags`        | `[]string`      | process-level tags (replaces the old "primary tag") |
| `ProductMask`        | uint64          | bitmask, `productAPM                                | productDSM = 3` |

> Correction to a common assumption: there is **no** `ClientStatsPayload`, no
> `PrimaryTag`, and no free-form `Tags` field. Identity beyond service/env is
> carried in `ProcessTags`.

**Bucket:**

| `StatsBucket` field        | type           | meaning                  |
| -------------------------- | -------------- | ------------------------ |
| `Start`                    | uint64         | bucket start (ns)        |
| `Duration`                 | uint64         | window length (ns, =10s) |
| `Stats`                    | `[]StatsPoint` | per-pathway groups       |
| `Backlogs`                 | `[]Backlog`    | offset entries (§5)      |
| `Transactions`             | []byte         | (transaction tracking)   |
| `TransactionCheckpointIds` | []byte         |                          |

**Point (per pathway hash):**

| `StatsPoint` field | type       | meaning                                                   |
| ------------------ | ---------- | --------------------------------------------------------- |
| `EdgeTags`         | `[]string` | the hop's tags (`direction`, `topic`, `type`, `group`, …) |
| `Hash`             | uint64     | pathway hash (join key)                                   |
| `ParentHash`       | uint64     | upstream pathway hash (topology edge)                     |
| `PathwayLatency`   | []byte     | **protobuf-marshaled DDSketch** (seconds)                 |
| `EdgeLatency`      | []byte     | **protobuf-marshaled DDSketch** (seconds)                 |
| `PayloadSize`      | []byte     | **protobuf-marshaled DDSketch** (bytes)                   |
| `TimestampType`    | string     | `"current"` or `"origin"`                                 |

**Nested encoding — the key detail:**

```
  ┌─ gzip (level 1, BestSpeed) ─────────────────────────────────────────┐
  │  ┌─ msgpack (tinylib/msgp) ─────────────────────────────────────┐   │
  │  │ StatsPayload{ Env, Service, Stats[ StatsBucket{               │   │
  │  │    Stats[ StatsPoint{ Hash, ParentHash, EdgeTags,             │   │
  │  │       PathwayLatency = <protobuf DDSketch bytes>, ← inner PB  │   │
  │  │       EdgeLatency    = <protobuf DDSketch bytes>,             │   │
  │  │       PayloadSize    = <protobuf DDSketch bytes>,             │   │
  │  │       TimestampType } ], Backlogs[…] } ] } }                  │   │
  │  └───────────────────────────────────────────────────────────────┘  │
  └──────────────────────────────────────────────────────────────────────┘
   → msgpack on the OUTSIDE, protobuf DDSketch blobs on the INSIDE, gzip over all
```

**HTTP** (`transport.go`): `POST {agentURL}/v0.1/pipeline_stats` with headers
`Content-Type: application/msgpack`, `Content-Encoding: gzip`, `Datadog-Meta-Lang:
go` (+ lang version/interpreter, and `Datadog-Container-ID`/`Datadog-Entity-ID` when
known).

**End-to-end flow** 🟢:

```
 SetCheckpoint() / TrackKafka*Offset()
     → fast queue → run goroutine add() / addKafkaOffset()
     → accumulate DDSketches in bucket.points + offset maps
     → every 10s flush(): bucket.export() proto-marshals sketches, builds Backlogs
     → assemble StatsPayload → sendPipelineStats(): msgpack → gzip → POST
```

---

## 7. The Agent path — a transparent reverse proxy ✅ (verified in this repo)

The trace-agent does **not** parse or aggregate DSM. It reverse-proxies the tracer's
opaque payload to intake.

- **Listen:** `/v0.1/pipeline_stats` registered in
  `pkg/trace/api/endpoints.go:117-120` → `r.pipelineStatsProxyHandler()`.
- **Forward:** `pkg/trace/api/pipeline_stats.go` —
  `pipelineStatsURLSuffix = "/api/v0.1/pipeline_stats"` (`:27`);
  `pipelineStatsEndpoints()` (`:31`) builds `e.Host + suffix` per configured
  endpoint with its API key; `pipelineStatsProxyHandler()` (`:49`) builds an
  `httputil.ReverseProxy`. The `Director` blanks `User-Agent`, sets `Via`, and adds
  `X-Datadog-Container-Tags` + `X-Datadog-Additional-Tags`
  (`host:…,default_env:…,agent_version:…`, plus `orchestrator:fargate_*`), and bumps
  statsd `datadog.trace_agent.pipelines_stats`.
- **Fan-out:** `multiDataStreamsTransport.RoundTrip` sends to every endpoint
  (`e.Host + /api/v0.1/pipeline_stats`, header `DD-API-KEY`); the first endpoint's
  response is returned, the rest discarded. Body treated as **opaque bytes**, bounded
  by `conf.MaxRequestBytes`.
- **No enable flag.** Always mounted; if endpoints aren't validated it degrades to
  `pipelineStatsErrorHandler` → HTTP 500 "Pipeline stats forwarder is OFF"
  (`:64-69`). Fan-out uses the standard `apm_config.additional_endpoints`.

**A separate, newer pipeline** handles DSM _messages_ (schema/message tracking, not
the pathway stats) via the core-agent event-platform forwarder — verified in
`comp/forwarder/eventplatform/impl/pipelines_datastreams.go`:

```go
const eventTypeDataStreamsMessage = "data-streams-message"
// track type "data_streams_messages", JSON, config prefix "data_streams.forwarder.",
// host prefix "trace.agent.", batch_wait 100ms (common_settings.go:1877-1878)
```

Don't conflate the two: **pathway stats** go through the trace-agent reverse proxy
(`/v0.1/pipeline_stats`, msgpack); **DSM messages** go through the event-platform
forwarder (`data_streams_messages`, JSON, batched). Also unrelated:
`comp/core/autodiscovery/providers/datastreams/kafka_actions.go` is Kafka remote-config
actions, not DSM stats.

---

## 8. File cross-reference

| Concern                                | Location                                                                                                                                                                             |
| -------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Agent listen endpoint                  | `pkg/trace/api/endpoints.go:117`                                                                                                                                                     |
| Agent reverse proxy                    | `pkg/trace/api/pipeline_stats.go:27,31,49,64,115`                                                                                                                                    |
| DSM-message event pipeline             | `comp/forwarder/eventplatform/impl/pipelines_datastreams.go`                                                                                                                         |
| `data_streams.forwarder.*` config      | `pkg/config/setup/common_settings.go:1877-1878`                                                                                                                                      |
| 🟢 pathway/node hash                   | dd-trace-go `internal/datastreams/pathway.go`, `hash_cache.go`                                                                                                                       |
| 🟢 wire format                         | dd-trace-go `internal/datastreams/propagator.go`, `datastreams/propagation.go`                                                                                                       |
| 🟢 checkpoints / aggregation / backlog | dd-trace-go `internal/datastreams/processor.go`                                                                                                                                      |
| 🟢 payload types / msgpack             | dd-trace-go `internal/datastreams/payload.go`, `payload_msgp.go`                                                                                                                     |
| 🟢 transport                           | dd-trace-go `internal/datastreams/transport.go`                                                                                                                                      |
| 🟢 Java wire/hash                      | dd-trace-java `internal-api/.../datastreams/PathwayContext.java`, `dd-trace-core/.../datastreams/{DefaultPathwayContext,DataStreamsPropagator,MsgPackDatastreamsPayloadWriter}.java` |

Legend: ✅ verified in datadog-agent this session · 🟢 code-verified via MCP this
session (dd-trace-go, and dd-trace-java for the cross-language wire contract) · 🟡
public docs, not code-verified here.
