// Package logger provides a logging utility that integrates with ElasticSearch and
// uses the logrus package for structured logging. This package initializes a singleton
// logger instance that can be used throughout an application for logging events.
package logger

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/christiandoxa/welog/pkg/constant/envkey"
	"github.com/elastic/go-elasticsearch/v8"
	"github.com/goccy/go-json"
	"github.com/sirupsen/logrus"
	"go.elastic.co/ecslogrus"
	"gopkg.in/go-extras/elogrus.v8"
)

var (
	client   *elasticsearch.Client // ElasticSearch client for sending log data
	instance *logrus.Logger        // Singleton instance of the logger
	once     sync.Once             // Ensures the logger is initialized only once
	mutex    sync.Mutex            // Protects access to the logger instance and client
)

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
	log := logrus.New()
	log.SetFormatter(&ecslogrus.Formatter{})
	log.SetReportCaller(true)

	elasticURL := os.Getenv(envkey.ElasticURL)
	if elasticURL == "" {
		log.Error("ElasticURL is not set")
		return log
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
		log.Error("Failed to create ES client: ", err)
		return log
	}

	// Ping with a 2-second timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	res, err := c.Ping(c.Ping.WithContext(ctx))
	if err != nil {
		log.Warn("Elasticsearch ping failed, skipping ES hook: ", err)
		client = nil
		return log
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Error(err)
		}
	}(res.Body)

	client = c

	parsedURL, err := url.Parse(elasticURL)
	if err != nil {
		log.Error(err)
		return log
	}
	host := parsedURL.Hostname()

	hook, err := elogrus.NewElasticHookWithFunc(client, host, logrus.TraceLevel, indexNameFunc)
	if err != nil {
		log.Error(err)
		return log
	}
	hook.MessageModifierFunc = ecsLogMessageModifierFunc(&ecslogrus.Formatter{})
	log.Hooks.Add(hook)

	return log
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
	log.Hooks.Add(hook)
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
