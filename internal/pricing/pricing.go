package pricing

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

// Price is in USD per 1M tokens.
type Price struct {
	Input  float64 `json:"input"`
	Output float64 `json:"output"`
}

//go:embed prices.json
var pricesJson []byte

func All() (map[string]Price, error) {
	var prices map[string]Price
	if err := json.Unmarshal(pricesJson, &prices); err != nil {
		return nil, fmt.Errorf("prices.json parse fail: %w", err)
	}
	return prices, nil
}

func Lookup(model string) (Price, error) {
	const (
		minPrice = 1e-6
		maxPrice = 10
	)

	prices, err := All()
	if err != nil {
		return Price{}, err
	}

	price, ok := prices[model]
	if !ok {
		return Price{}, fmt.Errorf("no known price for model %q", model)
	}
	if price.Input < minPrice || price.Input > maxPrice {
		return Price{}, fmt.Errorf("input price out of range for model %q: %f", model, price.Input)
	}
	if price.Output < minPrice || price.Output > maxPrice {
		return Price{}, fmt.Errorf("output price out of range for model %q: %f", model, price.Output)
	}
	return price, nil
}
