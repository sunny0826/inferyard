/**
 * InferYard Studio API 契约 — TypeScript 类型镜像（docs/api-contract.md 的权威镜像）。
 * 冻结后：前端复制为 web/src/api/types.ts；Go 侧 struct 的 json tag 必须与这里的
 * 字段名逐一一致。全部字段 snake_case；时间为 UTC ISO 8601 字符串；字节为整数 bytes；
 * 缺测/未知一律 null。
 */

// ----- 3. 错误模型 -----------------------------------------------------------

export type ApiErrorCode =
  | "invalid_request" // 400
  | "unauthorized" // 401
  | "not_found" // 404
  | "conflict" // 409，细分见 details.reason
  | "cli_blocked" // 422，inferyard 退出码 2
  | "internal_error" // 500，app 自身故障
  | "cli_error" // 500，inferyard 退出码 4
  | "cli_unavailable"; // 503，二进制未探测到/不可执行

export type ConflictReason =
  | "queue_duplicate" | "invalid_state" | "output_exists"
  | "not_resumable" | "refresh_in_progress" | "settings_incomplete";

export interface ApiError {
  code: ApiErrorCode;
  /** 服务端固定文案，不回显原始异常/stderr */
  message: string;
  /** inferyard limitations 固定原因原样透传；非 CLI 错误为 [] */
  limitations: string[];
  details: Record<string, unknown> | null;
}

export interface ApiErrorBody {
  error: ApiError;
}

// ----- 4.1 状态 ---------------------------------------------------------------

export type InferyardStatus = "ok" | "not_found" | "not_executable" | "version_failed";

export interface InferyardProbe {
  path: string | null;
  version: string | null;
  status: InferyardStatus;
}

export interface StatusResponse {
  status: "ok";
  version: string;
  inferyard: InferyardProbe;
  active_run_tasks: number;
  active_downloads: number;
}

// ----- 4.2 设置 ---------------------------------------------------------------

export interface UserSettings {
  /** null = 自动探测 PATH */
  inferyard_path: string | null;
  results_root: string | null;
  models_root: string | null;
}

export interface EffectiveSettings {
  inferyard_path: string | null;
  inferyard_version: string | null;
  inferyard_status: InferyardStatus;
  results_root: string | null;
  models_root: string | null;
  /** 派生：dirname(results_root)/reports；results_root 未配置时为 null */
  reports_root: string | null;
}

export interface SettingsResponse {
  settings: UserSettings;
  effective: EffectiveSettings;
}

/** PUT /api/settings：三字段均可选，仅更新出现字段；路径须为非空绝对路径 */
export interface UpdateSettingsRequest {
  inferyard_path?: string | null;
  results_root?: string;
  models_root?: string;
}

// ----- 4.3 设备（device_preflight.v1 透传；CLI 新增字段两端必须容忍） ---------

export interface DeviceGpuDevice {
  index: number;
  name?: string | null;
  memory_total_bytes?: number | null;
  memory_free_bytes?: number | null;
  [key: string]: unknown;
}

export interface DeviceGpu {
  status: "observed" | "unavailable";
  source?: string | null;
  reason?: string | null;
  devices: DeviceGpuDevice[];
  [key: string]: unknown;
}

export interface DeviceHardware {
  platform?: string | null;
  architecture?: string | null;
  cpu_model?: string | null;
  logical_cpus?: number | null;
  memory_total_bytes?: number | null;
  memory_available_bytes?: number | null;
  memory_available_is_estimate?: boolean;
  disk_free_bytes?: number | null;
  disk_path?: string | null;
  gpu?: DeviceGpu | null;
  /** 各字段采集来源 */
  sources?: Record<string, string | null>;
  /** 缺测原因（缺测字段值为 null） */
  missing?: Record<string, string>;
  [key: string]: unknown;
}

export interface DeviceModelAvailable {
  path: string;
  status: "available";
  name: string;
  size_bytes: number;
  architecture: string | null;
  context_length: number | null;
  file_type: number | null;
  chat_template_available: boolean;
}

export interface DeviceModelUnavailable {
  path: string;
  status: "unavailable";
  reason: string;
}

export type DeviceModel = DeviceModelAvailable | DeviceModelUnavailable;

export type DeviceMode = "cpu" | "cuda" | "metal";

export interface DeviceCandidate {
  model_path: string;
  model_name: string;
  mode: DeviceMode | "blocked";
  gpu_index: number | null;
  estimated_gpu_memory_bytes: number | null;
  min_available_memory_bytes: number;
  min_disk_bytes: number;
  context_size: number;
  threads: number;
  reason: string;
  [key: string]: unknown;
}

export interface DeviceRecommendation {
  status: "recommended" | "blocked";
  selected: DeviceCandidate | null;
  reason: string | null;
  candidates: DeviceCandidate[];
  limitations: string[];
}

