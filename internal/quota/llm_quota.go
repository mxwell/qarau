package quota

import (
	"fmt"
	"math"
	"strconv"
)

type LlmQuotaController struct {
	inputPricePer1MTok  float64
	outputPricePer1MTok float64
	dailyAllowance      float64
}

type TokenUsage struct {
	Input  int64
	Output int64
}

func NewLlmQuotaController(inputPrice, outputPrice, daily string) (*LlmQuotaController, error) {
	const (
		minPrice     = 1e-6
		maxPrice     = 10
		maxAllowance = 10
	)

	inputPricePer1MTok, err := strconv.ParseFloat(inputPrice, 64)
	if err != nil {
		return nil, fmt.Errorf("input price parse: %w", err)
	}

	if inputPricePer1MTok < minPrice || inputPricePer1MTok > maxPrice {
		return nil, fmt.Errorf("input price out of range: %f", inputPricePer1MTok)
	}

	outputPricePer1MTok, err := strconv.ParseFloat(outputPrice, 64)
	if err != nil {
		return nil, fmt.Errorf("output price parse: %w", err)
	}
	if outputPricePer1MTok < minPrice || outputPricePer1MTok > maxPrice {
		return nil, fmt.Errorf("output price out of range: %f", outputPricePer1MTok)
	}

	dailyAllowance, err := strconv.ParseFloat(daily, 64)
	if err != nil {
		return nil, fmt.Errorf("daily allowance parse: %w", err)
	}

	if dailyAllowance > maxAllowance {
		return nil, fmt.Errorf("daily allowance out of range: %f", dailyAllowance)
	}
	return &LlmQuotaController{
		inputPricePer1MTok:  inputPricePer1MTok,
		outputPricePer1MTok: outputPricePer1MTok,
		dailyAllowance:      dailyAllowance,
	}, nil
}

func (c LlmQuotaController) CalculateCost(usage TokenUsage) float64 {
	input := math.Max(0, float64(usage.Input))
	output := math.Max(0, float64(usage.Output))
	use := c.inputPricePer1MTok*input + c.outputPricePer1MTok*output
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
