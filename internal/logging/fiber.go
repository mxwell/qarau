package logging

import (
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v2"
)

// FiberRequestLogger returns a middleware that emits one structured log line
// per HTTP request. Level is info for <400, warn for 4xx, error for 5xx or
// when the handler returned an error.
func FiberRequestLogger(log *slog.Logger) fiber.Handler {
	return func(c *fiber.Ctx) error {
		start := time.Now()
		err := c.Next()
		status := c.Response().StatusCode()

		attrs := []any{
			"method", c.Method(),
			"path", c.Path(),
			"status", status,
			"latency_ms", time.Since(start).Milliseconds(),
			"ip", c.IP(),
			"bytes", len(c.Response().Body()),
		}
		if err != nil {
			attrs = append(attrs, "err", err.Error())
		}

		switch {
		case err != nil || status >= 500:
			log.Error("request", attrs...)
		case status >= 400:
			log.Warn("request", attrs...)
		default:
			log.Info("request", attrs...)
		}
		return err
	}
}