export interface PlatformCapabilities {
  device_inspection: boolean;
  live_single_run: boolean;
  live_experiments: boolean;
  metal_backend_verified: boolean;
}

export interface DeviceNextStep {
  action: string;
  reason?: string;
  command?: string[];
  [key: string]: unknown;
}

/** inferyard `device-check --json` 的 details 对象，原样透传 */
export interface DevicePreflight {
  kind: "device_preflight.v1";
  hardware: DeviceHardware;
  models: DeviceModel[];
  recommendation: DeviceRecommendation;
  model_requests_sent: number;
  platform_capabilities: PlatformCapabilities;
  ready_to_run: boolean;
  next_steps: DeviceNextStep[];
  [key: string]: unknown;
}

export interface DeviceResponse {
  snapshot: DevicePreflight | null;
  captured_at: string | null;
  refreshing: boolean;
  last_error: ApiError | null;
}

// ----- 4.4 运行历史 -----------------------------------------------------------

/** 呈现语义：cancelled > dirty > partial > complete（派生实现属 evidence 层） */
export type RunHistoryStatus = "complete" | "partial" | "cancelled" | "dirty";
export type RunRelation = "initial" | "repeat" | "resume" | "rerun" | "derived";

export interface RunScore {
  passed: number;
  valid: number;
  /** valid 为 0 时 null，不显示 0% */
  rate: number | null;
}

export interface RunSummary {
  run_id: string;
  path: string;
  batch_id: string | null;
  status: RunHistoryStatus;
  sealed: boolean;
  completeness: "complete" | "incomplete" | null;
  stop_reason: string | null;
  model_name: string | null;
  model_path: string | null;
  engine: string | null;
  bundle: string | null;
  /** 事件聚合得分；证据不足时 null（不读可缺失的 summary.json） */
  score: RunScore | null;
  relation: RunRelation | null;
  parent_run_id: string | null;
  started_at: string | null;
  finished_at: string | null;
  resumable: boolean;
}

export interface BatchSummary {
  batch_id: string;
  path: string;
  runs: RunSummary[];
}

export interface RunHistoryResponse {
  results_root: string | null;
  scanned_at: string;
  batches: BatchSummary[];
  standalone_runs: RunSummary[];
}

// ----- 4.5 运行任务 -----------------------------------------------------------

export type RunTaskState = "starting" | "running" | "cancelling" | "done" | "cancelled" | "failed";

/** 对齐证据事件 phase 枚举 */
export type RunPhase = "probe" | "warmup" | "baseline" | "formal" | "scoring" | "residual" | "finalizing";

export interface RunExit {
  exit_code: number;
  cli_status: string | null;
  /** 退出 3 时为 "incomplete"（partial 如实呈现，不算 failed） */
  completeness: string | null;
  stop_reason: string | null;
  limitations: string[];
}

export interface RunTask {
  task_id: string;
  kind: "run" | "resume";
  state: RunTaskState;
  /** state=starting 的细分：等待执行槽 */
  queued: boolean;
  config_path: string | null;
  resume_from: string | null;
  bundle: string | null;
  /** tailer 侦测到新 run 目录后填入 */
  run_id: string | null;
  run_path: string | null;
  created_at: string;
  started_at: string | null;
  finished_at: string | null;
  exit: RunExit | null;
  error: ApiError | null;
}

export type CaseState = "pending" | "running" | "completed" | "failed" | "cancelled" | "invalid";
export type QualityState = "pass" | "fail" | "unscorable" | "not_applicable";
export type ScoreCategory =
  | "instruction"
  | "extraction"
  | "qa"
  | "math"
  | "classification"
  | "structured"
  | "performance"
  | "svg";

export interface CaseProgress {
  case_id: string;
  plan_index: number | null;
  state: CaseState;
  category: ScoreCategory | null;
  quality_state: QualityState | null;
  started_at: string | null;
  finished_at: string | null;
  duration_ms: number | null;
  error_category: string | null;
}

export interface RunProgress {
  phase: RunPhase | null;
  done: number;
  /** 取自 inputs.json 冻结分母；未读到时 null（不猜） */
  total: number | null;
  current_case_id: string | null;
  /** 已消费 events.jsonl 的最大 seq */
  last_seq: number;
  cases: CaseProgress[];
}

/** memory.jsonl 聚合点；逐项可 null（缺测），CPU 由 ticks 差分计算 */
export interface ResourceSample {
  utc: string;
  mem_available_bytes: number | null;
  service_rss_bytes: number | null;
  service_cpu_percent: number | null;
  temperature_celsius: number | null;
}

export interface StartRunsRequest {
  /** 1–2 个已绑定 config（bind 产物目录或其 config.toml）绝对路径 */
  config_paths: string[];
  /** 单 config 时必须显式 true（默认双 bundle） */
  allow_single_bundle?: boolean;
}

