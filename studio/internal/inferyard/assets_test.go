package inferyard

import "testing"

func TestParseAssetScanValidDirectAndWrapped(t *testing.T) {
	direct := []byte(`{
		"definition": "asset-scan.v1",
		"models": [
			{
				"path": "/models/qwen.gguf",
				"name": "qwen",
				"format": "gguf",
				"size_bytes": 1024,
				"architecture": "qwen2",
				"context_length": 32768,
				"available": true,
				"reason": null
			},
			{
				"path": "/models/dir-model",
				"name": "dir-model",
				"format": "directory",
				"size_bytes": 4096,
				"architecture": null,
				"context_length": null,
				"available": false,
				"reason": "chat_template_unavailable"
			}
		],
		"engines": [
			{
				"path": "/engines/llama-server",
				"receipt": "/engines/runtime-receipt.json",
				"modified_ns": 1700000000
			}
		],
		"model_requests_sent": 0
	}`)
	scan, err := ParseAssetScan(direct)
	if err != nil {
		t.Fatalf("ParseAssetScan(direct) unexpected error: %v", err)
	}
	if scan.Definition != "asset-scan.v1" || len(scan.Models) != 2 || len(scan.Engines) != 1 || scan.ModelRequestsSent != 0 {
		t.Fatalf("unexpected parsed scan: %+v", scan)
	}
	if scan.Models[0].Format != "gguf" || !scan.Models[0].Available || *scan.Models[0].ContextLength != 32768 {
		t.Fatalf("unexpected model[0]: %+v", scan.Models[0])
	}
	if scan.Models[1].Format != "directory" || scan.Models[1].Available || *scan.Models[1].Reason != "chat_template_unavailable" {
		t.Fatalf("unexpected model[1]: %+v", scan.Models[1])
	}
	if scan.Engines[0].Receipt == nil || *scan.Engines[0].Receipt != "/engines/runtime-receipt.json" {
		t.Fatalf("unexpected engine[0]: %+v", scan.Engines[0])
	}

	wrapped := []byte(`{
		"command": "assets scan",
		"status": "scanned",
		"completeness": "complete",
		"run_id": null,
		"evidence_dir": null,
		"limitations": [],
		"details": ` + string(direct) + `
	}`)
	wrappedScan, err := ParseAssetScan(wrapped)
	if err != nil {
		t.Fatalf("ParseAssetScan(wrapped) unexpected error: %v", err)
	}
	if len(wrappedScan.Models) != 2 || len(wrappedScan.Engines) != 1 {
		t.Fatalf("unexpected wrapped scan: %+v", wrappedScan)
	}
}

func TestParseAssetScanRejectsInvalidInputs(t *testing.T) {
	cases := map[string]string{
		"empty":              "",
		"non_object":         "[]",
		"duplicate_keys":     `{"definition":"asset-scan.v1","definition":"asset-scan.v1","models":[],"engines":[],"model_requests_sent":0}`,
		"trailing_json":      `{"definition":"asset-scan.v1","models":[],"engines":[],"model_requests_sent":0}{}`,
		"wrong_definition":   `{"definition":"asset-scan.v2","models":[],"engines":[],"model_requests_sent":0}`,
		"null_models":        `{"definition":"asset-scan.v1","models":null,"engines":[],"model_requests_sent":0}`,
		"null_engines":       `{"definition":"asset-scan.v1","models":[],"engines":null,"model_requests_sent":0}`,
		"nonzero_requests":   `{"definition":"asset-scan.v1","models":[],"engines":[],"model_requests_sent":1}`,
		"extra_top_field":    `{"definition":"asset-scan.v1","models":[],"engines":[],"model_requests_sent":0,"extra":1}`,
		"bad_model_format":   `{"definition":"asset-scan.v1","models":[{"path":"/a","name":"a","format":"onnx","available":true}],"engines":[],"model_requests_sent":0}`,
		"missing_reason":     `{"definition":"asset-scan.v1","models":[{"path":"/a","name":"a","format":"gguf","available":false}],"engines":[],"model_requests_sent":0}`,
		"reason_when_avail":  `{"definition":"asset-scan.v1","models":[{"path":"/a","name":"a","format":"gguf","available":true,"reason":"bad"}],"engines":[],"model_requests_sent":0}`,
		"negative_size":      `{"definition":"asset-scan.v1","models":[{"path":"/a","name":"a","format":"gguf","size_bytes":-1,"available":true}],"engines":[],"model_requests_sent":0}`,
		"zero_context":       `{"definition":"asset-scan.v1","models":[{"path":"/a","name":"a","format":"gguf","context_length":0,"available":true}],"engines":[],"model_requests_sent":0}`,
		"negative_engine_ns": `{"definition":"asset-scan.v1","models":[],"engines":[{"path":"/e","modified_ns":-1}],"model_requests_sent":0}`,
		"wrapped_wrong_cmd":  `{"command":"device-check","status":"scanned","completeness":"complete","details":{"definition":"asset-scan.v1","models":[],"engines":[],"model_requests_sent":0}}`,
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseAssetScan([]byte(payload)); err == nil {
				t.Fatalf("%s: expected error, got nil", name)
			}
		})
	}
}

