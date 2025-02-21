// Licensed to Elasticsearch B.V. under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. Elasticsearch B.V. licenses this file to you under
// the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package middleware

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/elastic/elastic-agent-libs/monitoring"

	"github.com/elastic/apm-server/internal/beater/monitoringtest"
	"github.com/elastic/apm-server/internal/beater/request"
)

var (
	mockMonitoringRegistry = monitoring.Default.NewRegistry("mock.monitoring")
	mockMonitoringNil      = map[request.ResultID]*monitoring.Int{}
	mockMonitoring         = request.DefaultMonitoringMapForRegistry(mockMonitoringRegistry)
)

func TestMonitoringHandler(t *testing.T) {
	checkMonitoring := func(t *testing.T,
		h func(*request.Context),
		expected map[request.ResultID]int,
		m map[request.ResultID]*monitoring.Int,
	) {
		monitoringtest.ClearRegistry(m)
		c, _ := DefaultContextWithResponseRecorder()
		Apply(MonitoringMiddleware(m), h)(c)
		equal, result := monitoringtest.CompareMonitoringInt(expected, m)
		assert.True(t, equal, result)
	}

	t.Run("Error", func(t *testing.T) {
		checkMonitoring(t,
			Handler403,
			map[request.ResultID]int{
				request.IDRequestCount:            1,
				request.IDResponseCount:           1,
				request.IDResponseErrorsCount:     1,
				request.IDResponseErrorsForbidden: 1},
			mockMonitoring)
	})

	t.Run("Accepted", func(t *testing.T) {
		checkMonitoring(t,
			Handler202,
			map[request.ResultID]int{
				request.IDRequestCount:          1,
				request.IDResponseCount:         1,
				request.IDResponseValidCount:    1,
				request.IDResponseValidAccepted: 1},
			mockMonitoring)
	})

	t.Run("Idle", func(t *testing.T) {
		checkMonitoring(t,
			HandlerIdle,
			map[request.ResultID]int{
				request.IDRequestCount:       1,
				request.IDResponseCount:      1,
				request.IDResponseValidCount: 1,
				request.IDUnset:              1},
			mockMonitoring)
	})

	t.Run("Panic", func(t *testing.T) {
		checkMonitoring(t,
			Apply(RecoverPanicMiddleware(), HandlerPanic),
			map[request.ResultID]int{
				request.IDRequestCount:           1,
				request.IDResponseCount:          1,
				request.IDResponseErrorsCount:    1,
				request.IDResponseErrorsInternal: 1,
			},
			mockMonitoring)
	})

	t.Run("Nil", func(t *testing.T) {
		checkMonitoring(t,
			HandlerIdle,
			map[request.ResultID]int{},
			mockMonitoringNil)
	})
}
