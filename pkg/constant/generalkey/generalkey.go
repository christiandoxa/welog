// Package generalkey defines common keys used within the application's context for logging
// and request handling. These keys are used to store and retrieve specific values from
// the Fiber context, facilitating consistent and structured logging throughout the application.
package generalkey

// ClientLog is the context key used to store log entries related to client requests.
// This key helps in accumulating log data for outgoing HTTP requests that the server makes.
const ClientLog = "client-log"

// Logger is the context key used to store the logger instance within the context of each request.
// It allows middleware and handlers to access a logger pre-configured with request-specific fields.
const Logger = "logger"

// RequestID is the context key used to store the unique request identifier for each incoming request.
// This key helps track individual requests across various logs and enhances traceability.
const RequestID = "requestId"
