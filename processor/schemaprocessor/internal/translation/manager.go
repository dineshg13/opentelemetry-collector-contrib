// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package translation // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/schemaprocessor/internal/translation"

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"go.uber.org/zap"
)

var errNilValueProvided = errors.New("nil value provided")

// Manager is responsible for ensuring that schemas are kept up to date
// with the most recent version that are requested.
type Manager interface {
	// RequestTranslation will provide either the defined Translation
	// if it is a known target, or, return a noop variation.
	// In the event that a matched Translation, on a missed version
	// there is a potential to block during this process.
	// Otherwise, the translation will allow concurrent reads.
	RequestTranslation(ctx context.Context, schemaURL string) Translation

	// SetProviders will update the list of providers used by the manager
	// to look up schemaURLs
	SetProviders(providers ...Provider) error
}

type manager struct {
	log *zap.Logger

	rw            sync.RWMutex
	providers     []Provider
	match         map[string]*Version
	translatorMap map[string]*translator
}

var _ Manager = (*manager)(nil)

// NewManager creates a manager that will allow for management
// of schema
func NewManager(targetSchemaURLS []string, log *zap.Logger) (Manager, error) {
	if log == nil {
		return nil, fmt.Errorf("logger: %w", errNilValueProvided)
	}

	match := make(map[string]*Version, len(targetSchemaURLS))
	for _, target := range targetSchemaURLS {
		family, version, err := GetFamilyAndVersion(target)
		if err != nil {
			return nil, err
		}
		match[family] = version
	}

	return &manager{
		log:           log,
		match:         match,
		translatorMap: make(map[string]*translator),
	}, nil
}

func (m *manager) RequestTranslation(ctx context.Context, schemaURL string) Translation {
	family, version, err := GetFamilyAndVersion(schemaURL)
	if err != nil {
		m.log.Error("No valid schema url was provided, using no-op schema",
			zap.String("schema-url", schemaURL),
		)
		return nopTranslation{}
	}

	targetTranslation, match := m.match[family]
	if !match {
		m.log.Warn("Not a known targetTranslation, providing Nop Translation",
			zap.String("schema-url", schemaURL),
		)
		return nopTranslation{}
	}

	m.rw.RLock()
	t, exists := m.translatorMap[family]
	m.rw.RUnlock()

	if exists && t.SupportedVersion(version) {
		return t
	}

	for _, p := range m.providers {
		content, err := p.Retrieve(ctx, schemaURL)
		if err != nil {
			m.log.Error("Failed to lookup schemaURL",
				zap.Error(err),
				zap.String("schemaURL", schemaURL),
			)
			return nopTranslation{}
		}
		t, err := newTranslator(
			m.log.Named("translator").With(
				zap.String("family", family),
				zap.Stringer("target", targetTranslation),
			),
			joinSchemaFamilyAndVersion(family, targetTranslation),
			content,
		)
		if err != nil {
			m.log.Error("Failed to create translator", zap.Error(err))
			continue
		}
		m.rw.Lock()
		m.translatorMap[family] = t
		m.rw.Unlock()
		return t
	}

	return nopTranslation{}
}

func (m *manager) SetProviders(providers ...Provider) error {
	if len(providers) == 0 {
		return fmt.Errorf("zero providers set: %w", errNilValueProvided)
	}
	m.rw.Lock()
	defer m.rw.Unlock()
	m.providers = append(m.providers[:0], providers...)
	return nil
}
