package welog

import (
	"bytes"
	"github.com/gin-gonic/gin"
	"github.com/valyala/fasthttp"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/christiandoxa/welog/pkg/constant/generalkey"
	"github.com/christiandoxa/welog/pkg/infrastructure/logger"
	"github.com/gofiber/fiber/v2"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

// TestNewFiber tests the NewFiber middleware by setting up a Fiber application,
// making a mock request, and verifying that the middleware behaves as expected.
func TestNewFiber(t *testing.T) {
	// Initialize Fiber app
	app := fiber.New()
	app.Use(NewFiber(fiber.Config{}))

	// Mock a request
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("User-Agent", "Test-Agent")

	// Record response
	resp, err := app.Test(req) //nolint:bodyclose

	// Check for no errors
	assert.NoError(t, err)
	assert.Equal(t, fiber.StatusNotFound, resp.StatusCode)
}

// TestLogFiber tests the logFiber function by creating a mock Fiber context
// and verifying that logging occurs correctly when handling a request.
func TestLogFiber(t *testing.T) {
	// Create a mock Fiber context
	app := fiber.New()
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBuffer([]byte(`{"key": "value"}`)))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	// Set up a middleware function to log
	app.Use(func(c *fiber.Ctx) error {
		c.Locals(generalkey.Logger, logger.Logger().WithField(generalkey.RequestID, c.Locals("requestid")))
		c.Locals(generalkey.ClientLog, []logrus.Fields{})
		logFiber(c, time.Now(), "requestid")
		return c.SendStatus(fiber.StatusOK)
	})

	// Call the middleware with the request
	_, err := app.Test(req, -1) //nolint:bodyclose
	assert.NoError(t, err)

	// Check the response status
	assert.Equal(t, fiber.StatusOK, resp.Code)
}

// TestLogFiberClient tests the LogFiberClient function by setting up a Fiber context,
// calling the function with mock data, and checking that the log data is correctly appended.
func TestLogFiberClient(t *testing.T) {
	// Set up a Fiber app
	app := fiber.New()

	// Create a fasthttp.RequestCtx to acquire the Fiber context
	fastCtx := &fasthttp.RequestCtx{}
	fiberCtx := app.AcquireCtx(fastCtx)
	defer app.ReleaseCtx(fiberCtx) // Ensure the context is released after the test

	// Initialize required fields in the context
	fiberCtx.Locals(generalkey.ClientLog, []logrus.Fields{})

	// Mock input values
	url := "https://example.com"
	method := "GET"
	contentType := "application/json"
	header := map[string]interface{}{"Content-Type": "application/json"}
	responseHeader := map[string]interface{}{"Content-Type": "application/json"}
	body := []byte(`{"test": "data"}`)
	response := []byte(`{"response": "ok"}`)
	status := http.StatusOK
	start := time.Now()
	elapsed := 100 * time.Millisecond

	// Call LogFiberClient function
	LogFiberClient(fiberCtx, url, method, contentType, header, body, responseHeader, response, status, start, elapsed)

	// Verify that the log data was appended to the client log
	clientLog := fiberCtx.Locals(generalkey.ClientLog).([]logrus.Fields)
	assert.Len(t, clientLog, 1)
	assert.Equal(t, status, clientLog[0]["targetResponseStatus"])
}

// TestNewGin tests the NewGin middleware by setting up a Gin application,
// making a mock request, and verifying that the middleware behaves as expected.
func TestNewGin(t *testing.T) {
	// Initialize Gin engine
	r := gin.New()
	r.Use(NewGin())

	// Define a simple endpoint to test the middleware
	r.GET("/", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	// Create a mock request
	req, _ := http.NewRequest(http.MethodGet, "/", bytes.NewBuffer([]byte(`{"key": "value"}`)))
	req.Header.Set("X-Request-ID", "test-request-id")
	w := httptest.NewRecorder()

	// Perform the request
	r.ServeHTTP(w, req)

	// Verify the response
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "ok", w.Body.String())
}

// TestLogGin tests the logGin function by creating a mock Gin context
// and verifying that logging occurs correctly when handling a request.
func TestLogGin(t *testing.T) {
	// Create a logger with an output buffer
	buf := &bytes.Buffer{}
	log := logrus.New()
	log.Out = buf

	// Create a mock request and response recorder
	req, _ := http.NewRequest(http.MethodPost, "/", bytes.NewBuffer([]byte(`{"key": "value"}`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	// Create a mock Gin context
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	// Set logger and client log into the context
	c.Set(generalkey.Logger, log.WithField(generalkey.RequestID, "test-request-id"))
	c.Set(generalkey.ClientLog, []logrus.Fields{})

	// Create a response buffer to capture response data
	bodyBuf := &bytes.Buffer{}
	c.Writer = &responseBodyWriter{body: bodyBuf, ResponseWriter: c.Writer}

	// Call logGin function
	requestTime := time.Now()
	logGin(c, bodyBuf, requestTime)

	logOutput := buf.String()

	// Verify that logging occurred correctly
	assert.Contains(t, logOutput, `requestMethod=POST`)
	assert.Contains(t, logOutput, `responseStatus=200`)
}

// TestLogGinClient tests the LogGinClient function by setting up a Gin context,
// calling the function with mock data, and checking that the log data is correctly appended.
func TestLogGinClient(t *testing.T) {
	// Create a mock request and response recorder
	req, _ := http.NewRequest(http.MethodPost, "/", bytes.NewBuffer([]byte(`{"key": "value"}`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	// Create a mock Gin context
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	// Initialize required fields in the context
	c.Set(generalkey.ClientLog, []logrus.Fields{})

	// Mock input values
	url := "https://example.com"
	method := "POST"
	contentType := "application/json"
	header := map[string]interface{}{"Content-Type": "application/json"}
	responseHeader := map[string]interface{}{"Content-Type": "application/json"}
	body := []byte(`{"test": "data"}`)
	response := []byte(`{"response": "ok"}`)
	status := http.StatusOK
	start := time.Now()
	elapsed := 100 * time.Millisecond

	// Call LogGinClient function
	LogGinClient(c, url, method, contentType, header, body, responseHeader, response, status, start, elapsed)

	// Verify that the log data was appended to the client log
	clientLog, exists := c.Get(generalkey.ClientLog)
	assert.True(t, exists)
	logFields := clientLog.([]logrus.Fields)
	assert.Len(t, logFields, 1)
	assert.Equal(t, status, logFields[0]["targetResponseStatus"])
	assert.Equal(t, "POST", logFields[0]["targetRequestMethod"])
}
