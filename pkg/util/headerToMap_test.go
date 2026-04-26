package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/valyala/fasthttp"
)

func TestHeaderToMapResponseHeader(t *testing.T) {
	respHeader := fasthttp.ResponseHeader{}
	respHeader.Set("Content-Type", "application/json")
	respHeader.Set("X-Custom", "value")
	respHeader.Set("Set-Cookie", "sid=secret")

	result := HeaderToMap(&respHeader)

	assert.Equal(t, "application/json", result["Content-Type"])
	assert.Equal(t, "value", result["X-Custom"])
	assert.Equal(t, RedactedValue, result["Set-Cookie"])
}

func TestHeaderToMapRequestHeader(t *testing.T) {
	reqHeader := fasthttp.RequestHeader{}
	reqHeader.Set("Authorization", "Bearer token")
	reqHeader.Set("Accept", "application/json")

	result := HeaderToMap(&reqHeader)

	assert.Equal(t, RedactedValue, result["Authorization"])
	assert.Equal(t, "application/json", result["Accept"])
}

func TestHeaderToMapUnsupportedHeader(t *testing.T) {
	assert.Empty(t, HeaderToMap(struct{}{}))
}
