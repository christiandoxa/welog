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

func TestBuildTargetLogFieldsInvalidJSON(t *testing.T) {
	reqTime := time.Date(2024, 2, 3, 4, 5, 6, 0, time.UTC)
	req := model.TargetRequest{
		URL:         "https://example.com/api",
		Method:      "POST",
		ContentType: "application/json",
		Header:      map[string]interface{}{"Content-Type": "application/json"},
		Body:        []byte("{"),
		Timestamp:   reqTime,
	}
	res := model.TargetResponse{
		Header:  map[string]interface{}{"X-Trace": "bad"},
		Body:    []byte("{"),
		Status:  500,
		Latency: 2 * time.Second,
	}

	fields := BuildTargetLogFields(req, res)

	assert.Equal(t, "{", fields["targetRequestBodyString"])
	assert.Equal(t, "{", fields["targetResponseBodyString"])
	assert.Equal(t, reqTime.Add(2*time.Second).Format(time.RFC3339Nano), fields["targetResponseTimestamp"])
}

func TestBuildTargetLogFieldsRedactsSensitiveData(t *testing.T) {
	reqTime := time.Date(2024, 3, 4, 5, 6, 7, 0, time.UTC)
	req := model.TargetRequest{
		URL: "https://example.com/api?token=secret&keep=value",
		Header: map[string]interface{}{
			"Authorization": "Bearer secret",
			"Content-Type":  "application/json",
		},
		Body:      []byte(`{"password":"secret","nested":{"api_key":"abc"},"name":"welog"}`),
		Timestamp: reqTime,
	}
	res := model.TargetResponse{
		Header: map[string]interface{}{"Set-Cookie": "sid=secret"},
		Body:   []byte(`{"access_token":"abc","ok":true}`),
	}

	fields := BuildTargetLogFields(req, res)

	requestBody := fields["targetRequestBody"].(logrus.Fields)
	assert.Equal(t, RedactedValue, requestBody["password"])
	assert.NotContains(t, fields["targetRequestBodyString"], "secret")
	assert.Contains(t, fields["targetRequestBodyString"], RedactedValue)

	requestHeader := fields["targetRequestHeader"].(map[string]interface{})
	assert.Equal(t, RedactedValue, requestHeader["Authorization"])
	assert.Equal(t, "application/json", requestHeader["Content-Type"])
	assert.Equal(t, "https://example.com/api?keep=value&token=%5BREDACTED%5D", fields["targetRequestURL"])

	responseHeader := fields["targetResponseHeader"].(map[string]interface{})
	assert.Equal(t, RedactedValue, responseHeader["Set-Cookie"])
	assert.NotContains(t, fields["targetResponseBodyString"], "abc")
}
