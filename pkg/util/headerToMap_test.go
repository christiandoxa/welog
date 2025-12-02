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

	result := HeaderToMap(&respHeader)

	assert.Equal(t, "application/json", result["Content-Type"])
	assert.Equal(t, "value", result["X-Custom"])
}

func TestHeaderToMapRequestHeader(t *testing.T) {
	reqHeader := fasthttp.RequestHeader{}
	reqHeader.Set("Authorization", "Bearer token")

	result := HeaderToMap(&reqHeader)

	assert.Equal(t, "Bearer token", result["Authorization"])
}
