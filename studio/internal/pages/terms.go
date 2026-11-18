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
		return "本次续测的题以开始运行时固定；未读取时不猜测。"
	}
	return "本次测评的题以开始运行时固定；未读取时不猜测。"
}
