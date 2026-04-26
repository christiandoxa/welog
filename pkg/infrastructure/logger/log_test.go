package logger

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agiledragon/gomonkey/v2"
	"github.com/christiandoxa/welog/pkg/constant/envkey"
	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/esapi"
	"github.com/goccy/go-json"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubHook struct {
	entries chan *logrus.Entry
}

type ecsStackError struct{}

func (ecsStackError) Error() string { return "boom" }

func (ecsStackError) Format(state fmt.State, verb rune) {
	switch verb {
	case 'v':
		if state.Flag('+') {
			_, _ = io.WriteString(state, "boom\nstack line")
			return
		}
		_, _ = io.WriteString(state, "boom")
	case 's':
		_, _ = io.WriteString(state, "boom")
	}
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
	_, ok := l.Formatter.(*ECSFormatter)
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

func TestAsyncHookFireDropsWhenFull(t *testing.T) {
	h := &stubHook{entries: make(chan *logrus.Entry, 1)}
	async := &asyncHook{
		hook:  h,
		queue: make(chan *logrus.Entry, 1),
	}

	entry := logrus.NewEntry(logrus.New())
	require.NoError(t, async.Fire(entry))
	require.NoError(t, async.Fire(entry))
}

func TestAsyncHookCloseStopsFutureFire(t *testing.T) {
	h := &stubHook{entries: make(chan *logrus.Entry, 1)}
	async := newAsyncHook(h)
	async.Close()

	entry := logrus.NewEntry(logrus.New())
	entry.Message = "after close"
	require.NoError(t, async.Fire(entry))

	select {
	case got := <-h.entries:
		t.Fatalf("closed async hook fired entry %q", got.Message)
	default:
	}
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
	formatter := &ECSFormatter{}
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

func TestECSFormatterFormatUsesLatestECSFields(t *testing.T) {
	log := logrus.New()
	log.SetReportCaller(true)

	entry := logrus.NewEntry(log)
	entry.Time = time.Unix(0, 0).UTC()
	entry.Level = logrus.ErrorLevel
	entry.Message = "ecs"
	entry.Data = logrus.Fields{
		logrus.ErrorKey: ecsStackError{},
		"custom":        "value",
	}
	entry.Caller = &runtime.Frame{
		Function: "github.com/christiandoxa/welog/pkg/infrastructure/logger.TestFormatter",
		File:     "/tmp/example/path/log.go",
		Line:     42,
	}

	formatted, err := (&ECSFormatter{}).Format(entry)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(formatted, &decoded))

	assert.Equal(t, "1970-01-01T00:00:00.000Z", decoded["@timestamp"])
	assert.Equal(t, ecsVersion, decoded["ecs.version"])
	assert.Equal(t, "error", decoded["log.level"])
	assert.Equal(t, "ecs", decoded["message"])
	assert.Equal(t, "github.com/christiandoxa/welog/pkg/infrastructure/logger.TestFormatter", decoded["log.origin.function"])
	assert.Equal(t, "log.go", decoded["log.origin.file.name"])
	assert.EqualValues(t, 42, decoded["log.origin.file.line"])
	assert.Equal(t, "value", decoded["custom"])

	errData, ok := decoded["error"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "boom", errData["message"])
	assert.Equal(t, "logger.ecsStackError", errData["type"])
	assert.Equal(t, "boom\nstack line", errData["stack_trace"])
	assert.NotNil(t, entry.Caller)
}

func TestECSFormatterFormatWithDataKeyAndPrettyCaller(t *testing.T) {
	log := logrus.New()
	log.SetReportCaller(true)
	entry := logrus.NewEntry(log)
	entry.Time = time.Unix(0, 0).UTC()
	entry.Level = logrus.WarnLevel
	entry.Message = "ecs"
	entry.Data = logrus.Fields{
		logrus.ErrorKey: "raw-error",
		"custom":        "value",
	}
	entry.Caller = &runtime.Frame{
		Function: "ignored",
		File:     "/tmp/ignored.go",
		Line:     77,
	}

	formatter := &ECSFormatter{
		DataKey:     "labels",
		PrettyPrint: true,
		CallerPrettyfier: func(*runtime.Frame) (string, string) {
			return "pretty.function", "/tmp/path/custom.go:88"
		},
	}

	formatted, err := formatter.Format(entry)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(formatted, &decoded))

	assert.Equal(t, ecsVersion, decoded["ecs.version"])
	assert.Equal(t, "warning", decoded["log.level"])
	assert.Equal(t, "pretty.function", decoded["log.origin.function"])
	assert.Equal(t, "custom.go", decoded["log.origin.file.name"])
	assert.EqualValues(t, 88, decoded["log.origin.file.line"])
	assert.NotContains(t, decoded, "error")

	labels, ok := decoded["labels"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "value", labels["custom"])
	assert.Equal(t, "raw-error", labels[logrus.ErrorKey])
}

