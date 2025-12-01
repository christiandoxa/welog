package welog

import (
	"bytes"
	"io"
	"os"
	"os/user"
	"time"

	"github.com/christiandoxa/welog/pkg/constant/envkey"
	"github.com/christiandoxa/welog/pkg/constant/generalkey"
	"github.com/christiandoxa/welog/pkg/infrastructure/logger"
	"github.com/christiandoxa/welog/pkg/model"
	"github.com/christiandoxa/welog/pkg/util"
	"github.com/gin-gonic/gin"
	"github.com/goccy/go-json"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

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
		requestID := c.Get(generalkey.RequestIDHeader)
		if requestID == "" {
			requestID = uuid.NewString()
		}

		// Set the request ID to the context.
		c.Set(generalkey.RequestIDHeader, requestID)

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
	req model.TargetRequest,
	res model.TargetResponse,
) {
	logData := util.BuildTargetLogFields(req, res)

	clientLog := c.Locals(generalkey.ClientLog).([]logrus.Fields)
	c.Locals(generalkey.ClientLog, append(clientLog, logData))
}

// NewGin creates a new Gin middleware that logs requests and responses.
func NewGin() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Generate or retrieve the request ID.
		requestID := c.GetHeader(generalkey.RequestIDHeader)
		if requestID == "" {
			requestID = uuid.NewString()
		}

		// Set the request ID in the context.
		c.Header(generalkey.RequestIDHeader, requestID)

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

	currentUser, err := user.Current()
	if err != nil {
		logger.Logger().Error(err)
	}

	var request, response logrus.Fields
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		logger.Logger().Error(err)
	}
	c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
	if err = json.Unmarshal(bodyBytes, &request); err != nil {
		logger.Logger().Error(err)
	}

	responseBody := buf.Bytes()
	if err = json.Unmarshal(responseBody, &response); err != nil {
		logger.Logger().Error(err)
	}

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
	req model.TargetRequest,
	res model.TargetResponse,
) {
	logData := util.BuildTargetLogFields(req, res)

	clientLog, exists := c.Get(generalkey.ClientLog)
	if !exists {
		clientLog = []logrus.Fields{}
	}

	clientLog = append(clientLog.([]logrus.Fields), logData)
	c.Set(generalkey.ClientLog, clientLog)
}
