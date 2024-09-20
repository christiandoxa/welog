package welog

import (
	"github.com/christiandoxa/welog/pkg/constant/generalkey"
	"github.com/christiandoxa/welog/pkg/infrastructure/logger"
	"github.com/goccy/go-json"
	"github.com/gofiber/fiber/v2"
	"github.com/sirupsen/logrus"
	"os/user"
	"time"
)

func NewFiber(requestIDContextName ...string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		contextName := "requestid"

		if len(requestIDContextName) > 0 && requestIDContextName[0] != "" {
			contextName = requestIDContextName[0]
		}

		c.Locals(generalkey.Logger, logger.Logger().WithField(generalkey.RequestID, c.Locals(contextName)))
		c.Locals(generalkey.ClientLog, []logrus.Fields{})

		reqTime := time.Now()

		if err := c.Next(); err != nil {
			logFiber(c, reqTime, contextName)
			return err
		}

		logFiber(c, reqTime, contextName)

		return nil
	}
}

func logFiber(c *fiber.Ctx, reqTime time.Time, contextName string) {
	latency := time.Since(reqTime)

	currentUser, err := user.Current()

	if err != nil {
		c.Locals(generalkey.Logger).(*logrus.Entry).Error(err)
		currentUser = &user.User{Username: "unknown"}
	}

	var request, response logrus.Fields

	_ = json.Unmarshal(c.Body(), &request)
	_ = json.Unmarshal(c.Response().Body(), &response)

	clientLog := c.Locals(generalkey.ClientLog).([]logrus.Fields)

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

func LogFiberClient(c *fiber.Ctx, url string, method string, contentType string, header map[string]interface{}, body []byte, response []byte, status int, start time.Time, elapsed time.Duration) {
	var requestField, responseField logrus.Fields

	_ = json.Unmarshal(body, &requestField)
	_ = json.Unmarshal(response, &responseField)

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

	clientLog := c.Locals(generalkey.ClientLog).([]logrus.Fields)
	c.Locals(generalkey.ClientLog, append(clientLog, logData))
}
