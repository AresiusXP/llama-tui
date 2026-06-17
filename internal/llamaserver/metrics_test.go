package llamaserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchSlots_Mixed(t *testing.T) {
	slots := []slotResponse{
		{IsProcessing: true},
		{IsProcessing: false},
		{IsProcessing: true},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(slots)
	}))
	defer srv.Close()

	active, total := fetchSlots(context.Background(), srv.URL)
	if total != 3 {
		t.Fatalf("expected total=3, got %d", total)
	}
	if active != 2 {
		t.Fatalf("expected active=2, got %d", active)
	}
}

func TestFetchSlots_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	active, total := fetchSlots(context.Background(), srv.URL)
	if active != 0 || total != 0 {
		t.Fatalf("expected (0,0), got (%d,%d)", active, total)
	}
}

func TestFetchSlots_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "not json")
	}))
	defer srv.Close()

	active, total := fetchSlots(context.Background(), srv.URL)
	if active != 0 || total != 0 {
		t.Fatalf("expected (0,0), got (%d,%d)", active, total)
	}
}

func prometheusBody(genTPS, promptTPS float64, reqProcessing int) string {
	return fmt.Sprintf(`# HELP llamacpp:predicted_tokens_seconds Predicted tokens per second
# TYPE llamacpp:predicted_tokens_seconds gauge
llamacpp:predicted_tokens_seconds %.2f
# HELP llamacpp:prompt_tokens_seconds Prompt tokens per second
llamacpp:prompt_tokens_seconds %.2f
llamacpp:requests_processing %d
`, genTPS, promptTPS, reqProcessing)
}

func TestFetchPrometheusMetrics_AllThree(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, prometheusBody(42.5, 100.0, 3))
	}))
	defer srv.Close()

	genTPS, promptTPS, reqProcessing, ok := fetchPrometheusMetrics(context.Background(), srv.URL)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if genTPS != 42.5 {
		t.Errorf("expected genTPS=42.5, got %f", genTPS)
	}
	if promptTPS != 100.0 {
		t.Errorf("expected promptTPS=100.0, got %f", promptTPS)
	}
	if reqProcessing != 3 {
		t.Errorf("expected reqProcessing=3, got %d", reqProcessing)
	}
}

func TestFetchPrometheusMetrics_LabelDecorated(t *testing.T) {
	// Metric names may come with Prometheus label blocks; we should strip them.
	body := `llamacpp:predicted_tokens_seconds{model="test"} 7.0
llamacpp:prompt_tokens_seconds{model="test"} 8.0
llamacpp:requests_processing{model="test"} 1
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	genTPS, promptTPS, reqProcessing, ok := fetchPrometheusMetrics(context.Background(), srv.URL)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if genTPS != 7.0 || promptTPS != 8.0 || reqProcessing != 1 {
		t.Errorf("unexpected values: gen=%f prompt=%f req=%d", genTPS, promptTPS, reqProcessing)
	}
}

func TestFetchPrometheusMetrics_NoKnownMetrics(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "# HELP other_metric A metric\nother_metric 1.0\n")
	}))
	defer srv.Close()

	_, _, _, ok := fetchPrometheusMetrics(context.Background(), srv.URL)
	if ok {
		t.Fatal("expected ok=false when no known metrics present")
	}
}

func TestFetchMetrics_MetricsDisabled(t *testing.T) {
	slotsCalled := false
	metricsCalled := false

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/slots":
			slotsCalled = true
			json.NewEncoder(w).Encode([]slotResponse{{IsProcessing: true}})
		case "/metrics":
			metricsCalled = true
			fmt.Fprint(w, prometheusBody(1.0, 2.0, 1))
		}
	}))
	defer srv.Close()

	m := FetchMetrics(context.Background(), srv.URL, false)
	if !slotsCalled {
		t.Error("expected /slots to be called")
	}
	if metricsCalled {
		t.Error("expected /metrics NOT to be called when metricsEnabled=false")
	}
	if m.MetricsAvailable {
		t.Error("expected MetricsAvailable=false")
	}
}

func TestFetchMetrics_MetricsEnabled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/slots":
			json.NewEncoder(w).Encode([]slotResponse{{IsProcessing: false}, {IsProcessing: false}})
		case "/metrics":
			fmt.Fprint(w, prometheusBody(10.0, 20.0, 0))
		}
	}))
	defer srv.Close()

	m := FetchMetrics(context.Background(), srv.URL, true)
	if !m.MetricsAvailable {
		t.Error("expected MetricsAvailable=true")
	}
	if m.TotalSlots != 2 {
		t.Errorf("expected TotalSlots=2, got %d", m.TotalSlots)
	}
	if m.GenerationTPS != 10.0 {
		t.Errorf("expected GenerationTPS=10.0, got %f", m.GenerationTPS)
	}
}
