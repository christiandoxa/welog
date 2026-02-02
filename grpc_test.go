package welog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os/user"
	"strings"
	"testing"
	"time"

	"github.com/agiledragon/gomonkey/v2"
	"github.com/christiandoxa/welog/pkg/constant/generalkey"
	"github.com/christiandoxa/welog/pkg/model"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/protobuf/types/known/structpb"
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

type badJSON struct{}

func (badJSON) MarshalJSON() ([]byte, error) { return nil, errors.New("marshal error") }

type badUnmarshalJSON struct{}

func (badUnmarshalJSON) MarshalJSON() ([]byte, error) { return []byte("[]"), nil }

func TestFetchRequestIDFromContextValue(t *testing.T) {
	ctx := context.WithValue(context.Background(), generalkey.RequestID, "ctx-id")
	assert.Equal(t, "ctx-id", fetchRequestID(ctx))
}

func TestFetchRequestIDFromMetadataHeader(t *testing.T) {
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-request-id", "meta-id"))
	assert.Equal(t, "meta-id", fetchRequestID(ctx))
}

func TestFetchRequestIDFromMetadataFallbackKey(t *testing.T) {
	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyFuncReturn(strings.ToLower, "x-request-id-header")
	assert.Equal(t, "x-request-id-header", strings.ToLower("X-Request-ID"))

	md := metadata.Pairs(requestIDMetadataKey, "fallback-id")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	assert.Equal(t, "fallback-id", fetchRequestID(ctx))
}

func TestFetchRequestIDGeneratesNew(t *testing.T) {
	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyFuncReturn(uuid.NewString, "generated-id")

	assert.Equal(t, "generated-id", fetchRequestID(context.Background()))
}

func TestFetchLoggerUsesExisting(t *testing.T) {
	base := logrus.New()
	entry := logrus.NewEntry(base)
	ctx := context.WithValue(context.Background(), generalkey.Logger, entry)

	got := fetchLogger(ctx, "rid")
	require.NotNil(t, got)
	assert.Equal(t, "rid", got.Data[generalkey.RequestID])
}

func TestFetchLoggerDefault(t *testing.T) {
	got := fetchLogger(context.Background(), "rid")
	require.NotNil(t, got)
	assert.Equal(t, "rid", got.Data[generalkey.RequestID])
}

func TestFetchClientLogCopiesSlice(t *testing.T) {
	original := []logrus.Fields{{"key": "value"}}
	ctx := context.WithValue(context.Background(), generalkey.ClientLog, original)
	got := fetchClientLog(ctx)
	require.NotNil(t, got)
	assert.Equal(t, original, *got)
	assert.NotSame(t, &original, got)
}

func TestFetchClientLogPointer(t *testing.T) {
	original := []logrus.Fields{{"key": "value"}}
	ctx := context.WithValue(context.Background(), generalkey.ClientLog, &original)
	got := fetchClientLog(ctx)
	require.NotNil(t, got)
	assert.Equal(t, original, *got)
	assert.Same(t, &original, got)
}

func TestFetchClientLogDefault(t *testing.T) {
	got := fetchClientLog(context.Background())
	require.NotNil(t, got)
	assert.Len(t, *got, 0)
}

func TestMarshalPayloadNil(t *testing.T) {
	fields, raw := marshalPayload(nil)
	assert.Empty(t, fields)
	assert.Empty(t, raw)
}

func TestMarshalPayloadBadJSON(t *testing.T) {
	fields, raw := marshalPayload(badJSON{})
	assert.Empty(t, fields)
	assert.Equal(t, "{}", raw)
}

func TestMarshalPayloadProto(t *testing.T) {
	msg, err := structpb.NewStruct(map[string]interface{}{"foo": "bar"})
	require.NoError(t, err)

	fields, raw := marshalPayload(msg)
	assert.Equal(t, "bar", fields["foo"])
	assert.Contains(t, raw, "\"foo\"")
}

func TestMarshalPayloadUnmarshalError(t *testing.T) {
	fields, raw := marshalPayload(badUnmarshalJSON{})
	assert.Equal(t, "[]", raw)
	assert.Empty(t, fields)
}

func TestMetadataToMap(t *testing.T) {
	assert.Empty(t, metadataToMap(context.Background()))

	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("k", "v1", "k", "v2"))
	out := metadataToMap(ctx)
	vals, ok := out["k"].([]string)
	require.True(t, ok)
	assert.Equal(t, []string{"v1", "v2"}, vals)
}

func TestLogGRPCUnaryError(t *testing.T) {
	entry := logrus.NewEntry(logrus.New())
	logGRPCUnary(grpcUnaryLogContext{
		ctx:       context.Background(),
		entry:     entry,
		info:      &grpc.UnaryServerInfo{FullMethod: "/test"},
		request:   map[string]string{"a": "b"},
		response:  nil,
		start:     time.Now(),
		err:       errors.New("boom"),
		requestID: "rid",
		clientLog: &[]logrus.Fields{},
	})
}

func TestLogGRPCStreamError(t *testing.T) {
	entry := logrus.NewEntry(logrus.New())
	logGRPCStream(context.Background(), entry, &grpc.StreamServerInfo{FullMethod: "/test"}, time.Now(), errors.New("boom"), "rid", &[]logrus.Fields{})
}

func TestPeerAddress(t *testing.T) {
	assert.Equal(t, "", peerAddress(context.Background()))

	p := &peer.Peer{Addr: fakeAddr("127.0.0.1:1234")}
	ctx := peer.NewContext(context.Background(), p)
	assert.Equal(t, "127.0.0.1:1234", peerAddress(ctx))
}

type fakeAddr string

func (f fakeAddr) Network() string { return "tcp" }
func (f fakeAddr) String() string  { return string(f) }

func TestReadClientLogNil(t *testing.T) {
	assert.Nil(t, readClientLog(nil))
}

func TestUsernameError(t *testing.T) {
	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyFuncReturn(user.Current, (*user.User)(nil), errors.New("user error"))

	assert.Equal(t, "unknown", username())
}
