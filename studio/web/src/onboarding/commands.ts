export function quote(value: string): string { return `'${value.replaceAll("'", "'\\''")}'`; }
export function setArgument(command: string, flag: string, value: string): string {
  const token = `(?:'[^']*'|"(?:\\\\.|[^"\\\\])*"|[^\\s]+)`;
  const pattern = new RegExp(`(^|\\s)${flag}\\s+${token}`);
  const replacement = `${flag} ${quote(value)}`;
  return pattern.test(command) ? command.replace(pattern, (_, prefix: string) => prefix + replacement) : `${command} ${replacement}`;
}
export const actionLabels = {
  device_check: '3 · 检查设备', prepare_candidate: '4 · 创建候选', bind: '6 · 绑定进程',
  run: '7 · 运行题包', resume: '7 · 继续运行', report: '9 · 生成报告并验证', verify: '9 · 验证报告', done: '已完成', none: '已完成',
};
