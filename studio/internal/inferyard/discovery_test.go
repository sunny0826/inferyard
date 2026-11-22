package inferyard

import "testing"

func TestParseServiceDiscoveryAcceptsPolystrataOptionalFields(t *testing.T) {
	raw := []byte(`{
		"command": "service discover",
		"status": "discovered",
		"completeness": "complete",
		"run_id": null,
		"evidence_dir": null,
		"limitations": [],
		"details": {
			"model_requests_sent": 0,
			"services": [
				{
					"pid": 100,
					"process_name": "python",
					"process_start_ticks": 12345,
					"endpoints": [
						{
							"url": "http://127.0.0.1:18080",
							"health": "2xx",
							"models": ["qwen36-local"],
							"reason": null,
							"ambiguous_listener": false,
							"engine_family": "polystrata",
							"loaded": true,
							"max_context": 65536
						}
					]
				},
				{
					"pid": 101,
					"process_name": "llama-server",
					"process_start_ticks": 12346,
					"endpoints": [
						{
							"url": "http://127.0.0.1:8080",
							"health": "2xx",
							"models": ["llama-3"],
							"reason": null,
							"ambiguous_listener": false
						}
					]
				}
			]
		}
	}`)
	got, err := ParseServiceDiscovery(raw, 0)
	if err != nil {
		t.Fatalf("ParseServiceDiscovery failed: %v", err)
	}
	if len(got.Services) != 2 {
		t.Fatalf("expected 2 services, got %d", len(got.Services))
	}
	ps := got.Services[0].Endpoints[0]
	if ps.EngineFamily == nil || *ps.EngineFamily != "polystrata" {
		t.Fatalf("expected engine_family=polystrata, got %v", ps.EngineFamily)
	}
	if ps.Loaded == nil || !*ps.Loaded {
		t.Fatalf("expected loaded=true, got %v", ps.Loaded)
	}
	if ps.MaxContext == nil || *ps.MaxContext != 65536 {
		t.Fatalf("expected max_context=65536, got %v", ps.MaxContext)
	}
	legacy := got.Services[1].Endpoints[0]
	if legacy.EngineFamily != nil || legacy.Loaded != nil || legacy.MaxContext != nil {
		t.Fatalf("expected legacy optional fields to be nil")
	}

	// Unknown field on endpoint must still be rejected.
	bad := []byte(`{
		"command": "service discover",
		"status": "discovered",
		"completeness": "complete",
		"run_id": null,
		"evidence_dir": null,
		"limitations": [],
		"details": {
			"model_requests_sent": 0,
			"services": [
				{
					"pid": 100,
					"process_name": "python",
					"process_start_ticks": 12345,
					"endpoints": [
						{
							"url": "http://127.0.0.1:18080",
							"health": "2xx",
							"models": ["qwen36-local"],
							"reason": null,
							"ambiguous_listener": false,
							"unexpected": true
						}
					]
				}
			]
		}
	}`)
	if _, err := ParseServiceDiscovery(bad, 0); err == nil {
		t.Fatalf("expected unknown endpoint field to fail ParseServiceDiscovery")
	}
}
