// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// nolint:gocritic
package host // import "github.com/open-telemetry/opentelemetry-collector-contrib/exporter/datadogexporter/internal/metadata/internal/host"

import (
	"runtime"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/host"
	"go.uber.org/zap"
)

// InitHostMetadata initializes necessary CPU info
func InitHostMetadata() error {
	var err error
	_, err = cpu.Info()

	return err

}

//GetSystemStats would return system stats information. This is part of host metadata payload
func GetSystemStats(logger *zap.Logger) *SystemStats {
	var stats *SystemStats
	cpuInfo := getCPUInfo(logger)
	hostInfo := getHostInfo(logger)

	stats = &SystemStats{
		Machine:   runtime.GOARCH,
		Platform:  runtime.GOOS,
		Processor: cpuInfo.ModelName,
		CPUCores:  cpuInfo.Cores,
	}

	// fill the platform dependent bits of info
	fillOsVersion(stats, hostInfo)

	return stats
}

// getCPUInfo returns InfoStat for the first CPU gopsutil found
func getCPUInfo(logger *zap.Logger) *cpu.InfoStat {

	i, err := cpu.Info()
	if err != nil {
		// don't cache and return zero value
		logger.Error("failed to retrieve cpu info", zap.Error(err))
		return &cpu.InfoStat{}
	}
	info := &i[0]
	return info
}

func getHostInfo(logger *zap.Logger) *host.InfoStat {

	info, err := host.Info()
	if err != nil {
		// don't cache and return zero value
		logger.Error("failed to retrieve host info", zap.Error(err))
		return &host.InfoStat{}
	}
	return info
}

// GetStatusInformation just returns an InfoStat object, we need some additional information that's not
func GetStatusInformation(logger *zap.Logger) *host.InfoStat {
	return getHostInfo(logger)
}
