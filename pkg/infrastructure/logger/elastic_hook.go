package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/esapi"
	"github.com/sirupsen/logrus"
)

var errCannotCreateIndex = fmt.Errorf("cannot create index")

const elasticRequestTimeout = 5 * time.Second

// IndexNameFunc resolves the Elasticsearch index name at write time.
type IndexNameFunc func() string

// ModifyMessageFunc customizes the document sent to Elasticsearch.
type ModifyMessageFunc func(entry *logrus.Entry, message *Message) any

// ElasticHook is a logrus hook that writes entries to Elasticsearch.
type ElasticHook struct {
	client *elasticsearch.Client
	host   string
	index  IndexNameFunc
	levels []logrus.Level

	MessageModifierFunc ModifyMessageFunc
}

// Message represents the default payload written to Elasticsearch.
type Message struct {
	Host      string        `json:"host,omitempty"`
	Timestamp string        `json:"@timestamp"`
	File      string        `json:"file,omitempty"`
	Func      string        `json:"func,omitempty"`
	Message   string        `json:"message,omitempty"`
	Data      logrus.Fields `json:"data,omitempty"`
	Level     string        `json:"level,omitempty"`
}

// NewElasticHookWithFunc creates a hook with a dynamic index resolver.
func NewElasticHookWithFunc(client *elasticsearch.Client, host string, level logrus.Level, indexFunc IndexNameFunc) (*ElasticHook, error) {
	if err := ensureIndexExists(client, indexFunc); err != nil {
		return nil, err
	}

	return &ElasticHook{
		client: client,
		host:   host,
		index:  indexFunc,
		levels: enabledLevels(level),
	}, nil
}

func (hook *ElasticHook) Levels() []logrus.Level {
	return hook.levels
}

func (hook *ElasticHook) Fire(entry *logrus.Entry) error {
	data, err := json.Marshal(hook.createMessage(entry))
	if err != nil {
		return err
	}

	req := esapi.IndexRequest{
		Index: hook.index(),
		Body:  bytes.NewReader(data),
	}

	ctx, cancel := context.WithTimeout(context.Background(), elasticRequestTimeout)
	defer cancel()
	res, err := req.Do(ctx, hook.client)
	if err != nil {
		return err
	}
	closeBody(res)

	return nil
}

func (hook *ElasticHook) createMessage(entry *logrus.Entry) any {
	if e, ok := entry.Data[logrus.ErrorKey]; ok && e != nil {
		if err, ok := e.(error); ok {
			entry.Data[logrus.ErrorKey] = err.Error()
		}
	}

	var file string
	var function string
	if entry.HasCaller() {
		file = fmt.Sprintf("%s:%d", entry.Caller.File, entry.Caller.Line)
		function = entry.Caller.Function
	}

	msg := &Message{
		Host:      hook.host,
		Timestamp: entry.Time.UTC().Format(time.RFC3339Nano),
		File:      file,
		Func:      function,
		Message:   entry.Message,
		Data:      entry.Data,
		Level:     strings.ToUpper(entry.Level.String()),
	}

	if hook.MessageModifierFunc != nil {
		return hook.MessageModifierFunc(entry, msg)
	}

	return msg
}

func enabledLevels(level logrus.Level) []logrus.Level {
	levels := make([]logrus.Level, 0, 7)
	for _, candidate := range []logrus.Level{
		logrus.PanicLevel,
		logrus.FatalLevel,
		logrus.ErrorLevel,
		logrus.WarnLevel,
		logrus.InfoLevel,
		logrus.DebugLevel,
		logrus.TraceLevel,
	} {
		if candidate <= level {
			levels = append(levels, candidate)
		}
	}
	return levels
}

func ensureIndexExists(client *elasticsearch.Client, indexFunc IndexNameFunc) error {
	indexName := indexFunc()

	ctx, cancel := context.WithTimeout(context.Background(), elasticRequestTimeout)
	defer cancel()

	existsResp, err := client.Indices.Exists([]string{indexName}, client.Indices.Exists.WithContext(ctx))
	if err != nil {
		return err
	}
	closeBody(existsResp)

	if existsResp.StatusCode != http.StatusNotFound {
		if existsResp.IsError() {
			return fmt.Errorf("cannot check index: %s", existsResp.Status())
		}
		return nil
	}

	createResp, err := client.Indices.Create(indexName, client.Indices.Create.WithContext(ctx))
	if err != nil {
		return errCannotCreateIndex
	}
	defer closeBody(createResp)

	if createResp.IsError() {
		return errCannotCreateIndex
	}

	return nil
}

func closeBody(res *esapi.Response) {
	if res == nil || res.Body == nil {
		return
	}
	_ = res.Body.Close()
}
