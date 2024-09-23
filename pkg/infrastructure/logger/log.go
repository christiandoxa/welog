// Package logger provides a logging utility that integrates with ElasticSearch and
// uses the logrus package for structured logging. This package initializes a singleton
// logger instance that can be used throughout an application for logging events.
package logger

import (
	"fmt"
	"github.com/christiandoxa/welog/pkg/constant/envkey"
	"github.com/elastic/go-elasticsearch/v8"
	"github.com/goccy/go-json"
	"github.com/sirupsen/logrus"
	"go.elastic.co/ecslogrus"
	"gopkg.in/go-extras/elogrus.v8"
	"net/url"
	"os"
	"sync"
	"time"
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
		var data json.RawMessage

		data, err := formatter.Format(entry)
		if err != nil {
			return entry // in case of an error just preserve the original entry
		}

		return data
	}
}

// indexNameFunc generates the index name for ElasticSearch by concatenating the
// environment-specific index prefix and the current month and year in YYYY-MM format.
func indexNameFunc() string {
	return fmt.Sprint(os.Getenv(envkey.ElasticIndex), "-", time.Now().Format("2006-01"))
}

// logger initializes and configures a new instance of the logrus.Logger. It sets up
// the logger with ECS formatting and integrates it with ElasticSearch for centralized logging.
func logger() *logrus.Logger {
	log := logrus.New()
	log.SetFormatter(&ecslogrus.Formatter{})
	log.SetReportCaller(true)

	elasticURL := os.Getenv(envkey.ElasticURL)
	if elasticURL == "" {
		log.Fatal("ElasticURL is not set")
	}

	c, err := elasticsearch.NewClient(elasticsearch.Config{
		Addresses: []string{elasticURL},
		Username:  os.Getenv(envkey.ElasticUsername),
		Password:  os.Getenv(envkey.ElasticPd),
	})

	if err != nil {
		log.Fatal(err)
	}

	res, err := c.Ping()
	if err != nil {
		log.Fatal(err)
	}
	if res != nil {
		_ = res.Body.Close()
	}

	client = c

	// Parse URL
	parsedURL, err := url.Parse(elasticURL)
	if err != nil {
		log.Fatal(err)
	}

	// Parse hostname
	host := parsedURL.Hostname()

	hook, err := elogrus.NewElasticHookWithFunc(client, host, logrus.TraceLevel, indexNameFunc)
	if err != nil {
		log.Fatal(err)
	}

	hook.MessageModifierFunc = ecsLogMessageModifierFunc(&ecslogrus.Formatter{})
	log.Hooks.Add(hook)

	return log
}

// monitorConnection periodically checks the connection to ElasticSearch and reinitializes
// the logger if the connection is lost. This ensures that logging continues even after
// connectivity issues.
func monitorConnection() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			mutex.Lock()
			if client != nil {
				_, err := client.Ping()
				if err != nil {
					// Re-initialize the logger
					instance = logger()
				}
			} else {
				instance = logger()
			}
			mutex.Unlock()
		}
	}
}

// Logger returns the singleton instance of the logrus.Logger. It initializes the logger
// on the first call and starts a background goroutine to monitor the ElasticSearch connection.
func Logger() *logrus.Logger {
	once.Do(func() {
		mutex.Lock()
		defer mutex.Unlock()

		instance = logger()

		go monitorConnection() // Start the connection monitoring in a separate goroutine
	})

	mutex.Lock()
	defer mutex.Unlock()

	return instance
}
