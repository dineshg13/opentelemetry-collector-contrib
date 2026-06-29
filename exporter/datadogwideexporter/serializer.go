// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package datadogwideexporter

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/open-telemetry/opentelemetry-collector-contrib/exporter/datadogwideexporter/internal/widepb"
)

const DefaultWideEnvelopeMaxBytes = 5*1024*1024 - 64*1024

// EnvelopeIdentity names the producer for a batch of wide telemetry.
type EnvelopeIdentity struct {
	Host    string
	Service string
	Tags    map[string]string
}

// Serializer turns logical WideTable values into protobuf envelope payloads.
type Serializer struct {
	identity         EnvelopeIdentity
	maxEnvelopeBytes int
}

type SerializerOption func(*Serializer)

func WithMaxEnvelopeBytes(maxBytes int) SerializerOption {
	return func(s *Serializer) {
		if maxBytes > 0 {
			s.maxEnvelopeBytes = maxBytes
		}
	}
}

func NewSerializer(identity EnvelopeIdentity, opts ...SerializerOption) *Serializer {
	s := &Serializer{
		identity:         identity,
		maxEnvelopeBytes: DefaultWideEnvelopeMaxBytes,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

type SerializedEnvelope struct {
	Payload      []byte
	Host         string
	Service      string
	WindowStart  time.Time
	WindowEnd    time.Time
	TableCount   int
	EncodedBytes int
}

func (s *Serializer) Serialize(ctx context.Context, tables []WideTable) ([]SerializedEnvelope, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(tables) == 0 {
		return nil, nil
	}
	if s.identity.Service == "" {
		return nil, fmt.Errorf("envelope service is required")
	}

	windowStart := tables[0].WindowStart
	windowEnd := tables[0].WindowEnd
	pbTables := make([]*widepb.WideTable, 0, len(tables))
	for _, table := range tables {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !table.WindowStart.Equal(windowStart) || !table.WindowEnd.Equal(windowEnd) {
			return nil, fmt.Errorf("all tables in one serialization call must share a flush window")
		}
		pbTable, err := s.serializeTable(table)
		if err != nil {
			return nil, err
		}
		pbTables = append(pbTables, pbTable)
	}

	envelopes, err := s.pack(windowStart, windowEnd, pbTables)
	if err != nil {
		return nil, err
	}
	out := make([]SerializedEnvelope, 0, len(envelopes))
	for _, envelope := range envelopes {
		payload, err := proto.Marshal(envelope)
		if err != nil {
			return nil, err
		}
		out = append(out, SerializedEnvelope{
			Payload:      payload,
			Host:         envelope.Host,
			Service:      envelope.Service,
			WindowStart:  time.UnixMilli(int64(envelope.FlushWindowStartMs)).UTC(),
			WindowEnd:    time.UnixMilli(int64(envelope.FlushWindowEndMs)).UTC(),
			TableCount:   len(envelope.Tables),
			EncodedBytes: len(payload),
		})
	}
	return out, nil
}

func (s *Serializer) serializeTable(table WideTable) (*widepb.WideTable, error) {
	kind, err := protoTableKind(table.Kind)
	if err != nil {
		return nil, err
	}
	schemaText, err := schemaJSONFor(table.Schema)
	if err != nil {
		return nil, err
	}
	arrowIPC, err := encodeArrowIPC(table, schemaText)
	if err != nil {
		return nil, err
	}
	return &widepb.WideTable{
		EventType:  table.Identity.EventTypeName(),
		Kind:       kind,
		SchemaJson: schemaText,
		ArrowIpc:   arrowIPC,
	}, nil
}

func protoTableKind(kind TableKind) (widepb.TableKind, error) {
	switch kind {
	case TableKindAggregated:
		return widepb.TableKind_TABLE_KIND_AGGREGATED, nil
	case TableKindSampled:
		return widepb.TableKind_TABLE_KIND_SAMPLED, nil
	default:
		return widepb.TableKind_TABLE_KIND_UNSPECIFIED, fmt.Errorf("unsupported table kind %q", kind)
	}
}

func (s *Serializer) pack(windowStart, windowEnd time.Time, tables []*widepb.WideTable) ([]*widepb.WideTelemetryEnvelope, error) {
	newEnvelope := func() *widepb.WideTelemetryEnvelope {
		return &widepb.WideTelemetryEnvelope{
			Version:            2,
			Host:               s.identity.Host,
			Service:            s.identity.Service,
			FlushWindowStartMs: millisUint64(windowStart),
			FlushWindowEndMs:   millisUint64(windowEnd),
			Tags:               cleanEnvelopeTags(s.identity.Tags),
		}
	}

	var envelopes []*widepb.WideTelemetryEnvelope
	current := newEnvelope()
	for _, table := range tables {
		tableBytes := proto.Size(table) + 8
		if len(current.Tables) > 0 && proto.Size(current)+tableBytes > s.maxEnvelopeBytes {
			envelopes = append(envelopes, current)
			current = newEnvelope()
		}
		current.Tables = append(current.Tables, table)
		if len(current.Tables) == 1 && proto.Size(current) > s.maxEnvelopeBytes {
			return nil, fmt.Errorf("wide table event_type=%q kind=%s encoded envelope size %d exceeds max %d",
				table.GetEventType(),
				table.GetKind(),
				proto.Size(current),
				s.maxEnvelopeBytes,
			)
		}
	}
	if len(current.Tables) > 0 {
		envelopes = append(envelopes, current)
	}
	return envelopes, nil
}

func cleanEnvelopeTags(tags map[string]string) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	cleaned := make(map[string]string, len(tags))
	for key, value := range tags {
		if key == "" || value == "" {
			continue
		}
		cleaned[key] = value
	}
	if len(cleaned) == 0 {
		return nil
	}
	return cleaned
}

type semanticField struct {
	Type    string `json:"type"`
	Pattern string `json:"pattern,omitempty"`
}

type semanticSchema struct {
	Columns  map[string]semanticField `json:"columns"`
	Children []string                 `json:"children"`
}

func schemaJSONFor(schema TableSchema) (string, error) {
	fields := make(map[string]semanticField, len(schema.Fields))
	for _, field := range schema.Fields {
		semantic, err := semanticFieldFor(field)
		if err != nil {
			return "", err
		}
		fields[field.Name] = semantic
	}
	children := make([]string, 0, len(schema.Children))
	children = append(children, schema.Children...)
	sort.Strings(children)
	data, err := json.Marshal(semanticSchema{
		Columns:  fields,
		Children: children,
	})
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func semanticFieldFor(field FieldSchema) (semanticField, error) {
	switch field.Role {
	case FieldRoleDimension:
		return semanticField{Type: "tag"}, nil
	case FieldRoleAttribute:
		return semanticField{Type: "attr"}, nil
	case FieldRoleFact:
		typ, err := semanticFactType(field)
		if err != nil {
			return semanticField{}, err
		}
		return semanticField{Type: typ, Pattern: field.Pattern}, nil
	default:
		return semanticField{}, fmt.Errorf("field %q has unsupported role %q", field.Name, field.Role)
	}
}

func semanticFactType(field FieldSchema) (string, error) {
	switch field.FactKind {
	case FactKindCounter:
		return "counter", nil
	case FactKindGauge:
		return "gauge", nil
	case FactKindHistogram:
		return "histogram", nil
	case FactKindError:
		return "error", nil
	case FactKindWarning:
		return "warning", nil
	case FactKindInfo:
		return "info", nil
	default:
		return "", fmt.Errorf("fact %q has unsupported kind %q", field.Name, field.FactKind)
	}
}
