package welog

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/christiandoxa/welog/pkg/constant/generalkey"
	"github.com/christiandoxa/welog/pkg/model"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func TestNewGRPCUnary(t *testing.T) {
	buf := &bytes.Buffer{}
	log := logrus.New()
	log.SetFormatter(&logrus.JSONFormatter{DisableTimestamp: true})
	log.Out = buf

	ctx := context.WithValue(context.Background(), generalkey.Logger, logrus.NewEntry(log))
	ctx = metadata.NewIncomingContext(ctx, metadata.Pairs(strings.ToLower(generalkey.RequestIDHeader), "grpc-test-id"))

	interceptor := NewGRPCUnary()
	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Say"}

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		LogGRPCClient(ctx, model.TargetRequest{
			URL:         "https://example.com",
			Method:      "GET",
			ContentType: "application/json",
			Header:      map[string]interface{}{"Content-Type": "application/json"},
			Body:        []byte(`{"ping":"pong"}`),
			Timestamp:   time.Now(),
		}, model.TargetResponse{
			Header:  map[string]interface{}{"Content-Type": "application/json"},
			Body:    []byte(`{"ok":true}`),
			Status:  http.StatusOK,
			Latency: 10 * time.Millisecond,
		})
		return map[string]string{"hello": "world"}, nil
	}

	_, err := interceptor(ctx, map[string]string{"message": "hi"}, info, handler)
	assert.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.NotEmpty(t, lines)

	var entry map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(lines[len(lines)-1]), &entry))

	assert.Equal(t, "/test.Service/Say", entry["grpcMethod"])
	assert.Equal(t, "OK", entry["grpcStatusCode"])
	assert.Equal(t, "grpc-test-id", entry["requestId"])

	target, ok := entry["target"].([]interface{})
	require.True(t, ok)
	require.Len(t, target, 1)
	targetEntry, ok := target[0].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(http.StatusOK), targetEntry["targetResponseStatus"])
}

type testServerStream struct {
	grpc.ServerStream
	ctx        context.Context
	sentHeader metadata.MD
}

func (t *testServerStream) SetHeader(md metadata.MD) error {
	t.sentHeader = md
	return nil
}

func (t *testServerStream) SendHeader(md metadata.MD) error {
	t.sentHeader = md
	return nil
}

func (t *testServerStream) SetTrailer(metadata.MD) {}

func (t *testServerStream) Context() context.Context {
	return t.ctx
}

func (t *testServerStream) SendMsg(interface{}) error { return nil }

func (t *testServerStream) RecvMsg(interface{}) error { return nil }

func TestNewGRPCStream(t *testing.T) {
	buf := &bytes.Buffer{}
	log := logrus.New()
	log.SetFormatter(&logrus.JSONFormatter{DisableTimestamp: true})
	log.Out = buf

	ctx := context.WithValue(context.Background(), generalkey.Logger, logrus.NewEntry(log))
	ctx = metadata.NewIncomingContext(ctx, metadata.Pairs(strings.ToLower(generalkey.RequestIDHeader), "stream-id"))

	stream := &testServerStream{ctx: ctx}
	info := &grpc.StreamServerInfo{FullMethod: "/test.Service/Bidi", IsClientStream: true, IsServerStream: true}

	interceptor := NewGRPCStream()
	err := interceptor(nil, stream, info, func(_ interface{}, ss grpc.ServerStream) error {
		LogGRPCClient(ss.Context(), model.TargetRequest{
			URL:         "https://example.com",
			Method:      "POST",
			ContentType: "application/json",
			Header:      map[string]interface{}{"Content-Type": "application/json"},
			Body:        []byte(`{"foo":"bar"}`),
			Timestamp:   time.Now(),
		}, model.TargetResponse{
			Header:  map[string]interface{}{"Content-Type": "application/json"},
			Body:    []byte(`{"ok":true}`),
			Status:  http.StatusCreated,
			Latency: 15 * time.Millisecond,
		})
		return nil
	})

	assert.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.NotEmpty(t, lines)

	var entry map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(lines[len(lines)-1]), &entry))

	assert.Equal(t, "/test.Service/Bidi", entry["grpcMethod"])
	assert.Equal(t, "OK", entry["grpcStatusCode"])
	assert.Equal(t, "stream-id", entry["requestId"])
	assert.Equal(t, true, entry["grpcIsClientStream"])
	assert.Equal(t, true, entry["grpcIsServerStream"])
}
