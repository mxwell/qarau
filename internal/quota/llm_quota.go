package quota

import (
	"fmt"
	"math"
	"strconv"

	"github.com/mxwell/qarau/internal/pricing"
)

type LlmQuotaController struct {
	price          pricing.Price
	dailyAllowance float64
}

type TokenUsage struct {
	Input  int64
	Output int64
}

func NewLlmQuotaController(price pricing.Price, daily string) (*LlmQuotaController, error) {
	const maxAllowance = 10

	dailyAllowance, err := strconv.ParseFloat(daily, 64)
	if err != nil {
		return nil, fmt.Errorf("daily allowance parse: %w", err)
	}

	if dailyAllowance > maxAllowance {
		return nil, fmt.Errorf("daily allowance out of range: %f", dailyAllowance)
	}
	return &LlmQuotaController{
		price:          price,
		dailyAllowance: dailyAllowance,
	}, nil
}

func (c LlmQuotaController) CalculateCost(usage TokenUsage) float64 {
	input := math.Max(0, float64(usage.Input))
	output := math.Max(0, float64(usage.Output))
	use := c.price.Input*input + c.price.Output*output
	return use / 1e6
}

func (c LlmQuotaController) Available(usage TokenUsage) (bool, string) {
	use := c.CalculateCost(usage)
	status := fmt.Sprintf("%.3f out of %.3f", use, c.dailyAllowance)
	result := use < c.dailyAllowance-1e-6
	return result, status
}

func (c LlmQuotaController) CalculateUsedPercent(usage TokenUsage) int16 {
	use := c.CalculateCost(usage)
	if use > c.dailyAllowance-1e-6 {
		return 100
	}
	return int16(use * 100 / c.dailyAllowance)
}
