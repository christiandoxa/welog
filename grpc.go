package welog

import (
	"context"
	"fmt"
	"os/user"
	"strings"
	"time"

	"github.com/christiandoxa/welog/pkg/constant/generalkey"
	"github.com/christiandoxa/welog/pkg/infrastructure/logger"
	"github.com/christiandoxa/welog/pkg/model"
	"github.com/christiandoxa/welog/pkg/util"
	"github.com/goccy/go-json"
	"github.com/google/uuid"
	grpcmiddleware "github.com/grpc-ecosystem/go-grpc-middleware/v2"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const requestIDMetadataKey = "x-request-id"

// NewGRPCUnary returns a grpc.UnaryServerInterceptor that injects Welog context
// and logs request/response data.
func NewGRPCUnary() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		ctx, entry, requestID, clientLog := prepareGRPCContext(ctx)

		start := time.Now()
		res, err := handler(ctx, req)
		logGRPCUnary(grpcUnaryLogContext{
			ctx:       ctx,
			entry:     entry,
			info:      info,
			request:   req,
			response:  res,
			start:     start,
			err:       err,
			requestID: requestID,
			clientLog: clientLog,
		})

		return res, err
	}
}

// NewGRPCStream returns a grpc.StreamServerInterceptor that injects Welog context
// and logs stream lifecycle data.
func NewGRPCStream() grpc.StreamServerInterceptor {
	return func(srv interface{}, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx, entry, requestID, clientLog := prepareGRPCContext(stream.Context())
		wrapped := grpcmiddleware.WrapServerStream(stream)
		wrapped.WrappedContext = ctx

		start := time.Now()
		err := handler(srv, wrapped)
		logGRPCStream(ctx, entry, info, start, err, requestID, clientLog)

		return err
	}
}

// LogGRPCClient appends outbound call logs to the request-scoped slice.
func LogGRPCClient(ctx context.Context, req model.TargetRequest, res model.TargetResponse) {
	logData := util.BuildTargetLogFields(req, res)

	switch stored := ctx.Value(generalkey.ClientLog).(type) {
	case *[]logrus.Fields:
		*stored = append(*stored, logData)
	}

	_ = grpc.SetHeader(ctx, metadata.Pairs(requestIDMetadataKey, fetchRequestID(ctx)))
}

func prepareGRPCContext(ctx context.Context) (context.Context, *logrus.Entry, string, *[]logrus.Fields) {
	requestID := fetchRequestID(ctx)
	entry := fetchLogger(ctx, requestID)
	clientLog := fetchClientLog(ctx)

	ctx = context.WithValue(ctx, generalkey.RequestID, requestID)
	ctx = context.WithValue(ctx, generalkey.Logger, entry)
	ctx = context.WithValue(ctx, generalkey.ClientLog, clientLog)

	_ = grpc.SetHeader(ctx, metadata.Pairs(requestIDMetadataKey, requestID))

	return ctx, entry, requestID, clientLog
}

func fetchRequestID(ctx context.Context) string {
	if ctx != nil {
		if val := ctx.Value(generalkey.RequestID); val != nil {
			if id, ok := val.(string); ok && id != "" {
				return id
			}
		}
	}

	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if vals := md[strings.ToLower(generalkey.RequestIDHeader)]; len(vals) > 0 && vals[0] != "" {
			return vals[0]
		}
		if vals := md[requestIDMetadataKey]; len(vals) > 0 && vals[0] != "" {
			return vals[0]
		}
	}

	return uuid.NewString()
}

func fetchLogger(ctx context.Context, requestID string) *logrus.Entry {
	if existing := ctx.Value(generalkey.Logger); existing != nil {
		if entry, ok := existing.(*logrus.Entry); ok {
			return entry.WithField(generalkey.RequestID, requestID)
		}
	}

	return logger.Logger().WithField(generalkey.RequestID, requestID)
}

func fetchClientLog(ctx context.Context) *[]logrus.Fields {
	if existing := ctx.Value(generalkey.ClientLog); existing != nil {
		switch v := existing.(type) {
		case *[]logrus.Fields:
			return v
		case []logrus.Fields:
			copyStored := append([]logrus.Fields{}, v...)
			return &copyStored
		}
	}

	var entries []logrus.Fields
	return &entries
}

