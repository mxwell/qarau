package logging

import (
	"errors"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v2"
)

// FiberRequestLogger returns a middleware that emits one structured log line
// per HTTP request. Level is info for <400, warn for 4xx, error for 5xx.
func FiberRequestLogger(log *slog.Logger) fiber.Handler {
	return func(c *fiber.Ctx) error {
		start := time.Now()
		err := c.Next()
		status := c.Response().StatusCode()
		if err != nil {
			// fiber's ErrorHandler runs after the middleware chain, so the
			// response status doesn't reflect err yet.
			status = fiber.StatusInternalServerError
			if fe, ok := errors.AsType[*fiber.Error](err); ok {
				status = fe.Code
			}
		}

		attrs := []any{
			"method", c.Method(),
			"path", c.Path(),
			"status", status,
			"latency_ms", time.Since(start).Milliseconds(),
			"ip", c.IP(),
			"bytes", len(c.Response().Body()),
		}
		if err != nil {
			attrs = append(attrs, "err", err)
		}

		switch {
		case status >= 500:
			log.Error("request", attrs...)
		case status >= 400:
			log.Warn("request", attrs...)
		default:
			log.Info("request", attrs...)
		}
		return err
	}
}

// Strip any cookies to prevent Sentry from
// polluting the log with cookie parsing errors
func StripRequestCookies() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Request().Header.DelAllCookies()
		return c.Next()
	}
}
