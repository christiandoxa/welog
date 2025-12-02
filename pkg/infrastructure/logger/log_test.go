package logger

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/christiandoxa/welog/pkg/constant/envkey"
	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/esapi"
	"github.com/goccy/go-json"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.elastic.co/ecslogrus"
)

type stubHook struct {
	entries chan *logrus.Entry
}

func (s *stubHook) Levels() []logrus.Level { return logrus.AllLevels }
func (s *stubHook) Fire(e *logrus.Entry) error {
	s.entries <- e
	return nil
}

func TestLoggerWithoutElasticURL(t *testing.T) {
	t.Setenv(envkey.ElasticURL, "")

	l := logger()

	require.NotNil(t, l)
	_, ok := l.Formatter.(*ecslogrus.Formatter)
	assert.True(t, ok)
}

func TestLoggerWithElasticURL(t *testing.T) {
	t.Setenv(envkey.ElasticURL, "http://127.0.0.1:1")
	t.Setenv(envkey.ElasticIndex, "welog-test")
	t.Setenv(envkey.ElasticUsername, "elastic")
	t.Setenv(envkey.ElasticPassword, "password")

	l := logger()

	require.NotNil(t, l)
	assert.Nil(t, client)
}

func TestLoggerWithInvalidURL(t *testing.T) {
	t.Setenv(envkey.ElasticURL, "://bad-url")

	l := logger()

	require.NotNil(t, l)
}

type mockTransport struct{}

func (mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewBufferString(`{}`)),
		Header: http.Header{
			"X-Elastic-Product": []string{"Elasticsearch"},
		},
		Request: req,
	}, nil
}

func TestLoggerWithMockClientSuccess(t *testing.T) {
	t.Setenv(envkey.ElasticURL, "http://example.com")
	t.Setenv(envkey.ElasticIndex, "welog-test")

	origClient := newESClient
	origPing := pingWithContext
	newESClient = func(cfg elasticsearch.Config) (*elasticsearch.Client, error) {
		return elasticsearch.NewClient(elasticsearch.Config{
			Transport: mockTransport{},
			Addresses: cfg.Addresses,
		})
	}
	pingWithContext = func(c *elasticsearch.Client, ctx context.Context) (*esapi.Response, error) {
		return c.Ping(c.Ping.WithContext(ctx))
	}
	t.Cleanup(func() {
		newESClient = origClient
		pingWithContext = origPing
		client = nil
	})

	l := logger()

	require.NotNil(t, l)
	assert.NotEmpty(t, l.Hooks)
}

func TestAsyncHookFire(t *testing.T) {
	h := &stubHook{entries: make(chan *logrus.Entry, 1)}
	async := newAsyncHook(h)

	entry := logrus.NewEntry(logrus.New())
	entry.Message = "async message"

	err := async.Fire(entry)
	require.NoError(t, err)

	select {
	case got := <-h.entries:
		assert.Equal(t, "async message", got.Message)
	case <-time.After(time.Second):
		t.Fatalf("async hook did not fire")
	}
}

func TestAsyncHookLevels(t *testing.T) {
	h := &stubHook{entries: make(chan *logrus.Entry, 1)}
	async := newAsyncHook(h)

	assert.Equal(t, logrus.AllLevels, async.Levels())
}

func TestCopyBufferAndDuplicateEntry(t *testing.T) {
	original := bytes.NewBufferString("payload")
	cloned := copyBuffer(original)
	require.NotNil(t, cloned)
	assert.Equal(t, original.String(), cloned.String())
	assert.NotSame(t, original, cloned)

	entry := logrus.NewEntry(logrus.New())
	entry.Buffer = original
	entry.Message = "message"

	duplicated := duplicateEntry(entry)
	require.NotNil(t, duplicated.Buffer)
	assert.Equal(t, entry.Message, duplicated.Message)
	assert.NotSame(t, entry.Buffer, duplicated.Buffer)
}

func TestBuildFallbackBytes(t *testing.T) {
	entry := logrus.NewEntry(logrus.New())
	entry.Message = "fallback"

	result := buildFallbackBytes(entry, errors.New("hook failed"))

	require.NotEmpty(t, result)
	assert.True(t, bytes.HasSuffix(result, []byte("\n")))
	assert.Contains(t, string(result), "fallback")
	assert.Contains(t, string(result), "hook_error")
}

func TestFormatEntry(t *testing.T) {
	assert.Nil(t, formatEntry(nil))

	log := logrus.New()
	log.SetFormatter(&logrus.TextFormatter{DisableTimestamp: true})
	entry := log.WithField("key", "value")
	entry.Message = "formatted"

	out := formatEntry(entry)
	require.NotEmpty(t, out)
	assert.Contains(t, string(out), "formatted")
}

