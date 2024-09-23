# welog

`welog` is a logging library designed for Go applications, integrating with ElasticSearch and utilizing `logrus` for structured logging. It supports log management for Go applications running on the Fiber web framework and provides detailed request and response logging.

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
app.Use(welog.NewFiber())
```

### Logging Client Requests

When using a custom Fiber client, you can log the requests and responses using the `LogFiberClient` function. Here's an example:

```go
welog.LogFiberClient(c *fiber.Ctx, url string, method string, contentType string, header map[string]interface{}, body []byte, response []byte, status int, start time.Time, elapsed time.Duration)
```

### Logging Outside of Handlers

If you need to log errors or other information outside of a Fiber handler, you can directly use the `logger.Logger()` instance:

```go
logger.Logger().Fatal(err)
```

### Logging Inside Handlers

When logging within a handler, use the logger instance from the request context for consistent and contextual logging:

```go
c.Locals("logger").(*logrus.Entry).Error(err)
```

### Features

- **ElasticSearch Integration:** Logs are sent to ElasticSearch with customizable index names based on the current date.
- **Structured Logging with Logrus:** Logs are structured and formatted using `logrus`, ensuring readability and easier log analysis.
- **Automatic Logger Reinitialization:** The logger is automatically reinitialized in case of connection issues with ElasticSearch.
- **Fiber Request and Response Logging:** Logs detailed information about HTTP requests and responses, including headers, body content, status, and more.

### Environment Variables

- `ELASTIC_URL`: The URL of your ElasticSearch server.
- `ELASTIC_USERNAME`: The username for ElasticSearch authentication.
- `ELASTIC_PASSWORD`: The password for ElasticSearch authentication.
- `ELASTIC_INDEX`: The index name used in ElasticSearch for storing logs. The index name is appended with the current year and month.

### Example Code

Below is a complete example of how to set up and use the `welog` library in a Fiber application:

```go
package main

import (
    "log"
    "github.com/gofiber/fiber/v2"
    "github.com/christiandoxa/welog"
    "github.com/joho/godotenv"
)

func main() {
    // Load environment variables from .env file
    if err := godotenv.Load(); err != nil {
        log.Fatal("Error loading .env file")
    }

    app := fiber.New()

    // Use the welog middleware
    app.Use(welog.NewFiber())

    app.Get("/", func(c *fiber.Ctx) error {
        // Your handler code
        return c.SendString("Hello, World!")
    })

    app.Listen(":3000")
}
```

### License

This library is open-source and free to use. Feel free to contribute or modify the code as needed for your projects.
