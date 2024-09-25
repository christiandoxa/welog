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

// NewFiber creates a new Fiber middleware handler that sets up context for
// request logging and error handling. It adds request-specific loggers and
// context fields to each incoming request. The middleware handles errors
// using a custom or default error handler and logs request details.
//
// Parameters:
//   - config: A fiber.Config object that contains Fiber configuration,
//     including custom error handlers if any.
//   - requestIDContextName (optional): A variadic string parameter that
//     specifies the context key name for the request ID. If not provided,
//     the default key "requestid" is used.
//
// Returns:
//   - fiber.Handler: A Fiber handler function that can be used as middleware
//     in a Fiber application.
//
// Usage:
//
//	app := fiber.New()
//	app.Use(NewFiber(config, "customRequestID"))
//
// Behavior:
//   - Sets up a logger and client log fields in the context using the request ID.
//   - Logs request and response details along with any errors encountered during
//     request processing.
//   - Handles errors using the custom error handler if provided in the config,
//     otherwise uses the default Fiber error handler.
func NewFiber(config fiber.Config, requestIDContextName ...string) fiber.Handler {
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
			errorHandler := fiber.DefaultErrorHandler

			// Use custom error handler if provided
			if config.ErrorHandler != nil {
				errorHandler = config.ErrorHandler
			}

			// Log the error
			if err = errorHandler(c, err); err != nil {
				logFiber(c, reqTime, contextName)
				return err
			}
		}

		// Log request and response details
		logFiber(c, reqTime, contextName)

		return nil
	}
}

// logFiber logs the details of a request and response in the Fiber context.
// It captures various request/response information including headers, body, status, and latency.
func logFiber(c *fiber.Ctx, requestTime time.Time, contextName string) {
	latency := time.Since(requestTime)

	// Get the current user
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
		"requestTimestamp":     requestTime.Format(time.RFC3339Nano),
		"requestUrl":           c.BaseURL() + c.OriginalURL(),
		"responseBody":         response,
		"responseBodyString":   string(c.Response().Body()),
		"responseHeaderString": c.Response().Header.String(),
		"responseLatency":      latency.String(),
		"responseStatus":       c.Response().StatusCode(),
		"responseTimestamp":    requestTime.Add(latency).Format(time.RFC3339Nano),
		"responseUser":         currentUser.Username,
		"target":               clientLog,
	}).Info()
}

// LogFiberClient logs the details of an HTTP client request and its corresponding response within a Fiber context.
//
// This function extracts and logs various aspects of the HTTP request and response, including headers,
// bodies, status codes, timestamps, and latency. It unmarshal the JSON-encoded request and response
// bodies into structured log fields and appends this information to the client's log context.
//
// Parameters:
//   - c: The Fiber context (`*fiber.Ctx`) in which the logging occurs.
//   - requestURL: The URL of the HTTP request as a string.
//   - requestMethod: The HTTP method used for the request (e.g., "GET", "POST").
//   - requestContentType: The Content-Type header of the request as a string.
//   - requestHeader: A map containing the request headers (`map[string]interface{}`).
//   - requestBody: The body of the HTTP request as a byte slice (`[]byte`).
//   - responseHeader: A map containing the response headers (`map[string]interface{}`).
//   - responseBody: The body of the HTTP response as a byte slice (`[]byte`).
//   - responseStatus: The HTTP status code of the response as an integer.
//   - requestTime: A `time.Time` object representing when the request was made.
//   - responseLatency: A `time.Duration` representing the time taken to receive the response.
//
// Behavior:
//   - Attempts to unmarshal the JSON-encoded `requestBody` and `responseBody` into `logrus.Fields`.
//     Errors during unmarshalling are ignored.
//   - Constructs a `logrus.Fields` map containing detailed information about the request and response.
//   - Retrieves the existing client log from the Fiber context, appends the new log data, and stores it back
//     in the context under the key specified by `generalkey.ClientLog`.
//
// Example:
//
//	LogFiberClient(c, "https://api.example.com/data", "POST", "application/json", reqHeaders, reqBody, respHeaders, respBody, 200, time.Now(), time.Since(start))
func LogFiberClient(
	c *fiber.Ctx,
	requestURL string,
	requestMethod string,
	requestContentType string,
	requestHeader map[string]interface{},
	requestBody []byte,
	responseHeader map[string]interface{},
	responseBody []byte,
	responseStatus int,
	requestTime time.Time,
	responseLatency time.Duration,
) {
	var requestField, responseField logrus.Fields

	// Unmarshal request and response bodies into logrus fields
	_ = json.Unmarshal(requestBody, &requestField)
	_ = json.Unmarshal(responseBody, &responseField)

	// Prepare log data for the external request
	logData := logrus.Fields{
		"targetRequestBody":        requestField,
		"targetRequestBodyString":  string(requestBody),
		"targetRequestContentType": requestContentType,
		"targetRequestHeader":      requestHeader,
		"targetRequestMethod":      requestMethod,
		"targetRequestTimestamp":   requestTime.Format(time.RFC3339Nano),
		"targetRequestURL":         requestURL,
		"targetResponseBody":       responseField,
		"targetResponseBodyString": string(responseBody),
		"targetResponseHeader":     responseHeader,
		"targetResponseLatency":    responseLatency.String(),
		"targetResponseStatus":     responseStatus,
		"targetResponseTimestamp":  requestTime.Add(responseLatency).Format(time.RFC3339Nano),
	}

	// Append log data to the client log context
	clientLog := c.Locals(generalkey.ClientLog).([]logrus.Fields)
	c.Locals(generalkey.ClientLog, append(clientLog, logData))
}
