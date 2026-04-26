package util

import (
	"time"

	"github.com/christiandoxa/welog/pkg/model"
	"github.com/sirupsen/logrus"
)

func BuildTargetLogFields(req model.TargetRequest, res model.TargetResponse) logrus.Fields {
	requestField, requestBodyString := BuildBodyLogFields(req.Body)
	responseField, responseBodyString := BuildBodyLogFields(res.Body)

	return logrus.Fields{
		"targetRequestBody":        requestField,
		"targetRequestBodyString":  requestBodyString,
		"targetRequestContentType": req.ContentType,
		"targetRequestHeader":      SanitizeHeaderMap(req.Header),
		"targetRequestMethod":      req.Method,
		"targetRequestTimestamp":   req.Timestamp.Format(time.RFC3339Nano),
		"targetRequestURL":         SanitizeURL(req.URL),
		"targetResponseBody":       responseField,
		"targetResponseBodyString": responseBodyString,
		"targetResponseHeader":     SanitizeHeaderMap(res.Header),
		"targetResponseLatency":    res.Latency.String(),
		"targetResponseStatus":     res.Status,
		"targetResponseTimestamp":  req.Timestamp.Add(res.Latency).Format(time.RFC3339Nano),
	}
}