export interface StartRunsResponse {
  tasks: RunTask[];
}

export interface ResumeRunRequest {
  /** 绝对路径或相对 results_root；必须解析在结果根之内 */
  from_run: string;
  /** 与 server_pid 必须同时提供或同时省略 */
  endpoint_url?: string;
  server_pid?: number;
}

export interface ResumeRunResponse {
  task: RunTask;
}

export interface RunTaskListResponse {
  tasks: RunTask[];
}

export interface RunTaskDetail {
  task: RunTask;
  progress: RunProgress | null;
  /** 内存环形缓冲最近采样点（断线重建用） */
  resources: ResourceSample[];
}

export interface CancelTaskResponse {
  task: RunTask;
}

// ----- 4.6 报告 ---------------------------------------------------------------

export interface ReportSummary {
  /** 报告目录名 */
  report_id: string;
  path: string;
  created_at: string | null;
  run_ids: string[];
  run_paths: string[];
  /** 相对路径，如 /reports/<report_id>/report.html；前端拼接 ?token= 顶层打开，不 iframe */
  html_url: string;
}

export interface ReportListResponse {
  reports_root: string | null;
  reports: ReportSummary[];
}

export interface CreateReportRequest {
  /** 1..N 个 run 绝对路径，须在 results_root 之内 */
  run_paths: string[];
}

export interface CreateReportResponse {
  report: ReportSummary;
}

// ----- 4.7 下载 ---------------------------------------------------------------

export type DownloadState = "downloading" | "done" | "failed" | "cancelled";

export interface DownloadResult {
  /** 实测值（source.json / 最终 JSON） */
  bytes: number;
  sha256: string;
}

export interface DownloadTask {
  task_id: string;
  state: DownloadState;
  source_url: string;
  out_name: string;
  out_dir: string;
  /** .part 基名；侦测前 null */
  file_name: string | null;
  downloaded_bytes: number;
  /** 滑动窗口速率；不可算时 null。无总量，不伪造百分比 */
  bytes_per_second: number | null;
  started_at: string;
  finished_at: string | null;
  result: DownloadResult | null;
  error: ApiError | null;
}

export interface CreateDownloadRequest {
  /** HF/ModelScope blob URL，40 位十六进制 revision，单个 .gguf */
  source_url: string;
  /** 单路径分段 ^[A-Za-z0-9._-]+$，落盘 <models_root>/<out_name> */
  out_name: string;
}

export interface CreateDownloadResponse {
  task: DownloadTask;
}

export interface DownloadListResponse {
  tasks: DownloadTask[];
}

export interface CancelDownloadResponse {
  task: DownloadTask;
}

// ----- 5. SSE 事件（GET /api/events；id 单调递增，支持 Last-Event-ID 重放） ---

export type SseEventType =
  | "run.state" | "run.progress" | "run.resources"
  | "download.state" | "download.progress"
  | "device.updated" | "report.created" | "sync.reset";

/** run.state：任务创建/迁移/终态，全量替换语义 */
export interface RunStateEvent {
  task: RunTask;
}

/** run.progress：进度聚合，全量替换语义（幂等收敛） */
export interface RunProgressEvent {
  task_id: string;
  run_id: string | null;
  progress: RunProgress;
}

/** run.resources：资源曲线，增量追加语义（约 1Hz，可批量） */
export interface RunResourcesEvent {
  task_id: string;
  points: ResourceSample[];
}

/** download.state：任务创建/终态迁移，全量替换语义 */
export interface DownloadStateEvent {
  task: DownloadTask;
}

/** download.progress：.part 轮询（约 1Hz），增量语义 */
export interface DownloadProgressEvent {
  task_id: string;
  downloaded_bytes: number;
  bytes_per_second: number | null;
  utc: string;
}

/** device.updated：刷新完成（成功与失败均发；失败时 snapshot 保留旧值） */
export interface DeviceUpdatedEvent {
  snapshot: DevicePreflight | null;
  captured_at: string | null;
  error: ApiError | null;
}

/** report.created：报告生成成功广播（供非发起客户端刷新列表） */
export interface ReportCreatedEvent {
  report: ReportSummary;
}

/** sync.reset：回放缓冲缺口/服务端重启；客户端须 REST 全量重建 */
export interface SyncResetEvent {
  reason: "buffer_gap";
}

export interface ServerEventMap {
  "run.state": RunStateEvent;
  "run.progress": RunProgressEvent;
  "run.resources": RunResourcesEvent;
  "download.state": DownloadStateEvent;
  "download.progress": DownloadProgressEvent;
  "device.updated": DeviceUpdatedEvent;
  "report.created": ReportCreatedEvent;
  "sync.reset": SyncResetEvent;
}
