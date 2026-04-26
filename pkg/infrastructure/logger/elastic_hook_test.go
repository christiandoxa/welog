package logger

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"runtime"
	"testing"
	"time"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/esapi"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newTestClient(t *testing.T, transport http.RoundTripper) *elasticsearch.Client {
	t.Helper()

	client, err := elasticsearch.NewClient(elasticsearch.Config{
		Addresses: []string{"http://example.com"},
		Transport: transport,
	})
	require.NoError(t, err)

	return client
}

func newElasticResponse(req *http.Request, status int, body string) *http.Response {
	if body == "" {
		body = "{}"
	}

	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(bytes.NewBufferString(body)),
		Header: http.Header{
			"X-Elastic-Product": []string{"Elasticsearch"},
		},
		Request: req,
	}
}

func TestElasticHookCreateMessage(t *testing.T) {
	baseLogger := logrus.New()
	baseLogger.SetReportCaller(true)

	baseEntry := logrus.NewEntry(baseLogger)
	baseEntry.Time = time.Unix(1, 0).UTC()
	baseEntry.Level = logrus.ErrorLevel
	baseEntry.Message = "failed"
	baseEntry.Data = logrus.Fields{
		logrus.ErrorKey: errors.New("boom"),
		"key":           "value",
	}
	baseEntry.Caller = &runtime.Frame{
		File:     "/tmp/test.go",
		Line:     42,
		Function: "pkg.test",
	}

	hook := &ElasticHook{host: "svc"}
	raw := hook.createMessage(baseEntry)

	msg, ok := raw.(*Message)
	require.True(t, ok)
	assert.Equal(t, "svc", msg.Host)
	assert.Equal(t, "ERROR", msg.Level)
	assert.Equal(t, "/tmp/test.go:42", msg.File)
	assert.Equal(t, "pkg.test", msg.Func)
	assert.Equal(t, "boom", baseEntry.Data[logrus.ErrorKey])

	modified := false
	modifierEntry := logrus.NewEntry(baseLogger)
	modifierEntry.Time = baseEntry.Time
	modifierEntry.Level = logrus.InfoLevel
	modifierEntry.Message = "ok"
	modifierEntry.Data = logrus.Fields{}

	hook.MessageModifierFunc = func(entry *logrus.Entry, message *Message) any {
		modified = true
		assert.Equal(t, modifierEntry, entry)
		assert.Equal(t, "ok", message.Message)
		return map[string]any{"modified": true}
	}

	assert.Equal(t, map[string]any{"modified": true}, hook.createMessage(modifierEntry))
	assert.True(t, modified)
}

func TestElasticHookFire(t *testing.T) {
	t.Run("marshal error", func(t *testing.T) {
		hook := &ElasticHook{
			index: func() string { return "welog-test" },
			MessageModifierFunc: func(*logrus.Entry, *Message) any {
				return make(chan int)
			},
		}

		err := hook.Fire(logrus.NewEntry(logrus.New()))
		require.Error(t, err)
	})

	t.Run("request error", func(t *testing.T) {
		hook := &ElasticHook{
			client: newTestClient(t, errTransport{}),
			index:  func() string { return "welog-test" },
		}

		err := hook.Fire(logrus.NewEntry(logrus.New()))
		require.Error(t, err)
	})

	t.Run("success", func(t *testing.T) {
		var capturedBody string

		client := newTestClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
			body, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			capturedBody = string(body)
			return newElasticResponse(req, http.StatusCreated, `{"result":"created"}`), nil
		}))

		hook := &ElasticHook{
			client: client,
			host:   "svc",
			index:  func() string { return "welog-test" },
		}

		entry := logrus.NewEntry(logrus.New())
		entry.Time = time.Unix(2, 0).UTC()
		entry.Level = logrus.InfoLevel
		entry.Message = "sent"

		require.NoError(t, hook.Fire(entry))
		assert.Contains(t, capturedBody, `"message":"sent"`)
	})
}

func TestEnsureIndexExists(t *testing.T) {
	t.Run("exists error", func(t *testing.T) {
		err := ensureIndexExists(newTestClient(t, errTransport{}), func() string { return "welog-test" })
		require.Error(t, err)
	})

	t.Run("create on not found", func(t *testing.T) {
		createCalled := false
		client := newTestClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch req.Method {
			case http.MethodHead:
				return newElasticResponse(req, http.StatusNotFound, ""), nil
			case http.MethodPut:
				createCalled = true
				return newElasticResponse(req, http.StatusOK, `{"acknowledged":true}`), nil
			default:
				return newElasticResponse(req, http.StatusOK, `{}`), nil
			}
		}))

		require.NoError(t, ensureIndexExists(client, func() string { return "welog-test" }))
		assert.True(t, createCalled)
	})

	t.Run("exists status error", func(t *testing.T) {
		client := newTestClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return newElasticResponse(req, http.StatusInternalServerError, `{"error":"boom"}`), nil
		}))

		err := ensureIndexExists(client, func() string { return "welog-test" })
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot check index")
	})

	t.Run("create request error", func(t *testing.T) {
		client := newTestClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Method == http.MethodHead {
				return newElasticResponse(req, http.StatusNotFound, ""), nil
			}
			return nil, errors.New("create failed")
		}))

		err := ensureIndexExists(client, func() string { return "welog-test" })
		assert.ErrorIs(t, err, errCannotCreateIndex)
	})

	t.Run("create status error", func(t *testing.T) {
		client := newTestClient(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Method == http.MethodHead {
				return newElasticResponse(req, http.StatusNotFound, ""), nil
			}
			return newElasticResponse(req, http.StatusInternalServerError, `{"error":"boom"}`), nil
		}))

		err := ensureIndexExists(client, func() string { return "welog-test" })
		assert.ErrorIs(t, err, errCannotCreateIndex)
	})
}

func TestCloseBody(t *testing.T) {
	closeBody(nil)
	closeBody(&esapi.Response{})
}
