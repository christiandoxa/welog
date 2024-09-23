package welog

import (
	"bytes"
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
	app.Use(NewFiber())

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
	body := []byte(`{"test": "data"}`)
	response := []byte(`{"response": "ok"}`)
	status := http.StatusOK
	start := time.Now()
	elapsed := 100 * time.Millisecond

	// Call LogFiberClient function
	LogFiberClient(fiberCtx, url, method, contentType, header, body, response, status, start, elapsed)

	// Verify that the log data was appended to the client log
	clientLog := fiberCtx.Locals(generalkey.ClientLog).([]logrus.Fields)
	assert.Len(t, clientLog, 1)
	assert.Equal(t, status, clientLog[0]["targetResponseStatus"])
}