func TestWriteFallbackLog(t *testing.T) {
	backupPath := fallbackLogPath + ".bak"
	original, err := os.ReadFile(fallbackLogPath)
	existed := err == nil
	if existed {
		require.NoError(t, os.WriteFile(backupPath, original, 0o644))
	}
	defer func() {
		if existed {
			data, readErr := os.ReadFile(backupPath)
			require.NoError(t, readErr)
			require.NoError(t, os.WriteFile(fallbackLogPath, data, 0o644))
			_ = os.Remove(backupPath)
		} else {
			_ = os.Remove(fallbackLogPath)
		}
	}()

	entry := logrus.NewEntry(logrus.New())
	entry.Message = "persisted"

	writeFallbackLog(entry, errors.New("hook error"))

	content, err := os.ReadFile(fallbackLogPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "persisted")
	assert.Contains(t, string(content), "hook_error")
}

type errorHook struct {
	called chan struct{}
}

func (e *errorHook) Levels() []logrus.Level { return logrus.AllLevels }
func (e *errorHook) Fire(_ *logrus.Entry) error {
	close(e.called)
	return errors.New("hook error")
}

func TestAsyncHookWorkerErrorPath(t *testing.T) {
	backupPath := fallbackLogPath + ".bak"
	original, err := os.ReadFile(fallbackLogPath)
	existed := err == nil
	if existed {
		require.NoError(t, os.WriteFile(backupPath, original, 0o644))
	}
	defer func() {
		if existed {
			data, readErr := os.ReadFile(backupPath)
			require.NoError(t, readErr)
			require.NoError(t, os.WriteFile(fallbackLogPath, data, 0o644))
			_ = os.Remove(backupPath)
		} else {
			_ = os.Remove(fallbackLogPath)
		}
	}()

	h := &errorHook{called: make(chan struct{}, 1)}
	async := newAsyncHook(h)
	entry := logrus.NewEntry(logrus.New())

	require.NoError(t, async.Fire(entry))

	select {
	case <-h.called:
	case <-time.After(time.Second):
		t.Fatalf("hook did not fire")
	}
	time.Sleep(50 * time.Millisecond) // allow worker to write fallback log

	content, err := os.ReadFile(fallbackLogPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "hook_error")
}

func TestTrimOldestLines(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "logs-trim-*.txt")
	require.NoError(t, err)
	defer func() {
		path := tmpFile.Name()
		_ = tmpFile.Close()
		_ = os.Remove(path)
	}()

	_, err = tmpFile.WriteString("first\nsecond\nthird\n")
	require.NoError(t, err)
	require.NoError(t, tmpFile.Close())

	err = trimOldestLines(tmpFile.Name(), int64(len("first\n")+1))
	require.NoError(t, err)

	trimmed, err := os.ReadFile(tmpFile.Name())
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(trimmed)), "\n")
	assert.Equal(t, []string{"second", "third"}, lines)
}

func TestIndexNameFunc(t *testing.T) {
	t.Setenv(envkey.ElasticIndex, "welog-prefix")
	name := indexNameFunc()

	assert.True(t, strings.HasPrefix(name, "welog-prefix-"))
}

func TestEcsLogMessageModifierFunc(t *testing.T) {
	formatter := &ecslogrus.Formatter{}
	modifier := ecsLogMessageModifierFunc(formatter)

	entry := logrus.NewEntry(logrus.New())
	entry.Time = time.Now()
	entry.Message = "ecs"
	entry.Logger.Formatter = formatter

	result := modifier(entry, nil)

	if raw, ok := result.(json.RawMessage); ok {
		assert.NotEmpty(t, raw)
	} else if e, ok := result.(*logrus.Entry); ok {
		assert.Equal(t, "ecs", e.Message)
	} else {
		t.Fatalf("unexpected result type %T", result)
	}
}

func TestMonitorConnectionTriggersReinit(t *testing.T) {
	t.Setenv(envkey.ElasticURL, "http://127.0.0.1:1")
	t.Setenv(envkey.ElasticIndex, "welog-test")

	log := logger()

	reinitializeLogger(log)

	assert.NotNil(t, log)
}

