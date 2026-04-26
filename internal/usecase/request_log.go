package usecase

import (
	"time"

	"github.com/christiandoxa/welog/internal/entity"
	"github.com/christiandoxa/welog/pkg/model"
	"github.com/christiandoxa/welog/pkg/util"
	"github.com/sirupsen/logrus"
)

// RequestLogUseCase builds logrus fields from framework-neutral log events.
type RequestLogUseCase struct{}

// NewRequestLogUseCase creates the request logging use case.
func NewRequestLogUseCase() RequestLogUseCase {
	return RequestLogUseCase{}
}

// Payload converts raw bytes into a sanitized payload value object.
func (RequestLogUseCase) Payload(body []byte) entity.Payload {
	fields, bodyString := util.BuildBodyLogFields(body)
	return entity.Payload{
		Fields: fields,
		String: bodyString,
	}
}

// TargetFields builds outbound target request log fields.
func (RequestLogUseCase) TargetFields(req model.TargetRequest, res model.TargetResponse) logrus.Fields {
	return util.BuildTargetLogFields(req, res)
}

// HTTPFields builds HTTP request/response log fields.
func (RequestLogUseCase) HTTPFields(event entity.HTTPLogEvent) logrus.Fields {
	return logrus.Fields{
		"requestAgent":       event.RequestAgent,
		"requestBody":        event.RequestBody.Fields,
		"requestBodyString":  event.RequestBody.String,
		"requestContentType": event.RequestContentType,
		"requestHeader":      event.RequestHeader,
		"requestHostName":    event.RequestHostName,
		"requestId":          event.RequestID,
		"requestIp":          event.RequestIP,
		"requestMethod":      event.RequestMethod,
		"requestProtocol":    event.RequestProtocol,
		"requestTimestamp":   event.RequestTimestamp.Format(time.RFC3339Nano),
		"requestUrl":         event.RequestURL,
		"responseBody":       event.ResponseBody.Fields,
		"responseBodyString": event.ResponseBody.String,
		"responseHeader":     event.ResponseHeader,
		"responseLatency":    event.ResponseLatency.String(),
		"responseStatus":     event.ResponseStatus,
		"responseTimestamp":  event.RequestTimestamp.Add(event.ResponseLatency).Format(time.RFC3339Nano),
		"responseUser":       event.ResponseUser,
		"target":             event.Target,
	}
}

// GRPCUnaryFields builds unary gRPC request/response log fields.
func (RequestLogUseCase) GRPCUnaryFields(event entity.GRPCUnaryLogEvent) logrus.Fields {
	return logrus.Fields{
		"grpcMethod":         event.Method,
		"grpcRequest":        event.Request.Fields,
		"grpcRequestString":  event.Request.String,
		"grpcRequestMeta":    event.RequestMeta,
		"grpcPeer":           event.Peer,
		"grpcStatusCode":     event.StatusCode,
		"grpcError":          event.Error,
		"grpcResponse":       event.Response.Fields,
		"grpcResponseString": event.Response.String,
		"requestId":          event.RequestID,
		"requestTimestamp":   event.RequestTimestamp.Format(time.RFC3339Nano),
		"responseTimestamp":  event.RequestTimestamp.Add(event.ResponseLatency).Format(time.RFC3339Nano),
		"responseLatency":    event.ResponseLatency.String(),
		"responseUser":       event.ResponseUser,
		"target":             event.Target,
	}
}

// GRPCStreamFields builds streaming gRPC lifecycle log fields.
func (RequestLogUseCase) GRPCStreamFields(event entity.GRPCStreamLogEvent) logrus.Fields {
	return logrus.Fields{
		"grpcMethod":         event.Method,
		"grpcRequestMeta":    event.RequestMeta,
		"grpcPeer":           event.Peer,
		"grpcStatusCode":     event.StatusCode,
		"grpcError":          event.Error,
		"grpcIsClientStream": event.IsClientStream,
		"grpcIsServerStream": event.IsServerStream,
		"requestId":          event.RequestID,
		"requestTimestamp":   event.RequestTimestamp.Format(time.RFC3339Nano),
		"responseTimestamp":  event.RequestTimestamp.Add(event.ResponseLatency).Format(time.RFC3339Nano),
		"responseLatency":    event.ResponseLatency.String(),
		"responseUser":       event.ResponseUser,
		"target":             event.Target,
	}
}
