package util

import (
	"time"

	"github.com/christiandoxa/welog/pkg/infrastructure/logger"
	"github.com/christiandoxa/welog/pkg/model"
	"github.com/goccy/go-json"
	"github.com/sirupsen/logrus"
)

func BuildTargetLogFields(req model.TargetRequest, res model.TargetResponse) logrus.Fields {
	var requestField, responseField logrus.Fields

	if err := json.Unmarshal(req.Body, &requestField); err != nil {
		logger.Logger().Error(err)
	}
	if err := json.Unmarshal(res.Body, &responseField); err != nil {
		logger.Logger().Error(err)
	}

	return logrus.Fields{
		"targetRequestBody":        requestField,
		"targetRequestBodyString":  string(req.Body),
		"targetRequestContentType": req.ContentType,
		"targetRequestHeader":      req.Header,
		"targetRequestMethod":      req.Method,
		"targetRequestTimestamp":   req.Timestamp.Format(time.RFC3339Nano),
		"targetRequestURL":         req.URL,
		"targetResponseBody":       responseField,
		"targetResponseBodyString": string(res.Body),
		"targetResponseHeader":     res.Header,
		"targetResponseLatency":    res.Latency.String(),
		"targetResponseStatus":     res.Status,
		"targetResponseTimestamp":  req.Timestamp.Add(res.Latency).Format(time.RFC3339Nano),
	}
}
