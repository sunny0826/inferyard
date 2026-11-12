package inferyard

import "slices"

// The context-speed profile is frozen by the CLI
// (cli/src/inferyard/contracts/context_speed_profile.py) and exported in
// cli/schemas/context_speed_result.schema.json. Studio keeps one definition of
// each value here; contracts_test.go fails when the two sides drift.
const (
	ContextSpeed64K  = 65536
	ContextSpeed128K = 131072
	ContextSpeed256K = 262144

	ContextSpeedBundleID      = "zh-context-speed"
	ContextSpeedBundleVersion = "2026.10.1"
	ContextSpeedAdapter       = "prism_llama_server_v1"
	ContextSpeedSamples       = 5
)

// Default bundles form the default benchmark scope in the CLI workspace. A
// single-bundle run requires an explicit choice, so Studio selects both here.
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

// ContextSpeedTokens lists the supported context sizes in display order.
func ContextSpeedTokens() []int {
	return []int{ContextSpeed64K, ContextSpeed128K, ContextSpeed256K}
}

// ContextSpeedCaseID returns the frozen CLI case id for a context size.
func ContextSpeedCaseID(context int) (string, bool) {
	id, ok := contextSpeedCases[context]
	return id, ok
}

// ValidContextSpeedContext reports whether the CLI accepts this context size.
func ValidContextSpeedContext(context int) bool {
	return slices.Contains(ContextSpeedTokens(), context)
}
