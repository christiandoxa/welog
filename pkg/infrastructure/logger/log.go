// Package logger provides a logging utility that integrates with ElasticSearch and
// uses the logrus package for structured logging. This package initializes a singleton
// logger instance that can be used throughout an application for logging events.
package logger

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/christiandoxa/welog/pkg/constant/envkey"
	"github.com/elastic/go-elasticsearch/v8"
	"github.com/goccy/go-json"
	"github.com/gofiber/fiber/v2/log"
	"github.com/sirupsen/logrus"
	"go.elastic.co/ecslogrus"
	"gopkg.in/go-extras/elogrus.v8"
)

var (
	client   *elasticsearch.Client // ElasticSearch client for sending log data
	instance *logrus.Logger        // Singleton instance of the logger
	once     sync.Once             // Ensures the logger is initialized only once
	mutex    sync.Mutex            // Protects access to the logger instance and client
	fileLock sync.Mutex            // Protects fallback log file operations
)

const asyncHookBufferSize = 256 // buffered channel size to avoid blocking during outages
const fallbackLogPath = "logs.txt"
const fallbackMaxBytes int64 = 1 << 30 // 1GB

// ecsLogMessageModifierFunc returns a function that modifies log messages
// using the ECS log formatter. If an error occurs during formatting, the original
// log entry is preserved.
func ecsLogMessageModifierFunc(formatter *ecslogrus.Formatter) func(*logrus.Entry, *elogrus.Message) any {
	return func(entry *logrus.Entry, _ *elogrus.Message) any {
		data, err := formatter.Format(entry)
		if err != nil {
			return entry
		}
		return json.RawMessage(data)
	}
}

// indexNameFunc generates the index name for ElasticSearch by concatenating the
// environment-specific index prefix and the current date in YYYY-MM-DD format.
func indexNameFunc() string {
	return fmt.Sprint(os.Getenv(envkey.ElasticIndex), "-", time.Now().Format("2006-01-02"))
}

// logger initializes and configures a new instance of the logrus.Logger. It sets up
// the logger with ECS formatting and integrates it with ElasticSearch for centralized logging.
func logger() *logrus.Logger {
	logInstance := logrus.New()
	logInstance.SetFormatter(&ecslogrus.Formatter{})
	logInstance.SetReportCaller(true)

	elasticURL := os.Getenv(envkey.ElasticURL)
	if elasticURL == "" {
		logInstance.Error("ElasticURL is not set")
		return logInstance
	}

	// Configure HTTP transport with dial and header timeouts.
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ResponseHeaderTimeout: 5 * time.Second,
	}

	c, err := elasticsearch.NewClient(elasticsearch.Config{
		Addresses: []string{elasticURL},
		Username:  os.Getenv(envkey.ElasticUsername),
		Password:  os.Getenv(envkey.ElasticPassword),
		Transport: transport,
	})
	if err != nil {
		logInstance.Error("Failed to create ES client: ", err)
		return logInstance
	}

	// Ping with a 2-second timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	res, err := c.Ping(c.Ping.WithContext(ctx))
	if err != nil {
		logInstance.Warn("Elasticsearch ping failed, skipping ES hook: ", err)
		client = nil
		return logInstance
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			logInstance.Error(err)
		}
	}(res.Body)

	client = c

	parsedURL, err := url.Parse(elasticURL)
	if err != nil {
		logInstance.Error(err)
		return logInstance
	}
	host := parsedURL.Hostname()

	hook, err := elogrus.NewElasticHookWithFunc(client, host, logrus.TraceLevel, indexNameFunc)
	if err != nil {
		logInstance.Error(err)
		return logInstance
	}
	hook.MessageModifierFunc = ecsLogMessageModifierFunc(&ecslogrus.Formatter{})
	logInstance.Hooks.Add(newAsyncHook(hook))

	return logInstance
}

// monitorConnection starts a goroutine that periodically checks the connection to ElasticSearch.
// If the connection is lost, it re-initializes the ElasticSearch client and hooks.
// This ensures that even if the ElasticSearch instance is restarted, the application
// will continue to log to ElasticSearch once the connection is re-established.
func monitorConnection() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		mutex.Lock()
		if client != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_, err := client.Ping(client.Ping.WithContext(ctx))
			cancel()
			if err != nil {
				reinitializeLogger(instance)
			}
		} else {
			reinitializeLogger(instance)
		}
		mutex.Unlock()
	}
}

