// Package welog provides middleware and logging utilities for Fiber web applications.
// It integrates with logrus for structured logging and supports detailed request
// and response logging for HTTP requests.
package welog

import (
	"github.com/christiandoxa/welog/pkg/constant/generalkey"
	"github.com/christiandoxa/welog/pkg/infrastructure/logger"
	"github.com/goccy/go-json"
	"github.com/gofiber/fiber/v2"
	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
	"os/user"
	"time"
)

// init loads environment variables from a .env file. If the .env file is not found
// or cannot be loaded, the application will terminate with a fatal error.
func init() {
	// Load environment variables from .env file
	if err := godotenv.Load(); err != nil {
		logrus.Fatal("Error loading .env file")
	}
}

// NewFiber returns a new Fiber middleware handler that injects a logger with
// request-specific fields into the context of each request. It also logs the
// request and response details after each request is processed.
func NewFiber(requestIDContextName ...string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		contextName := "requestid"

		if len(requestIDContextName) > 0 && requestIDContextName[0] != "" {
			contextName = requestIDContextName[0]
		}

		// Add logger and client log fields to the context
		c.Locals(generalkey.Logger, logger.Logger().WithField(generalkey.RequestID, c.Locals(contextName)))
		c.Locals(generalkey.ClientLog, []logrus.Fields{})

		reqTime := time.Now()

		// Process the next handler in the chain
		if err := c.Next(); err != nil {
			logFiber(c, reqTime, contextName)
			return err
		}

		// Log request and response details
		logFiber(c, reqTime, contextName)

		return nil
	}
}

// logFiber logs the details of a request and response in the Fiber context.
// It captures various request/response information including headers, body, status, and latency.
func logFiber(c *fiber.Ctx, reqTime time.Time, contextName string) {
	latency := time.Since(reqTime)

	currentUser, err := user.Current()
	if err != nil {
		c.Locals(generalkey.Logger).(*logrus.Entry).Error(err)
		currentUser = &user.User{Username: "unknown"}
	}

	var request, response logrus.Fields

	// Unmarshal request and response bodies into logrus fields
	_ = json.Unmarshal(c.Body(), &request)
	_ = json.Unmarshal(c.Response().Body(), &response)

	clientLog := c.Locals(generalkey.ClientLog).([]logrus.Fields)

	// Log the request and response details
	c.Locals(generalkey.Logger).(*logrus.Entry).WithFields(logrus.Fields{
		"requestAgent":         c.Get("User-Agent"),
		"requestBody":          request,
		"requestBodyString":    string(c.Body()),
		"requestContentType":   c.Get("Content-Type"),
		"requestHeader":        c.GetReqHeaders(),
		"requestHostName":      c.Hostname(),
		"requestId":            c.Locals(contextName),
		"requestIp":            c.IP(),
		"requestMethod":        c.Method(),
		"requestProtocol":      c.Protocol(),
		"requestTimestamp":     reqTime.Format(time.RFC3339Nano),
		"requestUrl":           c.BaseURL() + c.OriginalURL(),
		"responseBody":         response,
		"responseBodyString":   string(c.Response().Body()),
		"responseHeaderString": c.Response().Header.String(),
		"responseLatency":      latency.String(),
		"responseStatus":       c.Response().StatusCode(),
		"responseTimestamp":    reqTime.Add(latency).Format(time.RFC3339Nano),
		"responseUser":         currentUser.Username,
		"target":               clientLog,
	}).Info()
}

// LogFiberClient logs details about an external HTTP request made by the Fiber application.
// It records the request and response data, headers, method, status, and latency into the context.
func LogFiberClient(c *fiber.Ctx, url string, method string, contentType string, header map[string]interface{}, body []byte, response []byte, status int, start time.Time, elapsed time.Duration) {
	var requestField, responseField logrus.Fields

	// Unmarshal request and response bodies into logrus fields
	_ = json.Unmarshal(body, &requestField)
	_ = json.Unmarshal(response, &responseField)

	// Prepare log data for the external request
	logData := logrus.Fields{
		"targetRequestHeader":      header,
		"targetRequestBody":        requestField,
		"targetRequestBodyString":  string(body),
		"targetRequestContentType": contentType,
		"targetRequestMethod":      method,
		"targetRequestTimestamp":   start.Format(time.RFC3339Nano),
		"targetRequestURL":         url,
		"targetResponseBody":       responseField,
		"targetResponseBodyString": string(response),
		"targetResponseLatency":    elapsed.String(),
		"targetResponseStatus":     status,
		"targetResponseTimestamp":  start.Add(elapsed).Format(time.RFC3339Nano),
	}

	// Append log data to the client log context
	clientLog := c.Locals(generalkey.ClientLog).([]logrus.Fields)
	c.Locals(generalkey.ClientLog, append(clientLog, logData))
}