func TestParseAssetScanExtendedRootsAndTotals(t *testing.T) {
	sixKey := []byte(`{
		"definition": "asset-scan.v1",
		"models": [],
		"engines": [],
		"roots": [
			{"path": "/models", "kind": "models", "disk_usage_bytes": 5120, "disk_free_bytes": 1048576},
			{"path": "/engines", "kind": "engines", "disk_usage_bytes": 0, "disk_free_bytes": null}
		],
		"total_model_bytes": 5120,
		"model_requests_sent": 0
	}`)
	scan, err := ParseAssetScan(sixKey)
	if err != nil {
		t.Fatalf("ParseAssetScan(sixKey) unexpected error: %v", err)
	}
	if len(scan.Roots) != 2 || scan.TotalModelBytes == nil || *scan.TotalModelBytes != 5120 {
		t.Fatalf("unexpected extended scan: %+v", scan)
	}
	if scan.Roots[0].Path != "/models" || scan.Roots[0].Kind != "models" || scan.Roots[0].DiskUsageBytes != 5120 ||
		scan.Roots[0].DiskFreeBytes == nil || *scan.Roots[0].DiskFreeBytes != 1048576 {
		t.Fatalf("unexpected root[0]: %+v", scan.Roots[0])
	}
	if scan.Roots[1].Path != "/engines" || scan.Roots[1].Kind != "engines" || scan.Roots[1].DiskFreeBytes != nil {
		t.Fatalf("unexpected root[1]: %+v", scan.Roots[1])
	}

	wrapped := []byte(`{
		"command": "assets scan",
		"status": "scanned",
		"completeness": "complete",
		"run_id": null,
		"evidence_dir": null,
		"limitations": [],
		"details": ` + string(sixKey) + `
	}`)
	wrappedScan, err := ParseAssetScan(wrapped)
	if err != nil || len(wrappedScan.Roots) != 2 || wrappedScan.TotalModelBytes == nil {
		t.Fatalf("wrapped extended scan failed: scan=%+v err=%v", wrappedScan, err)
	}

	fiveKeyRoots, err := ParseAssetScan([]byte(`{"definition":"asset-scan.v1","models":[],"engines":[],"roots":[],"model_requests_sent":0}`))
	if err != nil || fiveKeyRoots.Roots == nil || fiveKeyRoots.TotalModelBytes != nil {
		t.Fatalf("fiveKeyRoots failed: scan=%+v err=%v", fiveKeyRoots, err)
	}
	fiveKeyTotal, err := ParseAssetScan([]byte(`{"definition":"asset-scan.v1","models":[],"engines":[],"total_model_bytes":0,"model_requests_sent":0}`))
	if err != nil || fiveKeyTotal.Roots != nil || fiveKeyTotal.TotalModelBytes == nil || *fiveKeyTotal.TotalModelBytes != 0 {
		t.Fatalf("fiveKeyTotal failed: scan=%+v err=%v", fiveKeyTotal, err)
	}

	invalidCases := map[string]string{
		"null_total":          `{"definition":"asset-scan.v1","models":[],"engines":[],"total_model_bytes":null,"model_requests_sent":0}`,
		"negative_total":      `{"definition":"asset-scan.v1","models":[],"engines":[],"total_model_bytes":-1,"model_requests_sent":0}`,
		"null_roots":          `{"definition":"asset-scan.v1","models":[],"engines":[],"roots":null,"model_requests_sent":0}`,
		"root_extra_key":      `{"definition":"asset-scan.v1","models":[],"engines":[],"roots":[{"path":"/m","kind":"models","disk_usage_bytes":0,"extra":1}],"model_requests_sent":0}`,
		"root_empty_path":     `{"definition":"asset-scan.v1","models":[],"engines":[],"roots":[{"path":"  ","kind":"models","disk_usage_bytes":0}],"model_requests_sent":0}`,
		"root_bad_kind":       `{"definition":"asset-scan.v1","models":[],"engines":[],"roots":[{"path":"/m","kind":"other","disk_usage_bytes":0}],"model_requests_sent":0}`,
		"root_neg_usage":      `{"definition":"asset-scan.v1","models":[],"engines":[],"roots":[{"path":"/m","kind":"models","disk_usage_bytes":-1}],"model_requests_sent":0}`,
		"root_null_usage":     `{"definition":"asset-scan.v1","models":[],"engines":[],"roots":[{"path":"/m","kind":"models","disk_usage_bytes":null}],"model_requests_sent":0}`,
		"root_neg_free":       `{"definition":"asset-scan.v1","models":[],"engines":[],"roots":[{"path":"/m","kind":"models","disk_usage_bytes":0,"disk_free_bytes":-5}],"model_requests_sent":0}`,
		"root_missing_kind":   `{"definition":"asset-scan.v1","models":[],"engines":[],"roots":[{"path":"/m","disk_usage_bytes":0}],"model_requests_sent":0}`,
		"extra_with_extended": `{"definition":"asset-scan.v1","models":[],"engines":[],"roots":[],"total_model_bytes":0,"model_requests_sent":0,"extra":true}`,
	}
	for name, payload := range invalidCases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseAssetScan([]byte(payload)); err == nil {
				t.Fatalf("%s: expected error, got nil", name)
			}
		})
	}
}
