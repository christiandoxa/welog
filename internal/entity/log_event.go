package entity

import (
	"time"

	"github.com/sirupsen/logrus"
)

// Payload represents a sanitized body payload ready for structured logging.
type Payload struct {
	Fields logrus.Fields
	String string
}

// HTTPLogEvent contains framework-neutral HTTP request and response log data.
type HTTPLogEvent struct {
	RequestAgent       string
	RequestBody        Payload
	RequestContentType string
	RequestHeader      map[string]any
	RequestHostName    string
	RequestID          any
	RequestIP          string
	RequestMethod      string
	RequestProtocol    string
	RequestTimestamp   time.Time
	RequestURL         string
	ResponseBody       Payload
	ResponseHeader     map[string]any
	ResponseLatency    time.Duration
	ResponseStatus     int
	ResponseUser       string
	Target             []logrus.Fields
}

// GRPCUnaryLogEvent contains framework-neutral unary gRPC log data.
type GRPCUnaryLogEvent struct {
	Method           string
	Request          Payload
	RequestMeta      map[string]any
	Peer             string
	StatusCode       string
	Error            string
	Response         Payload
	RequestID        string
	RequestTimestamp time.Time
	ResponseLatency  time.Duration
	ResponseUser     string
	Target           []logrus.Fields
}

// GRPCStreamLogEvent contains framework-neutral streaming gRPC lifecycle log data.
type GRPCStreamLogEvent struct {
	Method           string
	RequestMeta      map[string]any
	Peer             string
	StatusCode       string
	Error            string
	IsClientStream   bool
	IsServerStream   bool
	RequestID        string
	RequestTimestamp time.Time
	ResponseLatency  time.Duration
	ResponseUser     string
	Target           []logrus.Fields
}
