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

// Initialize the package by loading environment variables from a .env file.
func init() {
	if err := godotenv.Load(); err != nil {
		logrus.Fatal("Error loading .env file")
	}
}

// responseBodyWriter is a custom response writer that captures the response body.
type responseBodyWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

// Write writes the response body to both the underlying ResponseWriter and the buffer.
func (w responseBodyWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

// NewFiber creates a new Fiber middleware that logs requests and responses.
func NewFiber(config fiber.Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Generate or retrieve the request ID.
		requestID := c.Get("X-Request-ID")
		if requestID == "" {
			requestID = uuid.NewString()
		}

		// Set request-related values to the context.
		c.Locals(generalkey.RequestID, requestID)
		c.Locals(generalkey.Logger, logger.Logger().WithField(generalkey.RequestID, requestID))
		c.Locals(generalkey.ClientLog, []logrus.Fields{})

		reqTime := time.Now()

		// Proceed to the next middleware and handle any errors.
		if err := c.Next(); err != nil {
			errorHandler := fiber.DefaultErrorHandler
			if config.ErrorHandler != nil {
				errorHandler = config.ErrorHandler
			}
			if err = errorHandler(c, err); err != nil {
				logFiber(c, reqTime)
				return err
			}
		}

		// Log the request and response details.
		logFiber(c, reqTime)

		return nil
	}
}

// logFiber logs the details of the Fiber request and response.
func logFiber(c *fiber.Ctx, requestTime time.Time) {
	latency := time.Since(requestTime)

	// Get the current user; if not available, set as "unknown".
	currentUser, err := user.Current()
	if err != nil {
		c.Locals(generalkey.Logger).(*logrus.Entry).Error(err)
		currentUser = &user.User{Username: "unknown"}
	}

	var request, response logrus.Fields
	_ = json.Unmarshal(c.Body(), &request)
	_ = json.Unmarshal(c.Response().Body(), &response)

	clientLog := c.Locals(generalkey.ClientLog).([]logrus.Fields)

	// Log various details of the request and response.
	c.Locals(generalkey.Logger).(*logrus.Entry).WithFields(logrus.Fields{
		"requestAgent":         c.Get("User-Agent"),
		"requestBody":          request,
		"requestBodyString":    string(c.Body()),
		"requestContentType":   c.Get("Content-Type"),
		"requestHeader":        c.GetReqHeaders(),
		"requestHostName":      c.Hostname(),
		"requestId":            c.Locals(generalkey.RequestID),
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

// LogFiberClient logs a custom client request and response for Fiber.
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

	_ = json.Unmarshal(requestBody, &requestField)
	_ = json.Unmarshal(responseBody, &responseField)

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

	clientLog := c.Locals(generalkey.ClientLog).([]logrus.Fields)
	c.Locals(generalkey.ClientLog, append(clientLog, logData))
}

// NewGin creates a new Gin middleware that logs requests and responses.
func NewGin() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Generate or retrieve the request ID.
		requestID := c.GetHeader("X-Request-ID")
		if requestID == "" {
			requestID = uuid.NewString()
		}

		// Set request-related values to the context.
		c.Set(generalkey.RequestID, requestID)
		c.Set(generalkey.Logger, logger.Logger().WithField(generalkey.RequestID, requestID))
		c.Set(generalkey.ClientLog, []logrus.Fields{})

		// Create a response writer that captures the response body.
		bodyBuf := &bytes.Buffer{}
		writer := responseBodyWriter{body: bodyBuf, ResponseWriter: c.Writer}
		c.Writer = writer

		requestTime := time.Now()

		// Proceed to the next middleware.
		c.Next()

		// Log the request and response details.
		logGin(c, bodyBuf, requestTime)
	}
}

// logGin logs the details of the Gin request and response.
func logGin(c *gin.Context, buf *bytes.Buffer, requestTime time.Time) {
	latency := time.Since(requestTime)

	currentUser, _ := user.Current()

	var request, response logrus.Fields
	bodyBytes, _ := io.ReadAll(c.Request.Body)
	c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
	_ = json.Unmarshal(bodyBytes, &request)

	responseBody := buf.Bytes()
	_ = json.Unmarshal(responseBody, &response)

	clientLog, _ := c.Get(generalkey.ClientLog)
	clientLogFields := clientLog.([]logrus.Fields)

	log, _ := c.Get(generalkey.Logger)
	entry := log.(*logrus.Entry)

	// Log various details of the request and response.
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

// LogGinClient logs a custom client request and response for Gin.
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

	_ = json.Unmarshal(requestBody, &requestField)
	_ = json.Unmarshal(responseBody, &responseField)

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