// reinitializeLogger reinitialize the ElasticSearch client and logger if the connection
// to ElasticSearch is lost. This function is used by the connection monitoring goroutine.
// It pings the ElasticSearch server and reinitialize the logger if the connection is
// successful.
func reinitializeLogger(log *logrus.Logger) {
	elasticURL := os.Getenv(envkey.ElasticURL)
	if elasticURL == "" {
		log.Error("ElasticURL is not set")
		return
	}

	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ResponseHeaderTimeout: 5 * time.Second,
	}

	c, err := elasticsearch.NewClient(elasticsearch.Config{
		Addresses: []string{elasticURL},
		Username:  os.Getenv(envkey.ElasticUsername),
		Password:  os.Getenv(envkey.ElasticPassword),
		Transport: transport,
	})
	if err != nil {
		log.Error("Failed to create ES client during reinit: ", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	res, err := c.Ping(c.Ping.WithContext(ctx))
	if err != nil {
		log.Warn("Elasticsearch ping failed during reinit, retaining old client: ", err)
		return
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Error(err)
		}
	}(res.Body)

	client = c
	log.ReplaceHooks(make(logrus.LevelHooks))

	parsedURL, err := url.Parse(elasticURL)
	if err != nil {
		log.Error(err)
		return
	}
	host := parsedURL.Hostname()

	hook, err := elogrus.NewElasticHookWithFunc(client, host, logrus.TraceLevel, indexNameFunc)
	if err != nil {
		log.Error(err)
		return
	}
	hook.MessageModifierFunc = ecsLogMessageModifierFunc(&ecslogrus.Formatter{})
	log.Hooks.Add(newAsyncHook(hook))
}

// Logger returns the singleton instance of the logrus.Logger. It initializes the logger
// on the first call and starts a background goroutine to monitor the ElasticSearch connection.
func Logger() *logrus.Logger {
	once.Do(func() {
		mutex.Lock()
		defer mutex.Unlock()
		instance = logger()
		go monitorConnection()
	})

	mutex.Lock()
	defer mutex.Unlock()
	return instance
}

// asyncHook wraps a logrus.Hook and processes Fire calls asynchronously using a buffered channel.
// This prevents request logging from blocking when Elasticsearch is slow or unavailable.
type asyncHook struct {
	hook  logrus.Hook
	queue chan *logrus.Entry
}

func newAsyncHook(hook logrus.Hook) *asyncHook {
	h := &asyncHook{
		hook:  hook,
		queue: make(chan *logrus.Entry, asyncHookBufferSize),
	}
	go h.worker()
	return h
}

func (h *asyncHook) Levels() []logrus.Level {
	return h.hook.Levels()
}

func (h *asyncHook) Fire(entry *logrus.Entry) error {
	entryCopy := duplicateEntry(entry)
	select {
	case h.queue <- entryCopy:
	default:
		// drop log if buffer is full to avoid blocking
	}
	return nil
}

func (h *asyncHook) worker() {
	for e := range h.queue {
		if err := h.hook.Fire(e); err != nil {
			writeFallbackLog(e, err)
		}
	}
}

// duplicateEntry creates a shallow copy of the logrus.Entry to safely use it asynchronously.
func duplicateEntry(entry *logrus.Entry) *logrus.Entry {
	clone := entry.Dup()
	clone.Time = entry.Time
	clone.Level = entry.Level
	clone.Message = entry.Message
	clone.Caller = entry.Caller
	clone.Buffer = copyBuffer(entry.Buffer)
	return clone
}

func copyBuffer(buf *bytes.Buffer) *bytes.Buffer {
	if buf == nil {
		return nil
	}
	dup := bytes.NewBuffer(make([]byte, 0, buf.Len()))
	_, _ = dup.Write(buf.Bytes())
	return dup
}

