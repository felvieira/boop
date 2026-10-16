// Package openrouter contains server-only OpenRouter catalog and credential calls.
package openrouter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const apiBase = "https://openrouter.ai/api/v1"

var ErrInvalidAPIKey = errors.New("OpenRouter rejected the API key")

type Model struct {
	ID                    string   `json:"id"`
	Name                  string   `json:"name"`
	InputPricePerMillion  *float64 `json:"input_price_per_million"`
	OutputPricePerMillion *float64 `json:"output_price_per_million"`
}

type Client struct {
	httpClient *http.Client
	mu         sync.Mutex
	models     []Model
	modelsAt   time.Time
}

func NewClient() *Client {
	return &Client{httpClient: &http.Client{Timeout: 20 * time.Second}}
}

// ValidateAPIKey checks authentication without making a model completion.
func (c *Client) ValidateAPIKey(ctx context.Context, apiKey string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+"/key", nil)
	if err != nil {
		return fmt.Errorf("create OpenRouter key check: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("OpenRouter key check unavailable: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return ErrInvalidAPIKey
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("OpenRouter key check returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// ListModels returns text models that advertise JSON response-format support.
// The public catalog is cached briefly and contains no account-specific data.
func (c *Client) ListModels(ctx context.Context) ([]Model, error) {
	c.mu.Lock()
	if time.Since(c.modelsAt) < 5*time.Minute && len(c.models) > 0 {
		cached := append([]Model(nil), c.models...)
		c.mu.Unlock()
		return cached, nil
	}
	c.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+"/models?limit=500", nil)
	if err != nil {
		return nil, fmt.Errorf("create OpenRouter model request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("OpenRouter model catalog unavailable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("OpenRouter model catalog returned HTTP %d", resp.StatusCode)
	}

	var payload struct {
		Data []struct {
			ID           string `json:"id"`
			Name         string `json:"name"`
			Architecture struct {
				OutputModalities []string `json:"output_modalities"`
			} `json:"architecture"`
			Pricing struct {
				Prompt     string `json:"prompt"`
				Completion string `json:"completion"`
			} `json:"pricing"`
			SupportedParameters []string `json:"supported_parameters"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode OpenRouter model catalog: %w", err)
	}

	models := make([]Model, 0, len(payload.Data))
	for _, candidate := range payload.Data {
		if candidate.ID == "" || !contains(candidate.Architecture.OutputModalities, "text") {
			continue
		}
		if !contains(candidate.SupportedParameters, "response_format") && !contains(candidate.SupportedParameters, "structured_outputs") {
			continue
		}
		name := strings.TrimSpace(candidate.Name)
		if name == "" {
			name = candidate.ID
		}
		models = append(models, Model{
			ID: candidate.ID, Name: name,
			InputPricePerMillion:  pricePerMillion(candidate.Pricing.Prompt),
			OutputPricePerMillion: pricePerMillion(candidate.Pricing.Completion),
		})
	}
	sort.Slice(models, func(i, j int) bool {
		if models[i].Name == models[j].Name {
			return models[i].ID < models[j].ID
		}
		return models[i].Name < models[j].Name
	})
	if len(models) == 0 {
		return nil, errors.New("OpenRouter model catalog has no JSON-capable text models")
	}
	c.mu.Lock()
	c.models = append([]Model(nil), models...)
	c.modelsAt = time.Now()
	c.mu.Unlock()
	return models, nil
}

func pricePerMillion(value string) *float64 {
	price, err := strconv.ParseFloat(value, 64)
	if err != nil || price < 0 {
		return nil
	}
	perMillion := price * 1_000_000
	return &perMillion
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
