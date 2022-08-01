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
package host

import (
	"testing"

	"github.com/shirou/gopsutil/v3/host"
	"github.com/stretchr/testify/assert"
)

func TestFillOsVersion(t *testing.T) {
	stats := &SystemStats{}
	info, _ := host.Info()
	fillOsVersion(stats, info)
	assert.Len(t, stats.Nixver, 3)
	assert.NotEmpty(t, stats.Nixver[0])
	assert.NotEmpty(t, stats.Nixver[1])
	assert.Empty(t, stats.Nixver[2])
}
