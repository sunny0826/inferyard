package inferyard

import "slices"

const (
	ContextSpeed64K  = 65536
	ContextSpeed128K = 131072
	ContextSpeed256K = 262144

	ContextSpeedBundleID      = "zh-context-speed"
	ContextSpeedBundleVersion = "2026.10.1"
	ContextSpeedAdapter       = "prism_llama_server_v1"
	ContextSpeedSamples       = 5
	LocalEngineAdapter        = "local_openai_v1"
	PolystrataFamily          = "polystrata"
)

const (
	DefaultBundleCore    = "zh-core"
	DefaultBundlePelican = "zh-svg-pelican"
)

var DefaultBundles = []string{DefaultBundleCore, DefaultBundlePelican}

var contextSpeedCases = map[int]string{
	ContextSpeed64K:  "context-speed-64k",
	ContextSpeed128K: "context-speed-128k",
	ContextSpeed256K: "context-speed-256k",
}

func ContextSpeedTokens() []int {
	return []int{ContextSpeed64K, ContextSpeed128K, ContextSpeed256K}
}

func ContextSpeedCaseID(context int) (string, bool) {
	id, ok := contextSpeedCases[context]
	return id, ok
}

func ValidContextSpeedContext(context int) bool {
	return slices.Contains(ContextSpeedTokens(), context)
}