type grpcUnaryLogContext struct {
	ctx       context.Context
	entry     *logrus.Entry
	info      *grpc.UnaryServerInfo
	request   interface{}
	response  interface{}
	start     time.Time
	err       error
	requestID string
	clientLog *[]logrus.Fields
}

func logGRPCUnary(p grpcUnaryLogContext) {
	latency := time.Since(p.start)

	requestBody, requestBodyString := marshalPayload(p.request)
	responseBody, responseBodyString := marshalPayload(p.response)

	md := metadataToMap(p.ctx)
	peerAddr := peerAddress(p.ctx)
	currentUser := username()
	code := status.Code(p.err)

	errorMessage := ""
	if p.err != nil {
		errorMessage = p.err.Error()
	}

	p.entry.WithFields(logrus.Fields{
		"grpcMethod":         p.info.FullMethod,
		"grpcRequest":        requestBody,
		"grpcRequestString":  requestBodyString,
		"grpcRequestMeta":    md,
		"grpcPeer":           peerAddr,
		"grpcStatusCode":     code.String(),
		"grpcError":          errorMessage,
		"grpcResponse":       responseBody,
		"grpcResponseString": responseBodyString,
		"requestId":          p.requestID,
		"requestTimestamp":   p.start.Format(time.RFC3339Nano),
		"responseTimestamp":  p.start.Add(latency).Format(time.RFC3339Nano),
		"responseLatency":    latency.String(),
		"responseUser":       currentUser,
		"target":             readClientLog(p.clientLog),
	}).Info()
}

func logGRPCStream(
	ctx context.Context,
	entry *logrus.Entry,
	info *grpc.StreamServerInfo,
	start time.Time,
	err error,
	requestID string,
	clientLog *[]logrus.Fields,
) {
	latency := time.Since(start)

	md := metadataToMap(ctx)
	peerAddr := peerAddress(ctx)
	currentUser := username()
	code := status.Code(err)

	errorMessage := ""
	if err != nil {
		errorMessage = err.Error()
	}

	entry.WithFields(logrus.Fields{
		"grpcMethod":         info.FullMethod,
		"grpcRequestMeta":    md,
		"grpcPeer":           peerAddr,
		"grpcStatusCode":     code.String(),
		"grpcError":          errorMessage,
		"grpcIsClientStream": info.IsClientStream,
		"grpcIsServerStream": info.IsServerStream,
		"requestId":          requestID,
		"requestTimestamp":   start.Format(time.RFC3339Nano),
		"responseTimestamp":  start.Add(latency).Format(time.RFC3339Nano),
		"responseLatency":    latency.String(),
		"responseUser":       currentUser,
		"target":             readClientLog(clientLog),
	}).Info()
}

func marshalPayload(value interface{}) (logrus.Fields, string) {
	if value == nil {
		return logrus.Fields{}, ""
	}

	var (
		raw []byte
		err error
	)

	switch v := value.(type) {
	case proto.Message:
		raw, err = protojson.MarshalOptions{
			EmitUnpopulated: true,
			UseProtoNames:   true,
		}.Marshal(v)
	default:
		raw, err = json.Marshal(v)
	}

	if err != nil {
		logger.Logger().Error(err)
		return logrus.Fields{}, fmt.Sprint(value)
	}

	var fields logrus.Fields
	if err = json.Unmarshal(raw, &fields); err != nil {
		logger.Logger().Error(err)
	}

	return fields, string(raw)
}

func metadataToMap(ctx context.Context) map[string]interface{} {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return map[string]interface{}{}
	}

	result := make(map[string]interface{}, len(md))
	for key, vals := range md {
		if len(vals) == 1 {
			result[key] = vals[0]
			continue
		}
		result[key] = vals
	}

	return result
}

func peerAddress(ctx context.Context) string {
	if p, ok := peer.FromContext(ctx); ok && p.Addr != nil {
		return p.Addr.String()
	}
	return ""
}

func username() string {
	currentUser, err := user.Current()
	if err != nil || currentUser == nil {
		logger.Logger().Error(err)
		return "unknown"
	}
	return currentUser.Username
}

func readClientLog(clientLog *[]logrus.Fields) []logrus.Fields {
	if clientLog == nil {
		return nil
	}
	return *clientLog
}