// writeFallbackLog persists a failed hook entry to a local file with a 1GB size cap.
// Oldest lines are removed to make space for new entries. This keeps logging non-blocking
// even when Elasticsearch is unreachable.
func writeFallbackLog(entry *logrus.Entry, hookErr error) {
	logBytes := buildFallbackBytes(entry, hookErr)
	if len(logBytes) == 0 || int64(len(logBytes)) > fallbackMaxBytes {
		return
	}

	fileLock.Lock()
	defer fileLock.Unlock()

	if err := ensureFallbackFile(); err != nil {
		return
	}
	if err := ensureFallbackCapacity(int64(len(logBytes))); err != nil {
		return
	}
	appendFallback(logBytes)
}

func buildFallbackBytes(entry *logrus.Entry, hookErr error) []byte {
	logBytes := formatEntry(entry)
	if hookErr != nil {
		logBytes = append(logBytes, []byte(fmt.Sprintf(" hook_error=%v", hookErr))...)
	}
	if len(logBytes) == 0 {
		return nil
	}
	if logBytes[len(logBytes)-1] != '\n' {
		logBytes = append(logBytes, '\n')
	}
	return logBytes
}

func ensureFallbackFile() error {
	if _, err := os.Stat(fallbackLogPath); os.IsNotExist(err) {
		f, createErr := os.Create(fallbackLogPath)
		if createErr != nil {
			return createErr
		}
		return f.Close()
	}
	return nil
}

func ensureFallbackCapacity(additional int64) error {
	size, err := fileSize(fallbackLogPath)
	if err != nil {
		return err
	}
	required := size + additional
	if required <= fallbackMaxBytes {
		return nil
	}
	return trimOldestLines(fallbackLogPath, required-fallbackMaxBytes)
}

func appendFallback(logBytes []byte) {
	f, err := os.OpenFile(fallbackLogPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer func(f *os.File) {
		err := f.Close()
		if err != nil {
			log.Error(err)
		}
	}(f)
	_, _ = f.Write(logBytes) // best-effort; ignore errors to keep non-blocking
}

// formatEntry renders a logrus.Entry using its formatter; falls back to message only.
func formatEntry(entry *logrus.Entry) []byte {
	if entry == nil {
		return nil
	}
	if entry.Logger != nil && entry.Logger.Formatter != nil {
		if data, err := entry.Logger.Formatter.Format(entry); err == nil {
			return data
		}
	}
	return []byte(entry.Message)
}

func fileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// trimOldestLines removes the oldest lines until at least bytesToFree bytes are freed.
func trimOldestLines(path string, bytesToFree int64) error {
	src, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func(src *os.File) {
		err := src.Close()
		if err != nil {
			log.Error(err)
		}
	}(src)

	tmp, err := os.CreateTemp(filepath.Dir(path), "logs-tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	scanner := newScanner(src)

	cleanup := func() {
		err := tmp.Close()
		if err != nil {
			log.Error(err)
		}
		err = os.Remove(tmpPath)
		if err != nil {
			log.Error(err)
		}
	}

	if err := discardUntilFreed(scanner, tmp, bytesToFree); err != nil {
		cleanup()
		return err
	}

	if err := copyRemaining(scanner, tmp); err != nil {
		cleanup()
		return err
	}
	if err := scanner.Err(); err != nil {
		cleanup()
		return err
	}

	if err := tmp.Close(); err != nil {
		err := os.Remove(tmpPath)
		if err != nil {
			log.Error(err)
		}
		return err
	}

	return os.Rename(tmpPath, path)
}

func discardUntilFreed(scanner *bufio.Scanner, tmp *os.File, bytesToFree int64) error {
	var removed int64
	for scanner.Scan() {
		line := scanner.Bytes()
		removed += int64(len(line)) + 1 // + newline
		if removed >= bytesToFree {
			return writeLine(tmp, line)
		}
	}
	return nil
}

func copyRemaining(scanner *bufio.Scanner, tmp *os.File) error {
	for scanner.Scan() {
		if err := writeLine(tmp, scanner.Bytes()); err != nil {
			return err
		}
	}
	return nil
}

func newScanner(r io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	return scanner
}

func writeLine(w io.Writer, line []byte) error {
	if _, err := w.Write(line); err != nil {
		return err
	}
	_, err := w.Write([]byte("\n"))
	return err
}
