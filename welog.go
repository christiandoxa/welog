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
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

var setenv = os.Setenv

// responseBodyWriter is a custom response writer that captures the response body.
type responseBodyWriter struct {
	gin.ResponseWriter
	body *responseBodyCapture
}

// Write writes the response body to both the underlying ResponseWriter and the buffer.
func (w responseBodyWriter) Write(b []byte) (int, error) {
	_, _ = w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

func (w responseBodyWriter) WriteString(s string) (int, error) {
	_, _ = w.body.WriteString(s)
	return w.ResponseWriter.WriteString(s)
}

type responseBodyCapture struct {
	buf   bytes.Buffer
	limit int
}

func newResponseBodyCapture() *responseBodyCapture {
	return &responseBodyCapture{limit: util.DefaultMaxLoggedBodyBytes}
}

func (b *responseBodyCapture) Write(p []byte) (int, error) {
	originalLen := len(p)
	remaining := b.limit + 1 - b.buf.Len()
	if remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		_, _ = b.buf.Write(p)
	}
	return originalLen, nil
}

func (b *responseBodyCapture) WriteString(s string) (int, error) {
	originalLen := len(s)
	remaining := b.limit + 1 - b.buf.Len()
	if remaining > 0 {
		if len(s) > remaining {
			s = s[:remaining]
		}
		_, _ = b.buf.WriteString(s)
	}
	return originalLen, nil
}

func (b *responseBodyCapture) Bytes() []byte {
	return b.buf.Bytes()
}

func SetConfig(config Config) {
	if err := setenv(envkey.ElasticIndex, config.ElasticIndex); err != nil {
		logger.Logger().Error(err)
	}
	if err := setenv(envkey.ElasticURL, config.ElasticURL); err != nil {
		logger.Logger().Error(err)
	}
	if err := setenv(envkey.ElasticUsername, config.ElasticUsername); err != nil {
		logger.Logger().Error(err)
	}
	if err := setenv(envkey.ElasticPassword, config.ElasticPassword); err != nil {
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
		fiberLogger(c).Error(err)
		currentUser = &user.User{Username: "unknown"}
	}

	request, requestBodyString := util.BuildBodyLogFields(c.Body())
	response, responseBodyString := util.BuildBodyLogFields(c.Response().Body())

	clientLog := fiberClientLog(c)

	// Log various details of the request and response.
	fiberLogger(c).WithFields(logrus.Fields{
		"requestAgent":       c.Get("User-Agent"),
		"requestBody":        request,
		"requestBodyString":  requestBodyString,
		"requestContentType": c.Get("Content-Type"),
		"requestHeader":      util.SanitizeStringSliceMap(c.GetReqHeaders()),
		"requestHostName":    c.Hostname(),
		"requestId":          c.Locals(generalkey.RequestID),
		"requestIp":          c.IP(),
		"requestMethod":      c.Method(),
		"requestProtocol":    c.Protocol(),
		"requestTimestamp":   requestTime.Format(time.RFC3339Nano),
		"requestUrl":         util.SanitizeURL(c.BaseURL() + c.OriginalURL()),
		"responseBody":       response,
		"responseBodyString": responseBodyString,
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

	clientLog := fiberClientLog(c)
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
		bodyBuf := newResponseBodyCapture()
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
func logGin(c *gin.Context, buf *responseBodyCapture, requestTime time.Time) {
	latency := time.Since(requestTime)

	currentUser, err := user.Current()
	if err != nil {
		logger.Logger().Error(err)
	}
	responseUser := "unknown"
	if currentUser != nil {
		responseUser = currentUser.Username
	}

	bodyBytes, err := readBodyForLog(c.Request.Body)
	if err != nil {
		logger.Logger().Error(err)
	}
	c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
	request, requestBodyString := util.BuildBodyLogFields(bodyBytes)

	responseBody := buf.Bytes()
	response, responseBodyString := util.BuildBodyLogFields(responseBody)

	clientLog, _ := c.Get(generalkey.ClientLog)
	clientLogFields, _ := clientLog.([]logrus.Fields)

	log, _ := c.Get(generalkey.Logger)
	entry, _ := log.(*logrus.Entry)
	if entry == nil {
		entry = logger.Logger().WithField(generalkey.RequestID, c.GetString(generalkey.RequestID))
	}

	// Log various details of the request and response.
	entry.WithFields(logrus.Fields{
		"requestAgent":       c.GetHeader("User-Agent"),
		"requestBody":        request,
		"requestBodyString":  requestBodyString,
		"requestContentType": c.GetHeader("Content-Type"),
		"requestHeader":      util.SanitizeHTTPHeader(c.Request.Header),
		"requestHostName":    c.Request.Host,
		"requestId":          c.GetString(generalkey.RequestID),
		"requestIp":          c.ClientIP(),
		"requestMethod":      c.Request.Method,
		"requestProtocol":    c.Request.Proto,
		"requestTimestamp":   requestTime.Format(time.RFC3339Nano),
		"requestUrl":         util.SanitizeURL(c.Request.RequestURI),
		"responseBody":       response,
		"responseBodyString": responseBodyString,
		"responseHeader":     util.SanitizeHTTPHeader(c.Writer.Header()),
		"responseLatency":    latency.String(),
		"responseStatus":     c.Writer.Status(),
		"responseTimestamp":  requestTime.Add(latency).Format(time.RFC3339Nano),
		"responseUser":       responseUser,
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

	clientLogFields, _ := clientLog.([]logrus.Fields)
	c.Set(generalkey.ClientLog, append(clientLogFields, logData))
}

func fiberLogger(c *fiber.Ctx) *logrus.Entry {
	if entry, ok := c.Locals(generalkey.Logger).(*logrus.Entry); ok {
		return entry
	}
	return logger.Logger().WithField(generalkey.RequestID, c.Locals(generalkey.RequestID))
}

func fiberClientLog(c *fiber.Ctx) []logrus.Fields {
	if clientLog, ok := c.Locals(generalkey.ClientLog).([]logrus.Fields); ok {
		return clientLog
	}
	return []logrus.Fields{}
}

func readBodyForLog(r io.Reader) ([]byte, error) {
	if r == nil {
		return nil, nil
	}
	return io.ReadAll(io.LimitReader(r, util.DefaultMaxLoggedBodyBytes+1))
}
