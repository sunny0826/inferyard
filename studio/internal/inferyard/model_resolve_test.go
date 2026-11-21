package inferyard

import "testing"

func TestParseModelResolveValidDirectAndWrapped(t *testing.T) {
	sha := "23749fefcc72300e3a2ad315e1317431b06b590a"
	direct := []byte(`{
		"definition": "model-resolve.v1",
		"platform": "huggingface",
		"repository": "Qwen/Qwen3-0.6B-GGUF",
		"revision": "` + sha + `",
		"path": "Qwen3-0.6B-Q8_0.gguf",
		"pinned_url": "https://huggingface.co/Qwen/Qwen3-0.6B-GGUF/blob/` + sha + `/Qwen3-0.6B-Q8_0.gguf",
		"download_url": "https://huggingface.co/Qwen/Qwen3-0.6B-GGUF/resolve/` + sha + `/Qwen3-0.6B-Q8_0.gguf",
		"filename": "Qwen3-0.6B-Q8_0.gguf",
		"bytes": 639446688,
		"already_pinned": false,
		"model_requests_sent": 0
	}`)
	parsed, err := ParseModelResolve(direct)
	if err != nil {
		t.Fatalf("ParseModelResolve(direct) unexpected error: %v", err)
	}
	if parsed.Revision != sha || parsed.Bytes == nil || *parsed.Bytes != 639446688 || parsed.AlreadyPinned {
		t.Fatalf("unexpected resolved payload: %+v", parsed)
	}

	msSHA := "6abe20cd0aed577f4d0b267935868ecae190aee9"
	wrapped := []byte(`{
		"command": "model resolve",
		"status": "resolved",
		"completeness": "complete",
		"run_id": null,
		"evidence_dir": null,
		"limitations": [],
		"details": {
			"definition": "model-resolve.v1",
			"platform": "modelscope",
			"repository": "Qwen/Qwen3-0.6B-GGUF",
			"revision": "` + msSHA + `",
			"path": "Qwen3-0.6B-Q8_0.gguf",
			"pinned_url": "https://modelscope.cn/Qwen/Qwen3-0.6B-GGUF/blob/` + msSHA + `/Qwen3-0.6B-Q8_0.gguf",
			"download_url": "https://modelscope.cn/api/v1/models/Qwen/Qwen3-0.6B-GGUF/repo?Revision=` + msSHA + `&FilePath=Qwen3-0.6B-Q8_0.gguf",
			"filename": "Qwen3-0.6B-Q8_0.gguf",
			"bytes": null,
			"already_pinned": true,
			"model_requests_sent": 0
		}
	}`)
	wrappedParsed, err := ParseModelResolve(wrapped)
	if err != nil {
		t.Fatalf("ParseModelResolve(wrapped) unexpected error: %v", err)
	}
	if wrappedParsed.Platform != "modelscope" || wrappedParsed.Bytes != nil || !wrappedParsed.AlreadyPinned {
		t.Fatalf("unexpected wrapped resolved payload: %+v", wrappedParsed)
	}

	dirPayload := []byte(`{
		"definition": "model-resolve.v1",
		"kind": "directory",
		"platform": "huggingface",
		"repository": "mlx-community/Qwen3-0.6B-4bit",
		"revision": "73e3e38d981303bc594367cd910ea6eb48349da8",
		"path": "",
		"pinned_url": "https://huggingface.co/mlx-community/Qwen3-0.6B-4bit/tree/73e3e38d981303bc594367cd910ea6eb48349da8",
		"download_url": "https://huggingface.co/mlx-community/Qwen3-0.6B-4bit/tree/73e3e38d981303bc594367cd910ea6eb48349da8",
		"filename": "Qwen3-0.6B-4bit",
		"bytes": 335450584,
		"already_pinned": true,
		"model_requests_sent": 0
	}`)
	dirParsed, err := ParseModelResolve(dirPayload)
	if err != nil {
		t.Fatalf("ParseModelResolve(directory) unexpected error: %v", err)
	}
	if dirParsed.Kind != "directory" || dirParsed.Filename != "Qwen3-0.6B-4bit" {
		t.Fatalf("unexpected directory resolve payload: %+v", dirParsed)
	}
}

