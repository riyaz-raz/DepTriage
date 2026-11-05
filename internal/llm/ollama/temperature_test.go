package ollama

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/deptriage/deptriage/internal/llm"
)

func TestZeroTemperatureIsSerialized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		var options map[string]json.RawMessage
		if err := json.Unmarshal(body["options"], &options); err != nil {
			t.Errorf("decode options: %v", err)
			return
		}
		var temperature float64
		if raw, ok := options["temperature"]; !ok {
			t.Error("request omitted options.temperature")
		} else if err := json.Unmarshal(raw, &temperature); err != nil || temperature != 0 {
			t.Errorf("temperature = %s, want explicit zero", raw)
		}
		writeContent(w, validOutput)
	}))
	defer server.Close()

	if _, err := New(server.URL, "model").Classify(context.Background(), llm.Request{Temp: 0}); err != nil {
		t.Fatal(err)
	}
}
