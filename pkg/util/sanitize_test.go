package util

import (
	"errors"
	"net/http"
	"testing"

	"github.com/agiledragon/gomonkey/v2"
	"github.com/goccy/go-json"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSanitizerDefaults(t *testing.T) {
	s := NewSanitizer(0, nil)

	assert.Equal(t, DefaultMaxLoggedBodyBytes, s.maxBodyBytes)
	assert.True(t, s.IsSensitiveKey("api-key"))
}

func TestBuildBodyLogFieldsEdgeBranches(t *testing.T) {
	s := NewSanitizer(len(`{"a":1}`), nil)

	fields, raw := s.BuildBodyLogFields([]byte(`{"a":1} `))
	assert.Equal(t, true, fields["_truncated"])
	assert.Equal(t, `{"a":1}`+truncatedSuffix, raw)

	emptyFields, emptyRaw := s.BuildBodyLogFields(nil)
	assert.Empty(t, emptyFields)
	assert.Empty(t, emptyRaw)

	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyFuncReturn(json.Marshal, nil, errors.New("marshal error"))

	marshalFields, marshalRaw := NewSanitizer(100, nil).BuildBodyLogFields([]byte(`{"token":"secret"}`))
	assert.Empty(t, marshalFields)
	assert.NotContains(t, marshalRaw, "secret")
	assert.Contains(t, marshalRaw, RedactedValue)
}

func TestSanitizeURLBranches(t *testing.T) {
	s := NewSanitizer(8, nil)

	assert.Contains(t, s.SanitizeURL("http://[::1"), "http://[")
	assert.Equal(t, "https://user:%5BREDACTED%5D@example.com/path", s.SanitizeURL("https://user:secret@example.com/path"))
	assert.Equal(t, "https://example.com/path?keep=value&token=%5BREDACTED%5D", SanitizeURL("https://example.com/path?token=secret&keep=value"))
}

func TestSanitizeCollectionBranches(t *testing.T) {
	s := NewSanitizer(4, nil)

	assert.Equal(t, "abcd"+truncatedSuffix, s.SanitizeRawString("abcdef"))
	assert.Equal(t, map[string]any{"Authorization": RedactedValue}, SanitizeHTTPHeader(http.Header{"Authorization": {"secret"}}))

	slices := SanitizeStringSliceMap(map[string][]string{
		"Authorization": {"secret"},
		"Accept":        {"application/json"},
		"X-Multi":       {"a", "b"},
	})
	assert.Equal(t, RedactedValue, slices["Authorization"])
	assert.Equal(t, "application/json", slices["Accept"])
	assert.Equal(t, []string{"a", "b"}, slices["X-Multi"])

	headers := s.SanitizeHeaderMap(map[string]any{
		"Authorization": "secret",
		"Nested":        map[string]any{"password": "secret"},
	})
	assert.Equal(t, RedactedValue, headers["Authorization"])
	assert.Equal(t, RedactedValue, headers["Nested"].(map[string]any)["password"])
}

func TestSanitizeValueBranches(t *testing.T) {
	s := NewSanitizer(4, nil)

	fields := s.SanitizeValue(logrus.Fields{
		"token":  "secret",
		"nested": logrus.Fields{"password": "secret"},
	}).(logrus.Fields)
	assert.Equal(t, RedactedValue, fields["token"])
	assert.Equal(t, RedactedValue, fields["nested"].(logrus.Fields)["password"])

	stringMap := s.SanitizeValue(map[string]string{
		"password": "secret",
		"name":     "welog",
	}).(map[string]string)
	assert.Equal(t, RedactedValue, stringMap["password"])
	assert.Equal(t, "welog", stringMap["name"])

	slice := s.SanitizeValue([]any{map[string]any{"client_secret": "secret"}, "ok"}).([]any)
	assert.Equal(t, RedactedValue, slice[0].(map[string]any)["client_secret"])
	assert.Equal(t, "ok", slice[1])

	assert.Equal(t, "plain", SanitizeValue("plain"))
}

func TestSanitizerSmallHelpers(t *testing.T) {
	assert.True(t, IsSensitiveKey("session-id"))
	assert.False(t, IsSensitiveKey("public-name"))
	assert.Equal(t, "abc123", normalizeKey("A-b_C123"))

	clipped, truncated := NewSanitizer(3, nil).clipBytes([]byte("abcd"))
	require.True(t, truncated)
	assert.Equal(t, []byte("abc"), clipped)
}
