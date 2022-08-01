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
	"strings"

	"github.com/shirou/gopsutil/v3/host"
)

type osVersion [3]string

const osName = runtime.GOOS

func fillOsVersion(stats *SystemStats, info *host.InfoStat) {
	stats.Nixver = osVersion{info.Platform, info.PlatformVersion, ""}
}

func getOSVersion(info *host.InfoStat) string {
	return strings.Trim(info.Platform+" "+info.PlatformVersion, " ")
}
