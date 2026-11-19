package pages

// Display terms for internal concepts. Internal identifiers, evidence fields and
// CLI contracts keep their technical names; only user-facing pages use these
// words. Each entry states what stays available in the advanced details.
const (
	termBenchmarkStage  = "配置测评"
	termStartBenchmark  = "开始测评"
	termBenchmarkNoun   = "测评"
	termCandidate       = "测评配置"
	termCandidateEngine = "引擎"
	termBundle          = "测评题集"
	termFrozen          = "固定"
	termSealed          = "证据收尾完成（未核验）"
	termRunColumn       = "运行"
	termReportColumn    = "报告"
	termTimeColumn      = "时间"
	termNoScore         = "未记录评分"
)

// bundleScopeText names the counted case set without exposing "frozen" or
// "denominator" as primary wording.
func bundleScopeText(resume bool) string {
	if resume {
		return "续测题目范围已在开始时固定。"
	}
	return "测评题目范围已在开始时固定。"
}
