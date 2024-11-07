package welog

import (
	"bytes"
	"fmt"
	"github.com/christiandoxa/welog/pkg/constant/envkey"
	"github.com/christiandoxa/welog/pkg/constant/generalkey"
	"github.com/christiandoxa/welog/pkg/infrastructure/logger"
	"github.com/christiandoxa/welog/pkg/util"
	"github.com/gin-gonic/gin"
	"github.com/goccy/go-json"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"io"
	"os"
	"os/user"
	"time"
)

type Config struct {
	ElasticIndex    string
	ElasticURL      string
	ElasticUsername string
	ElasticPassword string
}

type Target struct {
	TargetUrl      string        `json:"target_url"`
	ElapsedTime    time.Duration `json:"target_elapsed_time"`
	Method         string        `json:"target_method"`
	RequestHeader  any           `json:"target_request_header"`
	RequestBody    any           `json:"target_request_body"`
	ResponseHeader any           `json:"target_response_header"`
	ResponseBody   []byte        `json:"target_response_body"`
	StatusCode     int           `json:"target_status_code"`
}

// responseBodyWriter is a custom response writer that captures the response body.
type responseBodyWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

type stackTracer interface {
	StackTrace() errors.StackTrace
}

// Write writes the response body to both the underlying ResponseWriter and the buffer.
func (w responseBodyWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

func SetConfig(config Config) {
	if err := os.Setenv(envkey.ElasticIndex, config.ElasticIndex); err != nil {
		logger.Logger().Error(err)
	}
	if err := os.Setenv(envkey.ElasticURL, config.ElasticURL); err != nil {
		logger.Logger().Error(err)
	}
	if err := os.Setenv(envkey.ElasticUsername, config.ElasticUsername); err != nil {
		logger.Logger().Error(err)
	}
	if err := os.Setenv(envkey.ElasticPassword, config.ElasticPassword); err != nil {
		logger.Logger().Error(err)
	}
}

// NewFiber creates a new Fiber middleware that logs requests and responses.
func NewFiber(fiberConfig fiber.Config) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Generate or retrieve the request ID.
		requestID := c.Get("X-Request-ID")
		if requestID == "" {
			requestID = uuid.NewString()
		}

		// Set the request ID to the context.
		c.Set("X-Request-ID", requestID)

		// Set request-related values to the context.
		c.Locals(generalkey.RequestID, requestID)
		c.Locals(generalkey.Logger, logger.Logger().WithField(generalkey.RequestID, requestID))
		c.Locals(generalkey.ClientLog, []logrus.Fields{})

		reqTime := time.Now()

		// Proceed to the next middleware and handle any errors.
		if err := c.Next(); err != nil {
			errorHandler := fiber.DefaultErrorHandler
			if fiberConfig.ErrorHandler != nil {
				errorHandler = fiberConfig.ErrorHandler
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
	if err = json.Unmarshal(c.Body(), &request); err != nil {
		logger.Logger().Error(err)
	}
	if err = json.Unmarshal(c.Response().Body(), &response); err != nil {
		logger.Logger().Error(err)
	}

	clientLog := c.Locals(generalkey.ClientLog).([]logrus.Fields)

	// Log various details of the request and response.
	c.Locals(generalkey.Logger).(*logrus.Entry).WithFields(logrus.Fields{
		"requestAgent":       c.Get("User-Agent"),
		"requestBody":        request,
		"requestBodyString":  string(c.Body()),
		"requestContentType": c.Get("Content-Type"),
		"requestHeader":      c.GetReqHeaders(),
		"requestHostName":    c.Hostname(),
		"requestId":          c.Locals(generalkey.RequestID),
		"requestIp":          c.IP(),
		"requestMethod":      c.Method(),
		"requestProtocol":    c.Protocol(),
		"requestTimestamp":   requestTime.Format(time.RFC3339Nano),
		"requestUrl":         c.BaseURL() + c.OriginalURL(),
		"responseBody":       response,
		"responseBodyString": string(c.Response().Body()),
		"responseHeader":     util.HeaderToMap(&c.Response().Header),
		"responseLatency":    latency.String(),
		"responseStatus":     c.Response().StatusCode(),
		"responseTimestamp":  requestTime.Add(latency).Format(time.RFC3339Nano),
		"responseUser":       currentUser.Username,
		"target":             clientLog,
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

	if err := json.Unmarshal(requestBody, &requestField); err != nil {
		logger.Logger().Error(err)
	}
	if err := json.Unmarshal(responseBody, &responseField); err != nil {
		logger.Logger().Error(err)
	}

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

		// Set the request ID in the context.
		c.Header("X-Request-ID", requestID)

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
	log, _ := c.Get(generalkey.Logger)
	entry := log.(*logrus.Entry)

	var request, response, errorLog logrus.Fields
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		entry.WithError(err).Error("logger_self_log")
		request = nil
	}
	c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
	if err = json.Unmarshal(bodyBytes, &request); err != nil {
		entry.WithError(err).Error("logger_self_log")
		request = nil
	}

	responseBody := buf.Bytes()
	if err = json.Unmarshal(responseBody, &response); err != nil {
		entry.WithError(err).Error("logger_self_log")
		response = nil
	}

	errContext, ok := c.Get(generalkey.ErrorLog)
	if ok {
		errStack := errors.WithStack(errContext.(error))
		st := errStack.(stackTracer).StackTrace()
		errorLog = logrus.Fields{
			"is_error":      true,
			"error_message": fmt.Sprintf("%+v", errStack.Error()),
			"error_cause":   fmt.Sprintf("%+v", st[5:6]),
		}
	}

	// Log for client request and response
	entry.WithFields(logrus.Fields{
		"client_ip":        c.ClientIP(),
		"real_time_system": time.Now().In(time.UTC).Format("2006-01-02T15:04:05.000"),
		"elapsed_time":     latency.Milliseconds(),
		"req_header":       c.Request.Header,
		"req_body":         request,
		"req_verb":         c.Request.Method,
		"req_url":          fmt.Sprintf("%s%s", c.Request.Host, c.Request.URL.String()),
		"res_header":       c.Writer.Header(),
		"res_body":         response,
		"status_code":      c.Writer.Status(),
		"error_log":        errorLog,
	}).Info("client_log")
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

	if err := json.Unmarshal(requestBody, &requestField); err != nil {
		logger.Logger().Error(err)
	}
	if err := json.Unmarshal(responseBody, &responseField); err != nil {
		logger.Logger().Error(err)
	}

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

func LogTarget(c *gin.Context, target Target) {
	log, _ := c.Get(generalkey.Logger)
	entry := log.(*logrus.Entry)

	var response, errorLog logrus.Fields

	if err := json.Unmarshal(target.ResponseBody, &response); err != nil {
		entry.WithError(err).Error("logger_self_log")
		response = map[string]interface{}{
			"error_html": string(target.ResponseBody),
		}
	}

	errContext, ok := c.Get(generalkey.ErrorLog)
	if ok {
		errStack := errors.WithStack(errContext.(error))
		st := errStack.(stackTracer).StackTrace()
		errorLog = logrus.Fields{
			"is_error":      true,
			"error_message": fmt.Sprintf("%+v", errStack.Error()),
			"error_cause":   fmt.Sprintf("%+v", st[5:6]),
		}
	}

	// Log for target request and response
	entry.WithFields(logrus.Fields{
		"elapsed_time": target.ElapsedTime.Milliseconds(),
		"req_header":   target.RequestHeader,
		"req_body":     target.RequestBody,
		"req_verb":     target.Method,
		"req_url":      target.TargetUrl,
		"res_header":   target.ResponseHeader,
		"res_body":     response,
		"status_code":  target.StatusCode,
		"error_log":    errorLog,
	}).Info("target_log")
}
