// Package welog provides middleware and logging utilities for Fiber web applications.
// It integrates with logrus for structured logging and supports detailed request
// and response logging for HTTP requests.
package welog

import (
	"bytes"
	"github.com/christiandoxa/welog/pkg/constant/generalkey"
	"github.com/christiandoxa/welog/pkg/infrastructure/logger"
	"github.com/gin-gonic/gin"
	"github.com/goccy/go-json"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/sirupsen/logrus"
	"io"
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

type responseBodyWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (w responseBodyWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
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

// NewGin returns a gin.HandlerFunc middleware that adds a request ID to the context,
// sets up a logger with the request ID, and captures the response body for logging purposes.
// It also logs the request and response details after the request is processed.
//
// The middleware performs the following actions:
// 1. Retrieves the request ID from the "X-Request-ID" header. If the header is not present, it generates a new UUID as the request ID.
// 2. Sets the request ID into the context with the key defined by `generalkey.RequestID`.
// 3. Sets a logger into the context with the request ID field for structured logging purposes.
// 4. Initializes an empty slice of logrus.Fields and sets it in the context with the key `generalkey.ClientLog`.
// 5. Wraps the response writer to capture the response body for logging.
// 6. Logs the request and response details using the `logGin` function after the request is completed.
//
// Returns:
// gin.HandlerFunc - The configured middleware handler function.
func NewGin() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader("X-Request-ID")

		if requestID == "" {
			requestID = uuid.NewString()
		}

		c.Set(generalkey.RequestID, requestID)

		c.Set(generalkey.Logger, logger.Logger().WithField(generalkey.RequestID, requestID))

		c.Set(generalkey.ClientLog, []logrus.Fields{})

		bodyBuf := &bytes.Buffer{}

		writer := responseBodyWriter{body: bodyBuf, ResponseWriter: c.Writer}

		c.Writer = writer

		requestTime := time.Now()

		c.Next()

		logGin(c, bodyBuf, requestTime)
	}
}

// logGin logs the details of an HTTP request and response using the Gin framework and Logrus.
//
// This function captures request and response data, including headers, body content, and timing
// information, and logs them with additional fields using a Logrus entry.
//
// Parameters:
//   - c: The current Gin context, which holds the request, response, and various context data.
//   - buf: A buffer that contains the response body data to be logged.
//   - requestTime: The time when the request was received, used to calculate latency.
//
// The function performs the following steps:
//  1. Calculates the latency between the request and response.
//  2. Retrieves the current user from the system.
//  3. Reads and unmarshal the request body into a logrus.Fields structure for logging.
//  4. Reads and unmarshal the response body from the buffer into a logrus.Fields structure for logging.
//  5. Retrieves additional logging fields related to the client from the Gin context.
//  6. Retrieves the logger entry from the Gin context.
//  7. Logs the request and response details with various fields including request and response headers,
//     body content, latency, and user information using the Logrus logger.
//
// Note: Errors during unmarshalling or retrieving fields are ignored and will not interrupt the logging process.
func logGin(c *gin.Context, buf *bytes.Buffer, requestTime time.Time) {
	latency := time.Since(requestTime)

	currentUser, _ := user.Current()

	var request, response logrus.Fields

	bodyBytes, _ := io.ReadAll(c.Request.Body)
	_ = json.Unmarshal(bodyBytes, &request)

	responseBody := buf.Bytes()
	_ = json.Unmarshal(responseBody, &response)

	clientLog, _ := c.Get(generalkey.ClientLog)
	clientLogFields := clientLog.([]logrus.Fields)

	log, _ := c.Get(generalkey.Logger)
	entry := log.(*logrus.Entry)

	entry.WithFields(logrus.Fields{
		"requestAgent":       c.GetHeader("User-Agent"),
		"requestBody":        request,
		"requestBodyString":  string(bodyBytes),
		"requestContentType": c.GetHeader("Content-Type"),
		"requestHeader":      c.Request.Header,
		"requestHostName":    c.Request.Host,
		"requestId":          c.GetString(generalkey.RequestID),
		"requestIp":          c.ClientIP(),
		"requestMethod":      c.Request.Method,
		"requestProtocol":    c.Request.Proto,
		"requestTimestamp":   requestTime.Format(time.RFC3339Nano),
		"requestUrl":         c.Request.RequestURI,
		"responseBody":       response,
		"responseBodyString": string(responseBody),
		"responseHeader":     c.Writer.Header(),
		"responseLatency":    latency.String(),
		"responseStatus":     c.Writer.Status(),
		"responseTimestamp":  requestTime.Add(latency).Format(time.RFC3339Nano),
		"responseUser":       currentUser.Username,
		"target":             clientLogFields,
	}).Info()
}

// LogGinClient logs the details of an HTTP request and response using Gin and Logrus.
//
// This function captures information about an HTTP request and its corresponding response,
// including headers, bodies, status, and timing, and stores this information in the Gin
// context under a specific key for further use (e.g., logging or debugging purposes).
//
// Parameters:
//   - c *gin.Context: The Gin context that holds the request and response details.
//   - requestURL string: The URL of the request being logged.
//   - requestMethod string: The HTTP method used in the request (e.g., GET, POST).
//   - requestContentType string: The content type of the request (e.g., application/json).
//   - requestHeader map[string]interface{}: A map containing the headers of the request.
//   - requestBody []byte: The body of the request as a byte slice.
//   - responseHeader map[string]interface{}: A map containing the headers of the response.
//   - responseBody []byte: The body of the response as a byte slice.
//   - responseStatus int: The HTTP status code of the response.
//   - requestTime time.Time: The timestamp of when the request was initiated.
//   - responseLatency time.Duration: The duration it took to receive the response.
//
// Behavior:
//   - The function unmarshal the request and response bodies into Logrus fields, if possible.
//   - It compiles the request and response details into a log data structure with various fields
//     like request body, headers, status, and timing details.
//   - The log data is then stored in the Gin context using the key defined in `generalkey.ClientLog`.
//   - If the `generalkey.ClientLog` key does not already exist in the context, it initializes a
//     new slice of Logrus fields before appending the current log data.
//
// Example:
//
//	LogGinClient(c, "https://example.com/api", "POST", "application/json",
//	    map[string]interface{}{"Authorization": "Bearer token"}, []byte(`{"name":"John"}`),
//	    map[string]interface{}{"Content-Type": "application/json"}, []byte(`{"status":"success"}`),
//	    200, time.Now(), time.Millisecond*250)
func LogGinClient(
	c *gin.Context,
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

	clientLog, exists := c.Get(generalkey.ClientLog)

	if !exists {
		clientLog = []logrus.Fields{}
	}

	clientLog = append(clientLog.([]logrus.Fields), logData)

	c.Set(generalkey.ClientLog, clientLog)
}
