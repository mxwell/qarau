package fiberutil

import "github.com/gofiber/fiber/v2"

func errorJson(c *fiber.Ctx, status int, message string) error {
	return c.Status(status).JSON(fiber.Map{
		"error": fiber.Map{
			"message": message,
		},
	})
}

func InternalError(c *fiber.Ctx, message string) error {
	return errorJson(c, fiber.StatusInternalServerError, message)
}

func UnauthorizedError(c *fiber.Ctx, message string) error {
	return errorJson(c, fiber.StatusUnauthorized, message)
}

func BadRequest(c *fiber.Ctx, message string) error {
	return errorJson(c, fiber.StatusBadRequest, message)
}

func NotFound(c *fiber.Ctx, message string) error {
	return errorJson(c, fiber.StatusNotFound, message)
}