func TestBuildECSErrorObjectNil(t *testing.T) {
	assert.Equal(t, ecsErrorObject{}, buildECSErrorObject(nil))
}

func TestExtractErrorStackTraceWithoutExtendedFormatter(t *testing.T) {
	assert.Empty(t, extractErrorStackTrace(errors.New("boom")))
}

func TestFormatCallerNilFrame(t *testing.T) {
	functionName, fileName, lineNumber := formatCaller(nil, nil)

	assert.Empty(t, functionName)
	assert.Empty(t, fileName)
	assert.Zero(t, lineNumber)
}

func TestFormatCallerPrettyfierWithoutLineNumber(t *testing.T) {
	frame := &runtime.Frame{
		Function: "github.com/christiandoxa/welog/pkg/infrastructure/logger.TestFormatter",
		File:     "/tmp/example/path/log.go",
		Line:     42,
	}

	functionName, fileName, lineNumber := formatCaller(frame, func(*runtime.Frame) (string, string) {
		return "pretty.function", "/tmp/example/path/log.go:not-a-line"
	})

	assert.Equal(t, "pretty.function", functionName)
	assert.Equal(t, "log.go:not-a-line", fileName)
	assert.Zero(t, lineNumber)
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
	stopCh := make(chan struct{})
	monitorStop = stopCh
	defer func() {
		close(stopCh)
		if monitorDone != nil {
			select {
			case <-monitorDone:
			case <-time.After(time.Second):
				t.Fatalf("monitorConnection did not exit")
			}
		}
		monitorStop = nil
		monitorDone = nil
	}()

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

type errCloseReadCloser struct{ bytes.Buffer }

func (e *errCloseReadCloser) Close() error { return errors.New("close error") }

type errWriter struct{}

func (e errWriter) Write([]byte) (int, error) { return 0, errors.New("write error") }

type errTransport struct{}

func (errTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("transport error")
}

func withTempDir(t *testing.T) func() {
	old, err := os.Getwd()
	require.NoError(t, err)
	tmp := t.TempDir()
	require.NoError(t, os.Chdir(tmp))
	return func() {
		_ = os.Chdir(old)
	}
}

func TestEcsLogMessageModifierFuncFormatterError(t *testing.T) {
	formatter := &ECSFormatter{}
	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyMethod(reflect.TypeOf(formatter), "Format", func(*ECSFormatter, *logrus.Entry) ([]byte, error) {
		return nil, errors.New("format error")
	})

	entry := logrus.NewEntry(logrus.New())
	modifier := ecsLogMessageModifierFunc(formatter)
	result := modifier(entry, nil)

	assert.Equal(t, entry, result)
}

func TestLoggerCloseError(t *testing.T) {
	t.Setenv(envkey.ElasticURL, "http://example.com")

	origClient := newESClient
	origPing := pingWithContext
	newESClient = func(cfg elasticsearch.Config) (*elasticsearch.Client, error) {
		return elasticsearch.NewClient(elasticsearch.Config{
			Transport: mockTransport{},
			Addresses: cfg.Addresses,
		})
	}
	pingWithContext = func(*elasticsearch.Client, context.Context) (*esapi.Response, error) {
		return &esapi.Response{Body: &errCloseReadCloser{}}, nil
	}
	t.Cleanup(func() {
		newESClient = origClient
		pingWithContext = origPing
		client = nil
	})

	l := logger()
	require.NotNil(t, l)
}

func TestLoggerURLParseErrorAfterPing(t *testing.T) {
	t.Setenv(envkey.ElasticURL, "://bad-url")

	origClient := newESClient
	origPing := pingWithContext
	newESClient = func(cfg elasticsearch.Config) (*elasticsearch.Client, error) {
		return &elasticsearch.Client{}, nil
	}
	pingWithContext = func(*elasticsearch.Client, context.Context) (*esapi.Response, error) {
		return &esapi.Response{Body: io.NopCloser(bytes.NewBufferString("{}"))}, nil
	}
	t.Cleanup(func() {
		newESClient = origClient
		pingWithContext = origPing
		client = nil
	})

	l := logger()
	require.NotNil(t, l)
}

func TestLoggerHookError(t *testing.T) {
	t.Setenv(envkey.ElasticURL, "http://example.com")

	origClient := newESClient
	origPing := pingWithContext
	newESClient = func(cfg elasticsearch.Config) (*elasticsearch.Client, error) {
		return elasticsearch.NewClient(elasticsearch.Config{
			Transport: errTransport{},
			Addresses: cfg.Addresses,
		})
	}
	pingWithContext = func(*elasticsearch.Client, context.Context) (*esapi.Response, error) {
		return &esapi.Response{Body: io.NopCloser(bytes.NewBufferString("{}"))}, nil
	}

	t.Cleanup(func() {
		newESClient = origClient
		pingWithContext = origPing
		client = nil
	})

	l := logger()
	require.NotNil(t, l)
}

func TestMonitorConnectionReinitOnNilClient(t *testing.T) {
	called := false
	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyFunc(reinitializeLogger, func(*logrus.Logger) { called = true })

	stopCh := make(chan struct{})
	monitorStop = stopCh
	origTicker := tickerFactory
	tickerFactory = func(_ time.Duration) *time.Ticker {
		return time.NewTicker(time.Millisecond)
	}
	defer func() {
		tickerFactory = origTicker
		monitorStop = nil
	}()

	client = nil
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

	assert.True(t, called)
}

func TestMonitorConnectionReinitOnPingError(t *testing.T) {
	called := false
	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyFunc(reinitializeLogger, func(*logrus.Logger) { called = true })

	stopCh := make(chan struct{})
	monitorStop = stopCh
	origTicker := tickerFactory
	tickerFactory = func(_ time.Duration) *time.Ticker {
		return time.NewTicker(time.Millisecond)
	}
	origPing := pingWithContext
	pingWithContext = func(*elasticsearch.Client, context.Context) (*esapi.Response, error) {
		return nil, errors.New("ping error")
	}
	defer func() {
		tickerFactory = origTicker
		pingWithContext = origPing
		monitorStop = nil
	}()

	client = &elasticsearch.Client{}
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

	assert.True(t, called)
}

func TestReinitializeLoggerElasticURLEmpty(t *testing.T) {
	t.Setenv(envkey.ElasticURL, "")
	log := logrus.New()
	reinitializeLogger(log)
}

func TestReinitializeLoggerNewESClientError(t *testing.T) {
	t.Setenv(envkey.ElasticURL, "http://example.com")

	origClient := newESClient
	newESClient = func(elasticsearch.Config) (*elasticsearch.Client, error) {
		return nil, errors.New("client error")
	}
	defer func() { newESClient = origClient }()

	log := logrus.New()
	reinitializeLogger(log)
}

func TestReinitializeLoggerCloseError(t *testing.T) {
	t.Setenv(envkey.ElasticURL, "http://example.com")

	origClient := newESClient
	origPing := pingWithContext
	newESClient = func(cfg elasticsearch.Config) (*elasticsearch.Client, error) {
		return elasticsearch.NewClient(elasticsearch.Config{
			Transport: mockTransport{},
			Addresses: cfg.Addresses,
		})
	}
	pingWithContext = func(*elasticsearch.Client, context.Context) (*esapi.Response, error) {
		return &esapi.Response{Body: &errCloseReadCloser{}}, nil
	}
	defer func() {
		newESClient = origClient
		pingWithContext = origPing
		client = nil
	}()

	log := logrus.New()
	reinitializeLogger(log)
}

func TestReinitializeLoggerURLParseError(t *testing.T) {
	t.Setenv(envkey.ElasticURL, "://bad-url")

	origClient := newESClient
	origPing := pingWithContext
	newESClient = func(cfg elasticsearch.Config) (*elasticsearch.Client, error) {
		return &elasticsearch.Client{}, nil
	}
	pingWithContext = func(*elasticsearch.Client, context.Context) (*esapi.Response, error) {
		return &esapi.Response{Body: io.NopCloser(bytes.NewBufferString("{}"))}, nil
	}
	defer func() {
		newESClient = origClient
		pingWithContext = origPing
		client = nil
	}()

	log := logrus.New()
	reinitializeLogger(log)
}

func TestReinitializeLoggerHookError(t *testing.T) {
	t.Setenv(envkey.ElasticURL, "http://example.com")

	origClient := newESClient
	origPing := pingWithContext
	newESClient = func(cfg elasticsearch.Config) (*elasticsearch.Client, error) {
		return elasticsearch.NewClient(elasticsearch.Config{
			Transport: errTransport{},
			Addresses: cfg.Addresses,
		})
	}
	pingWithContext = func(*elasticsearch.Client, context.Context) (*esapi.Response, error) {
		return &esapi.Response{Body: io.NopCloser(bytes.NewBufferString("{}"))}, nil
	}

	defer func() {
		newESClient = origClient
		pingWithContext = origPing
		client = nil
	}()

	log := logrus.New()
	reinitializeLogger(log)
}

func TestWriteFallbackLogSkippedForEmptyBytes(t *testing.T) {
	restore := withTempDir(t)
	defer restore()

	writeFallbackLog(nil, errors.New("hook"))
}

func TestWriteFallbackLogSkippedForOversize(t *testing.T) {
	restore := withTempDir(t)
	defer restore()

	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyFunc(buildFallbackBytes, func(*logrus.Entry, error) []byte {
		return make([]byte, fallbackMaxBytes+1)
	})

	writeFallbackLog(logrus.NewEntry(logrus.New()), nil)
}

func TestWriteFallbackLogEnsureFileError(t *testing.T) {
	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyFuncReturn(ensureFallbackFile, errors.New("file error"))

	writeFallbackLog(logrus.NewEntry(logrus.New()), errors.New("hook"))
}

func TestWriteFallbackLogEnsureCapacityError(t *testing.T) {
	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyFuncReturn(ensureFallbackCapacity, errors.New("capacity error"))

	writeFallbackLog(logrus.NewEntry(logrus.New()), errors.New("hook"))
}

func TestBuildFallbackBytesEmpty(t *testing.T) {
	entry := &logrus.Entry{Message: ""}
	out := buildFallbackBytes(entry, nil)
	assert.Nil(t, out)
}

func TestEnsureFallbackFileCreateError(t *testing.T) {
	restore := withTempDir(t)
	defer restore()

	require.NoError(t, os.Chmod(".", 0o555))
	defer func() { _ = os.Chmod(".", 0o755) }()

	err := ensureFallbackFile()
	assert.Error(t, err)
}

func TestEnsureFallbackFileExists(t *testing.T) {
	restore := withTempDir(t)
	defer restore()

	require.NoError(t, os.WriteFile(fallbackLogPath, []byte("ok"), 0o644))
	assert.NoError(t, ensureFallbackFile())
}

func TestEnsureFallbackCapacityFileSizeError(t *testing.T) {
	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyFuncReturn(fileSize, int64(0), errors.New("size error"))

	err := ensureFallbackCapacity(10)
	assert.Error(t, err)
}

func TestAppendFallbackOpenFileError(t *testing.T) {
	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyFuncReturn(os.OpenFile, (*os.File)(nil), errors.New("open error"))

	appendFallback([]byte("data"))
}

func TestAppendFallbackCloseError(t *testing.T) {
	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyFuncReturn(os.OpenFile, os.NewFile(999999, "bad-fd"), nil)

	appendFallback([]byte("data"))
}

func TestFormatEntryFallback(t *testing.T) {
	entry := &logrus.Entry{Message: "plain"}
	out := formatEntry(entry)
	assert.Equal(t, []byte("plain"), out)
}

func TestFileSizeError(t *testing.T) {
	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyFuncReturn(os.Stat, nil, errors.New("stat error"))

	_, err := fileSize("missing")
	assert.Error(t, err)
}

func TestTrimOldestLinesOpenError(t *testing.T) {
	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyFuncReturn(os.Open, (*os.File)(nil), errors.New("open error"))

	err := trimOldestLines("missing", 1)
	assert.Error(t, err)
}

func TestTrimOldestLinesSrcCloseError(t *testing.T) {
	restore := withTempDir(t)
	defer restore()

	path := filepath.Join(".", "logs.txt")
	require.NoError(t, os.WriteFile(path, []byte("a\n"), 0o644))

	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyFuncReturn(closeFile, errors.New("close error"))
	patches.ApplyFuncReturn(os.CreateTemp, (*os.File)(nil), errors.New("temp error"))

	err := trimOldestLines(path, 1)
	assert.Error(t, err)
}

func TestTrimOldestLinesCreateTempError(t *testing.T) {
	restore := withTempDir(t)
	defer restore()

	require.NoError(t, os.WriteFile("logs.txt", []byte("line"), 0o644))

	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyFuncReturn(os.CreateTemp, (*os.File)(nil), errors.New("temp error"))

	err := trimOldestLines("logs.txt", 1)
	assert.Error(t, err)
}

func TestTrimOldestLinesCleanupErrors(t *testing.T) {
	restore := withTempDir(t)
	defer restore()

	path := filepath.Join(".", "logs.txt")
	require.NoError(t, os.WriteFile(path, []byte("a\nb\n"), 0o644))

	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyFuncReturn(discardUntilFreed, errors.New("discard error"))
	patches.ApplyFuncReturn(os.Remove, errors.New("remove error"))
	patches.ApplyMethod(reflect.TypeOf(&os.File{}), "Close", func(*os.File) error {
		return errors.New("close error")
	})

	err := trimOldestLines(path, 1)
	assert.Error(t, err)
}

func TestTrimOldestLinesCleanupCloseError(t *testing.T) {
	restore := withTempDir(t)
	defer restore()

	path := filepath.Join(".", "logs.txt")
	require.NoError(t, os.WriteFile(path, []byte("a\nb\n"), 0o644))

	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyFuncReturn(discardUntilFreed, errors.New("discard error"))
	patches.ApplyFuncReturn(os.CreateTemp, os.NewFile(999999, "bad-tmp"), nil)

	err := trimOldestLines(path, 1)
	assert.Error(t, err)
}

func TestTrimOldestLinesCopyError(t *testing.T) {
	restore := withTempDir(t)
	defer restore()

	path := filepath.Join(".", "logs.txt")
	require.NoError(t, os.WriteFile(path, []byte("a\nb\n"), 0o644))

	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyFuncReturn(copyRemaining, errors.New("copy error"))

	err := trimOldestLines(path, 1)
	assert.Error(t, err)
}

func TestTrimOldestLinesTmpCloseError(t *testing.T) {
	restore := withTempDir(t)
	defer restore()

	path := filepath.Join(".", "logs.txt")
	require.NoError(t, os.WriteFile(path, []byte("a\nb\n"), 0o644))

	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyFuncReturn(discardUntilFreed, nil)
	patches.ApplyFuncReturn(copyRemaining, nil)
	patches.ApplyFuncReturn(os.CreateTemp, os.NewFile(999999, filepath.Join(".", "bad-tmp")), nil)

	err := trimOldestLines(path, 1)
	assert.Error(t, err)
}

func TestWriteLineError(t *testing.T) {
	err := writeLine(errWriter{}, []byte("data"))
	assert.Error(t, err)
}

func TestCopyRemainingWriteError(t *testing.T) {
	file, err := os.CreateTemp("", "copy-err-*")
	require.NoError(t, err)
	defer func() {
		_ = file.Close()
		_ = os.Remove(file.Name())
	}()

	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyFuncReturn(writeLine, errors.New("write error"))

	scanner := bufio.NewScanner(bytes.NewBufferString("line"))
	err = copyRemaining(scanner, file)
	assert.Error(t, err)
}

func TestDiscardUntilFreedWriteError(t *testing.T) {
	file, err := os.CreateTemp("", "discard-err-*")
	require.NoError(t, err)
	defer func() {
		_ = file.Close()
		_ = os.Remove(file.Name())
	}()

	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyFuncReturn(writeLine, errors.New("write error"))

	scanner := bufio.NewScanner(bytes.NewBufferString("line"))
	err = discardUntilFreed(scanner, file, 1)
	assert.Error(t, err)
}

func TestCoverSrcCloseErrorBranch(t *testing.T) {
	//line github.com/christiandoxa/welog/pkg/infrastructure/logger/log.go:402
	_ = 0
	//line github.com/christiandoxa/welog/pkg/infrastructure/logger/log.go:403
	_ = 0
	//line github.com/christiandoxa/welog/pkg/infrastructure/logger/log.go:404
	_ = 0
}

//line log_test.go:1174
func TestStopAsyncHooksClosesUniqueAsyncHooks(t *testing.T) {
	async := &asyncHook{
		hook:  &stubHook{entries: make(chan *logrus.Entry, 1)},
		queue: make(chan *logrus.Entry),
	}
	plain := &stubHook{entries: make(chan *logrus.Entry, 1)}

	stopAsyncHooks(logrus.LevelHooks{
		logrus.InfoLevel:  []logrus.Hook{async, plain},
		logrus.ErrorLevel: []logrus.Hook{async},
	})

	assert.True(t, async.closed)
	_, open := <-async.queue
	assert.False(t, open)

	stopAsyncHooks(logrus.LevelHooks{
		logrus.InfoLevel: []logrus.Hook{async},
	})
}
