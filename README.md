# Welog

`Welog` is a logging library designed for Go applications, integrating with ElasticSearch and utilizing `logrus` for structured logging. It supports log management for Go applications running on popular web frameworks like Fiber and Gin, providing detailed request and response logging.

## Requirements

Before using this library, ensure you have the following environment variables set up in your `.env` file:

```bash
ELASTIC_URL=http://127.0.0.1:9200
ELASTIC_USERNAME=elastic
ELASTIC_PASSWORD=changeme
ELASTIC_INDEX=welog
```

## Installation

To install `welog`, add the library to your Go project by running:

```bash
go get github.com/christiandoxa/welog
```

Additionally, install `godotenv` to manage environment variables:

```bash
go get github.com/joho/godotenv
```

## Usage

### Middleware Setup in Fiber

To use the `welog` middleware in a Fiber application, add the middleware as follows:

```go
app := fiber.New()
app.Use(welog.NewFiber(fiber.Config{}))
```

### Middleware Setup in Gin

To use the `welog` middleware in a Gin application, set up the middleware as follows:

```go
router := gin.Default()
router.Use(welog.NewGin())
```

### Logging Client Requests

#### Logging Client Requests in Fiber

When using a custom Fiber client, you can log client requests with `welog` using the following method:

```go
welog.LogFiberClient(
    c,
    requestURL,
    requestMethod,
    requestContentType,
    requestHeader,
    requestBody,
    responseHeader,
    responseBody,
    responseStatus,
    requestTime,
    responseLatency,
)
```

- `c`: The Fiber context.
- Other parameters: Include details of the request and response such as URL, method, headers, body, status, and timing.

#### Logging Client Requests in Gin

For custom logging of client requests within Gin, use the `LogGinClient` method:

```go
welog.LogGinClient(
    c,
    requestURL,
    requestMethod,
    requestContentType,
    requestHeader,
    requestBody,
    responseHeader,
    responseBody,
    responseStatus,
    requestTime,
    responseLatency,
)
```

- `c`: The Gin context.
- Other parameters: Include details of the request and response such as URL, method, headers, body, status, and timing.

### Logging Outside of Handlers

If you need to log errors or other information outside of a Fiber or Gin handler, you can directly use the `logger.Logger()` instance:

```go
logger.Logger().Fatal(err)
```

### Logging Inside Handlers in Fiber

When logging within a Fiber handler, use the logger instance stored in the Fiber context to ensure consistent and contextual logging:

```go
c.Locals("logger").(*logrus.Entry).Error(err)
```

### Logging Inside Handlers in Gin

When logging within a Gin handler, use the logger instance stored in the Gin context to ensure consistent and contextual logging:

```go
c.MustGet("logger").(*logrus.Entry).Error(err)
```

## Contributing

Contributions to `welog` are welcome! If you have suggestions, bug reports, or want to contribute code, please create a pull request or open an issue on GitHub.

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
