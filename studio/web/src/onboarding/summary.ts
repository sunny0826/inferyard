import type { WorkspaceProgress } from '../api/onboarding-schema';

type BundleProgress = WorkspaceProgress['bundles'][number];

export const phaseLabels = {
  preflight_missing: '待检查设备',
  candidate_missing: '待准备运行配置',
  bound_missing: '待连接模型服务',
  run_missing: '待开始运行',
  run_incomplete: '运行未完成',
  run_complete: '运行已完成',
} satisfies Record<BundleProgress['phase'], string>;

const nextSteps = {
  device_check: '在向导中检查设备，确认模型与引擎已准备好。',
  prepare_candidate: '在向导中准备下一个题包的运行配置。',
  bind: '在向导中连接已启动的模型服务。',
  run: '在向导中确认设备与预算，然后开始运行题包。',
  report: '在向导中汇总两份题包结果，生成报告。',
  verify: '在向导中验证已生成的报告。',
  done: '可在向导中查看当前工作区的完成情况。',
} satisfies Record<WorkspaceProgress['next_action'], string>;

export function nextStep(progress: WorkspaceProgress): string {
  if (progress.default_scope_complete) return '两份题包均已完成，汇总报告已通过验证。';
  const active = progress.bundles.find(bundle => bundle.phase !== 'run_complete');
  if (active?.next_action === 'resume') return '在向导中查看未完成的运行，确认后继续。';
  return nextSteps[progress.next_action];
}

export function bundleName(bundle: string): string {
  if (bundle === 'zh-core') return '中文核心题包';
  if (bundle === 'zh-svg-pelican') return 'SVG 鹈鹕题包';
  return bundle;
}
