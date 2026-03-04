# Welog [![Go Test](https://github.com/christiandoxa/welog/actions/workflows/test.yml/badge.svg?branch=main)](https://github.com/christiandoxa/welog/actions/workflows/test.yml)

**Welog** is a structured logging library for Go applications, integrating with **Elasticsearch** and powered
by [Logrus](https://github.com/sirupsen/logrus).  
It ships with a built-in **ECS 9.3.0 JSON formatter** and provides detailed request/response logging for **Fiber**, *
*Gin**, and **gRPC** (via go-grpc-middleware), covering
both server-side and client-side calls.

---

## Features

- 📦 **Plug-and-play** middleware for Fiber, Gin, and gRPC interceptors
- 📝 **Structured JSON logs** via Logrus with a built-in ECS formatter
- 🧩 **ECS 9.3.0-compatible fields** such as `@timestamp`, `log.level`, `ecs.version`, `log.origin.*`, and `error.*`
- 🔍 **Detailed request/response tracing** with latency and metadata
- 🔗 **Elasticsearch integration** for centralized log storage
- 🎯 **Context-aware logging** for handlers
- 🔄 **Client request logging** for outbound HTTP calls
- 🔌 **gRPC interceptors** ready for go-grpc-middleware chains

---

## Installation

```bash
go get github.com/christiandoxa/welog
```

No additional ECS formatter dependency is required.

---

## Quick Start

```go
package main

import (
	"github.com/christiandoxa/welog"
	"github.com/gofiber/fiber/v2"
)

func main() {
	// 1. Configure Elasticsearch connection
	welog.SetConfig(welog.Config{
		ElasticIndex:    "my-logs",
		ElasticURL:      "http://localhost:9200",
		ElasticUsername: "elastic",
		ElasticPassword: "changeme",
	})

	// 2. Create Fiber app and attach Welog middleware
	app := fiber.New()
	app.Use(welog.NewFiber(fiber.Config{}))

	// Example route
	app.Get("/", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"message": "hello world"})
	})

	app.Listen(":3000")
}
```

---

## Configuration

Welog exposes `welog.Config` to store Elasticsearch connection details:

```go
type Config struct {
	ElasticIndex    string
	ElasticURL      string
	ElasticUsername string
	ElasticPassword string
}
```

Set it at application startup using:

```go
welog.SetConfig(welog.Config{
	ElasticIndex:    "my-logs",
	ElasticURL:      "http://localhost:9200",
	ElasticUsername: "elastic",
	ElasticPassword: "changeme",
})
```

### Environment Variables Used

| Variable             | Description              |
|----------------------|--------------------------|
| `ELASTIC_INDEX__`    | Elasticsearch index name |
| `ELASTIC_URL__`      | Elasticsearch base URL   |
| `ELASTIC_USERNAME__` | Elasticsearch username   |
| `ELASTIC_PASSWORD__` | Elasticsearch password   |

---

## Middleware Setup

### Fiber

```go
app := fiber.New()
app.Use(welog.NewFiber(fiber.Config{}))
```

### Gin

```go
router := gin.Default()
router.Use(welog.NewGin())
```

### gRPC

```go
import (
	grpcmiddleware "github.com/grpc-ecosystem/go-grpc-middleware/v2"
	"google.golang.org/grpc"
)

server := grpc.NewServer(
	grpcmiddleware.WithUnaryServerChain(welog.NewGRPCUnary()),
	grpcmiddleware.WithStreamServerChain(welog.NewGRPCStream()),
)
```

---

## Client Request Logging

Welog supports logging outbound HTTP requests from within your application.

### Fiber Example

```go
import (
	"net/http"
	"time"

	"github.com/christiandoxa/welog/pkg/model"
)

reqModel := model.TargetRequest{
	URL:         "https://example.com/api",
	Method:      "GET",
	ContentType: "application/json",
	Header:      map[string]interface{}{"Authorization": "Bearer token"},
	Body:        []byte(`{"param":"value"}`),
	Timestamp:   time.Now(),
}

resModel := model.TargetResponse{
	Header:  map[string]interface{}{"Content-Type": "application/json"},
	Body:    []byte(`{"status":"ok"}`),
	Status:  http.StatusOK,
	Latency: 200 * time.Millisecond,
}

welog.LogFiberClient(c, reqModel, resModel)
```

### Gin Example

```go
welog.LogGinClient(c, reqModel, resModel)
```

### gRPC Example

```go
welog.LogGRPCClient(ctx, reqModel, resModel)
```

---

## Logging Inside Handlers

Welog stores a contextual `*logrus.Entry` inside the request context.

### Fiber

```go
c.Locals("logger").(*logrus.Entry).Error("Something went wrong")
```

### Gin

```go
c.MustGet("logger").(*logrus.Entry).Error("Something went wrong")
```

### gRPC

```go
entry := ctx.Value("logger").(*logrus.Entry)
entry.Error("Something went wrong")
```

---

## Log Format

All logs emitted through `pkg/infrastructure/logger` use Welog's built-in ECS formatter. This means request logs,
client-call logs, and direct logger usage share the same root ECS fields while still keeping Welog-specific request
payload fields.

Common ECS fields emitted by the formatter:

- `@timestamp`
- `message`
- `log.level`
- `ecs.version`
- `log.origin.function`
- `log.origin.file.name`
- `log.origin.file.line`
- `error.message`
- `error.type`
- `error.stack_trace` when the error supports extended formatting

This formatter is bundled inside the project, so Welog no longer depends on `go.elastic.co/ecslogrus`.

---

## Direct Logger Usage

Need a logger outside of HTTP/gRPC middleware? Use the singleton directly after configuring Welog (via `SetConfig` or
environment variables):

```go
import (
	"github.com/christiandoxa/welog/pkg/infrastructure/logger"
)

func connectWithCache(dsn string) {
	// your connection logic here...
	logger.Logger().Info("Using cached connection for DSN:", dsn)
}
```

Direct logger calls use the same ECS formatter as the middleware-generated request logs.

---

## Sample Output

Example JSON log entry generated by `logFiber` (abbreviated):

```json
{
  "@timestamp": "2026-03-04T10:15:30.123Z",
  "ecs.version": "9.3.0",
  "log.level": "info",
  "log.origin.function": "github.com/christiandoxa/welog.logFiber",
  "log.origin.file.name": "welog.go",
  "log.origin.file.line": 122,
  "message": "",
  "requestId": "8e0a34bb-0b90-43c4-911c-f0a85f5c0dd2",
  "requestAgent": "PostmanRuntime/7.31.1",
  "requestBody": {
    "foo": "bar"
  },
  "requestMethod": "POST",
  "requestUrl": "http://localhost/api/v1/resource",
  "requestTimestamp": "2026-03-04T10:15:30.000000000Z",
  "responseStatus": 200,
  "responseLatency": "150ms",
  "responseTimestamp": "2026-03-04T10:15:30.150000000Z",
  "target": [
    {
      "targetRequestMethod": "GET",
      "targetRequestURL": "https://example.com/api/data",
      "targetResponseStatus": 200,
      "targetResponseLatency": "200ms"
    }
  ]
}
```

---

## Contributing

Contributions are welcome! Please open an issue or submit a pull request.

Run the test suite with coverage:

```bash
$GOPATH/bin/gotestsum -- -coverprofile=coverage.out -covermode=count ./...
go tool cover -func coverage.out | grep total
```

---

## License

MIT License — see [LICENSE](LICENSE) for details.
