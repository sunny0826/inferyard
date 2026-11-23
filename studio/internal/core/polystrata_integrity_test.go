package core

import (
	"slices"
	"testing"
)

func TestPolystrataPrepareArgsPassesPinnedSHA256ForDefaultSourceOnly(t *testing.T) {
	defaultArgs := polystrataPrepareArgs("/tmp/out", "/tmp/data", &PolystrataSetupRequest{
		Family:      "qwen36",
		Model:       "UD-Q4_K_M",
		ModelSource: "modelscope",
	}, false, "http://127.0.0.1:7890")
	srcIdx := slices.Index(defaultArgs, "--source")
	shaIdx := slices.Index(defaultArgs, "--expected-sha256")
	if srcIdx < 0 || srcIdx+1 >= len(defaultArgs) || defaultArgs[srcIdx+1] != polystrataDefaultSourceURL {
		t.Fatalf("expected pinned --source %q in args=%v", polystrataDefaultSourceURL, defaultArgs)
	}
	if shaIdx < 0 || shaIdx+1 >= len(defaultArgs) || defaultArgs[shaIdx+1] != polystrataDefaultSourceSHA256 {
		t.Fatalf("expected pinned --expected-sha256 %q in args=%v", polystrataDefaultSourceSHA256, defaultArgs)
	}

	explicitDefaultArgs := polystrataPrepareArgs("/tmp/out", "/tmp/data", &PolystrataSetupRequest{
		Source: polystrataDefaultSourceURL,
	}, true, "")
	if idx := slices.Index(explicitDefaultArgs, "--expected-sha256"); idx < 0 || explicitDefaultArgs[idx+1] != polystrataDefaultSourceSHA256 {
		t.Fatalf("expected pinned --expected-sha256 when explicit default URL supplied: %v", explicitDefaultArgs)
	}

	for _, customSrc := range []string{"/tmp/custom-polystrata.tar.gz", "https://example.test/custom.tar.gz"} {
		customArgs := polystrataPrepareArgs("/tmp/out", "/tmp/data", &PolystrataSetupRequest{
			Source: customSrc,
		}, false, "")
		idx := slices.Index(customArgs, "--source")
		if idx < 0 || idx+1 >= len(customArgs) || customArgs[idx+1] != customSrc {
			t.Fatalf("expected custom --source %q in args=%v", customSrc, customArgs)
		}
		if slices.Contains(customArgs, "--expected-sha256") {
			t.Fatalf("unexpected --expected-sha256 for custom source %q: %v", customSrc, customArgs)
		}
	}
}
