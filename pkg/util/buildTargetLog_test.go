package util

import (
	"testing"
	"time"

	"github.com/christiandoxa/welog/pkg/model"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestBuildTargetLogFields(t *testing.T) {
	reqTime := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	req := model.TargetRequest{
		URL:         "https://example.com/api",
		Method:      "POST",
		ContentType: "application/json",
		Header:      map[string]interface{}{"Content-Type": "application/json"},
		Body:        []byte(`{"name":"welog"}`),
		Timestamp:   reqTime,
	}
	res := model.TargetResponse{
		Header:  map[string]interface{}{"X-Trace": "abc"},
		Body:    []byte(`{"ok":true}`),
		Status:  201,
		Latency: time.Second,
	}

	fields := BuildTargetLogFields(req, res)

	assert.Equal(t, logrus.Fields{"name": "welog"}, fields["targetRequestBody"])
	assert.Equal(t, `{"name":"welog"}`, fields["targetRequestBodyString"])
	assert.Equal(t, req.ContentType, fields["targetRequestContentType"])
	assert.Equal(t, req.Method, fields["targetRequestMethod"])
	assert.Equal(t, req.URL, fields["targetRequestURL"])
	assert.Equal(t, logrus.Fields{"ok": true}, fields["targetResponseBody"])
	assert.Equal(t, `{"ok":true}`, fields["targetResponseBodyString"])
	assert.Equal(t, res.Status, fields["targetResponseStatus"])
	assert.Equal(t, res.Latency.String(), fields["targetResponseLatency"])
	assert.Equal(t, reqTime.Add(time.Second).Format(time.RFC3339Nano), fields["targetResponseTimestamp"])
}