func TestMonitorConnectionWithCustomTicker(t *testing.T) {
	t.Setenv(envkey.ElasticURL, "http://example.com")

	stopCh := make(chan struct{})
	monitorStop = stopCh
	origTicker := tickerFactory
	tickerFactory = func(_ time.Duration) *time.Ticker {
		return time.NewTicker(time.Millisecond)
	}
	origClient := newESClient
	origPing := pingWithContext
	newESClient = func(cfg elasticsearch.Config) (*elasticsearch.Client, error) {
		return elasticsearch.NewClient(elasticsearch.Config{
			Transport: mockTransport{},
			Addresses: cfg.Addresses,
		})
	}
	pingWithContext = func(c *elasticsearch.Client, ctx context.Context) (*esapi.Response, error) {
		return c.Ping(c.Ping.WithContext(ctx))
	}
	t.Cleanup(func() {
		tickerFactory = origTicker
		newESClient = origClient
		pingWithContext = origPing
		client = nil
		monitorStop = nil
	})

	client, _ = newESClient(elasticsearch.Config{Addresses: []string{"http://example.com"}})
	instance = logrus.New()

	done := make(chan struct{})
	go func() {
		monitorConnection()
		close(done)
	}()

	time.Sleep(5 * time.Millisecond)
	close(stopCh)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("monitorConnection did not exit")
	}
}

func TestReinitializeLoggerSuccess(t *testing.T) {
	t.Setenv(envkey.ElasticURL, "http://example.com")
	t.Setenv(envkey.ElasticIndex, "welog-test")

	origClient := newESClient
	origPing := pingWithContext
	newESClient = func(cfg elasticsearch.Config) (*elasticsearch.Client, error) {
		return elasticsearch.NewClient(elasticsearch.Config{
			Transport: mockTransport{},
			Addresses: cfg.Addresses,
		})
	}
	pingWithContext = func(c *elasticsearch.Client, ctx context.Context) (*esapi.Response, error) {
		return c.Ping(c.Ping.WithContext(ctx))
	}
	t.Cleanup(func() {
		newESClient = origClient
		pingWithContext = origPing
		client = nil
	})

	log := logrus.New()
	reinitializeLogger(log)

	assert.NotNil(t, client)
	assert.NotEmpty(t, log.Hooks)
}

func TestTrimOldestLinesInsufficientData(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "logs-trim-insufficient-*.txt")
	require.NoError(t, err)
	defer func() {
		path := tmpFile.Name()
		_ = tmpFile.Close()
		_ = os.Remove(path)
	}()

	_, err = tmpFile.WriteString("only\n")
	require.NoError(t, err)
	require.NoError(t, tmpFile.Close())

	err = trimOldestLines(tmpFile.Name(), 1024)
	require.NoError(t, err)

	data, err := os.ReadFile(tmpFile.Name())
	require.NoError(t, err)
	assert.NotNil(t, data)
}

func TestTrimOldestLinesScannerError(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "logs-trim-error-*.txt")
	require.NoError(t, err)
	defer func() {
		path := tmpFile.Name()
		_ = tmpFile.Close()
		_ = os.Remove(path)
	}()

	longLine := strings.Repeat("x", 2*1024*1024)
	_, err = tmpFile.WriteString(longLine)
	require.NoError(t, err)
	require.NoError(t, tmpFile.Close())

	err = trimOldestLines(tmpFile.Name(), 10)
	assert.Error(t, err)
}

func TestEnsureFallbackCapacityTrim(t *testing.T) {
	backupPath := fallbackLogPath + ".bak"
	original, err := os.ReadFile(fallbackLogPath)
	existed := err == nil
	if existed {
		require.NoError(t, os.WriteFile(backupPath, original, 0o644))
	}
	defer func() {
		if existed {
			data, readErr := os.ReadFile(backupPath)
			require.NoError(t, readErr)
			require.NoError(t, os.WriteFile(fallbackLogPath, data, 0o644))
			_ = os.Remove(backupPath)
		} else {
			_ = os.Remove(fallbackLogPath)
		}
	}()

	require.NoError(t, os.WriteFile(fallbackLogPath, []byte("line1\nline2\n"), 0o644))

	err = ensureFallbackCapacity(fallbackMaxBytes + 1)
	require.NoError(t, err)

	_, err = os.Stat(fallbackLogPath)
	require.NoError(t, err)
}

func TestLoggerSingleton(t *testing.T) {
	once = sync.Once{}
	instance = nil
	client = nil

	t.Setenv(envkey.ElasticURL, "")

	first := Logger()
	second := Logger()

	require.Equal(t, first, second)
}

func TestFileSizeHelper(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "size.txt")
	require.NoError(t, os.WriteFile(path, []byte("12345"), 0o644))

	size, err := fileSize(path)
	require.NoError(t, err)
	assert.Equal(t, int64(5), size)
}

func TestNewScanner(t *testing.T) {
	buf := bytes.NewBufferString(strings.Repeat("a", 10))
	scanner := newScanner(buf)

	require.NotNil(t, scanner)
}

// Ensure the dependency compiles within tests.
func TestElasticClientType(t *testing.T) {
	var c *elasticsearch.Client
	assert.Nil(t, c)
}
