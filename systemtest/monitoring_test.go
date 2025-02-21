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

package systemtest_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/mitchellh/mapstructure"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/elastic/apm-server/systemtest"
	"github.com/elastic/apm-server/systemtest/apmservertest"
	"github.com/elastic/apm-server/systemtest/estest"
)

func TestAPMServerMonitoring(t *testing.T) {
	srv := apmservertest.NewUnstartedServerTB(t)
	srv.Config.Monitoring = newFastMonitoringConfig()
	err := srv.Start()
	require.NoError(t, err)

	var state struct {
		Output struct {
			Name string
		}
	}
	getBeatsMonitoringState(t, srv, &state)
	assert.Equal(t, "elasticsearch", state.Output.Name)

	doc := getBeatsMonitoringStats(t, srv, nil)
	assert.Contains(t, doc.Metrics, "apm-server")
}

func TestMonitoring(t *testing.T) {
	srv := apmservertest.NewUnstartedServerTB(t)
	srv.Config.Monitoring = newFastMonitoringConfig()
	err := srv.Start()
	require.NoError(t, err)

	const N = 15
	tracer := srv.Tracer()
	for i := 0; i < N; i++ {
		tx := tracer.StartTransaction("name", "type")
		tx.Duration = time.Second
		tx.End()
	}
	tracer.Flush(nil)
	systemtest.Elasticsearch.ExpectMinDocs(t, N, "traces-*", nil)

	var metrics struct {
		Libbeat map[string]interface{}
		Output  map[string]interface{}
	}
	getBeatsMonitoringStats(t, srv, &metrics)
	// Remove the output.write.bytes key since there isn't a way to assert the
	// writtenBytes at this layer.
	if o := metrics.Libbeat["output"].(map[string]interface{}); len(o) > 0 {
		if w := o["write"].(map[string]interface{}); len(w) > 0 {
			if w["bytes"] != nil {
				delete(w, "bytes")
			}
		}
	}
	assert.Equal(t, map[string]interface{}{
		"output": map[string]interface{}{
			"events": map[string]interface{}{
				"acked":   float64(N),
				"active":  0.0,
				"batches": 1.0,
				"failed":  0.0,
				"toomany": 0.0,
				"total":   float64(N),
			},
			"type":  "elasticsearch",
			"write": map[string]interface{}{},
		},
		"pipeline": map[string]interface{}{
			"events": map[string]interface{}{
				"total": float64(N),
			},
		},
	}, metrics.Libbeat)
	if es := metrics.Output["elasticsearch"].(map[string]interface{}); len(es) > 0 {
		if br := es["bulk_requests"].(map[string]interface{}); len(br) > 0 {
			assert.Greater(t, br["available"], float64(10))
			delete(br, "available")
		}
	}
	assert.Equal(t, map[string]interface{}{
		"elasticsearch": map[string]interface{}{
			"bulk_requests": map[string]interface{}{
				"completed": 1.0,
			},
			"indexers": map[string]interface{}{
				"active":    float64(1),
				"created":   0.0,
				"destroyed": 0.0,
			},
		},
	}, metrics.Output)
}

func TestAPMServerMonitoringBuiltinUser(t *testing.T) {
	// This test is about ensuring the "apm_system" built-in user
	// has sufficient privileges to index monitoring data.
	const username = "apm_system"
	const password = "changeme"
	systemtest.ChangeUserPassword(t, username, password)

	srv := apmservertest.NewUnstartedServerTB(t)
	srv.Config.Monitoring = &apmservertest.MonitoringConfig{
		Enabled:     true,
		StatePeriod: time.Duration(time.Second),
		Elasticsearch: &apmservertest.ElasticsearchOutputConfig{
			Enabled:  true,
			Username: username,
			Password: password,
		},
	}
	require.NoError(t, srv.Start())

	getBeatsMonitoringState(t, srv, nil)
}

func getBeatsMonitoringState(t testing.TB, srv *apmservertest.Server, out interface{}) *beatsMonitoringDoc {
	return getBeatsMonitoring(t, srv, "beats_state", out)
}

func getBeatsMonitoringStats(t testing.TB, srv *apmservertest.Server, out interface{}) *beatsMonitoringDoc {
	return getBeatsMonitoring(t, srv, "beats_stats", out)
}

func getBeatsMonitoring(t testing.TB, srv *apmservertest.Server, type_ string, out interface{}) *beatsMonitoringDoc {
	var result estest.SearchResult
	req := systemtest.Elasticsearch.Search(".monitoring-beats-*").WithQuery(
		estest.TermQuery{Field: type_ + ".beat.uuid", Value: srv.BeatUUID},
	).WithSort("timestamp:desc")
	if _, err := req.Do(context.Background(), &result, estest.WithCondition(result.Hits.MinHitsCondition(1))); err != nil {
		t.Error(err)
	}

	var doc beatsMonitoringDoc
	doc.RawSource = []byte(result.Hits.Hits[0].RawSource)
	err := json.Unmarshal(doc.RawSource, &doc)
	require.NoError(t, err)
	if out != nil {
		switch doc.Type {
		case "beats_state":
			assert.NoError(t, mapstructure.Decode(doc.State, out))
		case "beats_stats":
			assert.NoError(t, mapstructure.Decode(doc.Metrics, out))
		}
	}
	return &doc
}

type beatsMonitoringDoc struct {
	RawSource  []byte    `json:"-"`
	Timestamp  time.Time `json:"timestamp"`
	Type       string    `json:"type"`
	BeatsState `json:"beats_state,omitempty"`
	BeatsStats `json:"beats_stats,omitempty"`
}

type BeatsState struct {
	State map[string]interface{} `json:"state"`
}

type BeatsStats struct {
	Metrics map[string]interface{} `json:"metrics"`
}

func newFastMonitoringConfig() *apmservertest.MonitoringConfig {
	return &apmservertest.MonitoringConfig{
		Enabled:       true,
		MetricsPeriod: 100 * time.Millisecond,
		StatePeriod:   100 * time.Millisecond,
	}
}