func TestParseModelResolveRejectsInvalidInputs(t *testing.T) {
	sha := "23749fefcc72300e3a2ad315e1317431b06b590a"
	base := func(override string) string {
		return `{"definition":"model-resolve.v1","platform":"huggingface","repository":"Qwen/Qwen3-0.6B-GGUF","revision":"` + sha + `","path":"a.gguf","pinned_url":"https://huggingface.co/Qwen/Qwen3-0.6B-GGUF/blob/` + sha + `/a.gguf","download_url":"https://huggingface.co/Qwen/Qwen3-0.6B-GGUF/resolve/` + sha + `/a.gguf","filename":"a.gguf","bytes":1024,"already_pinned":false,"model_requests_sent":0` + override + `}`
	}
	cases := map[string]string{
		"empty":            "",
		"duplicate_keys":   `{"definition":"model-resolve.v1","definition":"model-resolve.v1"}`,
		"extra_field":      base(`,"extra":1`),
		"wrong_definition": `{"definition":"model-resolve.v2","platform":"huggingface","repository":"Qwen/Qwen3-0.6B-GGUF","revision":"` + sha + `","path":"a.gguf","pinned_url":"https://huggingface.co/Qwen/Qwen3-0.6B-GGUF/blob/` + sha + `/a.gguf","download_url":"https://huggingface.co/Qwen/Qwen3-0.6B-GGUF/resolve/` + sha + `/a.gguf","filename":"a.gguf","bytes":null,"already_pinned":false,"model_requests_sent":0}`,
		"nonzero_requests": `{"definition":"model-resolve.v1","platform":"huggingface","repository":"Qwen/Qwen3-0.6B-GGUF","revision":"` + sha + `","path":"a.gguf","pinned_url":"https://huggingface.co/Qwen/Qwen3-0.6B-GGUF/blob/` + sha + `/a.gguf","download_url":"https://huggingface.co/Qwen/Qwen3-0.6B-GGUF/resolve/` + sha + `/a.gguf","filename":"a.gguf","bytes":null,"already_pinned":false,"model_requests_sent":1}`,
		"null_pinned_flag": `{"definition":"model-resolve.v1","platform":"huggingface","repository":"Qwen/Qwen3-0.6B-GGUF","revision":"` + sha + `","path":"a.gguf","pinned_url":"https://huggingface.co/Qwen/Qwen3-0.6B-GGUF/blob/` + sha + `/a.gguf","download_url":"https://huggingface.co/Qwen/Qwen3-0.6B-GGUF/resolve/` + sha + `/a.gguf","filename":"a.gguf","bytes":null,"already_pinned":null,"model_requests_sent":0}`,
		"bad_revision":     `{"definition":"model-resolve.v1","platform":"huggingface","repository":"Qwen/Qwen3-0.6B-GGUF","revision":"main","path":"a.gguf","pinned_url":"https://huggingface.co/Qwen/Qwen3-0.6B-GGUF/blob/main/a.gguf","download_url":"https://huggingface.co/Qwen/Qwen3-0.6B-GGUF/resolve/main/a.gguf","filename":"a.gguf","bytes":null,"already_pinned":false,"model_requests_sent":0}`,
		"zero_bytes":       `{"definition":"model-resolve.v1","platform":"huggingface","repository":"Qwen/Qwen3-0.6B-GGUF","revision":"` + sha + `","path":"a.gguf","pinned_url":"https://huggingface.co/Qwen/Qwen3-0.6B-GGUF/blob/` + sha + `/a.gguf","download_url":"https://huggingface.co/Qwen/Qwen3-0.6B-GGUF/resolve/` + sha + `/a.gguf","filename":"a.gguf","bytes":0,"already_pinned":false,"model_requests_sent":0}`,
		"bad_platform":     `{"definition":"model-resolve.v1","platform":"github","repository":"Qwen/Qwen3-0.6B-GGUF","revision":"` + sha + `","path":"a.gguf","pinned_url":"https://huggingface.co/Qwen/Qwen3-0.6B-GGUF/blob/` + sha + `/a.gguf","download_url":"https://huggingface.co/Qwen/Qwen3-0.6B-GGUF/resolve/` + sha + `/a.gguf","filename":"a.gguf","bytes":null,"already_pinned":false,"model_requests_sent":0}`,
		"wrong_command":    `{"command":"model acquire","status":"resolved","completeness":"complete","run_id":null,"evidence_dir":null,"limitations":[],"details":` + base("") + `}`,
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseModelResolve([]byte(payload)); err == nil {
				t.Fatalf("%s: expected error, got nil", name)
			}
		})
	}
}
