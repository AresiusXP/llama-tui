// Package llamaserver — metrics.go fetches runtime performance data from a
// running llama-server instance via its /slots and /metrics endpoints.
package llamaserver

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ServerMetrics holds a snapshot of llama-server runtime performance data.
type ServerMetrics struct {
	// GenerationTPS is the average token generation speed in tokens/second.
	// Only populated when the --metrics flag is enabled on llama-server.
	GenerationTPS float64

	// PromptTPS is the average prompt ingestion speed in tokens/second.
	// Only populated when the --metrics flag is enabled on llama-server.
	PromptTPS float64

	// RequestsProcessing is the number of in-flight inference requests.
	// Only populated when the --metrics flag is enabled on llama-server.
	RequestsProcessing int

	// ActiveSlots is the number of slots currently generating tokens.
	// Sourced from GET /slots (always available).
	ActiveSlots int

	// TotalSlots is the total number of parallel slots configured.
	// Sourced from GET /slots (always available).
	TotalSlots int

	// MetricsAvailable is true when the /metrics endpoint returned data.
	// False when llama-server was started without --metrics.
	MetricsAvailable bool
}

// ServerMetricsMsg is the Bubble Tea message delivered to app.Update
// after a metrics poll completes.
// Epoch matches the metricsEpoch field on app.Model — messages from a stale
// polling loop (from a previous model load) are discarded if the epoch differs.
type ServerMetricsMsg struct {
	Metrics ServerMetrics
	Epoch   int
}

// metricsHTTPClient is a shared client with a tight timeout for polling.
var metricsHTTPClient = &http.Client{Timeout: 3 * time.Second}

// FetchMetrics queries the running llama-server for performance data.
// It always fetches /slots (always available) and optionally /metrics
// (requires --metrics flag). Errors are silently swallowed — the caller
// should treat a partially-filled ServerMetrics as best-effort data.
func FetchMetrics(ctx context.Context, baseURL string, metricsEnabled bool) ServerMetrics {
	var m ServerMetrics
	m.ActiveSlots, m.TotalSlots = fetchSlots(ctx, baseURL)
	if metricsEnabled {
		m.GenerationTPS, m.PromptTPS, m.RequestsProcessing, m.MetricsAvailable = fetchPrometheusMetrics(ctx, baseURL)
	}
	return m
}

// slotResponse is the minimal JSON shape returned by GET /slots.
type slotResponse struct {
	IsProcessing bool `json:"is_processing"`
}

// fetchSlots calls GET /slots and returns (activeSlots, totalSlots).
// Returns (0, 0) on any error.
func fetchSlots(ctx context.Context, baseURL string) (active, total int) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/slots", nil)
	if err != nil {
		return 0, 0
	}
	resp, err := metricsHTTPClient.Do(req)
	if err != nil {
		return 0, 0
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, 0
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, 0
	}

	var slots []slotResponse
	if err := json.Unmarshal(body, &slots); err != nil {
		return 0, 0
	}

	total = len(slots)
	for _, s := range slots {
		if s.IsProcessing {
			active++
		}
	}
	return active, total
}

// fetchPrometheusMetrics calls GET /metrics and parses the Prometheus text
// format for the three gauge metrics we care about.
// Returns (generationTPS, promptTPS, requestsProcessing, ok).
func fetchPrometheusMetrics(ctx context.Context, baseURL string) (genTPS, promptTPS float64, reqProcessing int, ok bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/metrics", nil)
	if err != nil {
		return 0, 0, 0, false
	}
	resp, err := metricsHTTPClient.Do(req)
	if err != nil {
		return 0, 0, 0, false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, 0, 0, false
	}

	// Prometheus text format: each metric line is "name value [timestamp]".
	// Comment lines start with #. We only need 3 specific gauge names.
	scanner := bufio.NewScanner(resp.Body)
	found := 0
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := fields[0]
		// Strip Prometheus label block if present (e.g. metric{label="v"} → metric).
		if idx := strings.Index(name, "{"); idx != -1 {
			name = name[:idx]
		}
		val, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			continue
		}
		switch name {
		case "llamacpp:predicted_tokens_seconds":
			genTPS = val
			found++
		case "llamacpp:prompt_tokens_seconds":
			promptTPS = val
			found++
		case "llamacpp:requests_processing":
			reqProcessing = int(val)
			found++
		}
		if found == 3 {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, 0, 0, false
	}

	return genTPS, promptTPS, reqProcessing, found > 0
}
