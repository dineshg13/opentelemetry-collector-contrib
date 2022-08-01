// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

package gohai // github.com/open-telemetry/opentelemetry-collector-contrib/exporter/datadogexporter/internal/metadata/internal/gohai

import (
	"github.com/DataDog/gohai/cpu"
	"github.com/DataDog/gohai/filesystem"
	"github.com/DataDog/gohai/memory"
	"github.com/DataDog/gohai/platform"
	"go.uber.org/zap"
)

// GetPayload builds a payload of every metadata collected with gohai except processes metadata.
func GetPayload(logger *zap.Logger) *Gohai {
	return getGohaiInfo(logger)
}

func getGohaiInfo(logger *zap.Logger) *Gohai {
	res := new(Gohai)

	cpuPayload, err := new(cpu.Cpu).Collect()
	if err == nil {
		res.CPU = cpuPayload
	} else {
		logger.Error("Failed to retrieve cpu metadata", zap.Error(err))
	}

	fileSystemPayload, err := new(filesystem.FileSystem).Collect()
	if err == nil {
		res.FileSystem = fileSystemPayload
	} else {
		logger.Error("Failed to retrieve filesystem metadata", zap.Error(err))
	}

	memoryPayload, err := new(memory.Memory).Collect()
	if err == nil {
		res.Memory = memoryPayload
	} else {
		logger.Error("Failed to retrieve memory metadata", zap.Error(err))
	}

	platformPayload, err := new(platform.Platform).Collect()
	if err == nil {
		res.Platform = platformPayload
	} else {
		logger.Error("Failed to retrieve platform metadata", zap.Error(err))
	}

	return res
}
