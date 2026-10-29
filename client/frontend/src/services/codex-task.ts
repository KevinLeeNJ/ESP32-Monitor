import type { CodexTasks } from "../types/companion.js";

// The adapter observes the latest local desktop chat, not all Codex tasks.
export function taskStatusText(tasks?: CodexTasks): string {
  if (!tasks || tasks.health.state === "disabled") return "任务监控未启用";
  if (tasks.state === "waiting_approval") return "等待批准";
  if (tasks.state === "waiting_input") return "等待输入";
  if (tasks.active_count == null || tasks.state === "unknown") return "任务状态不可用";
  if (tasks.state === "running") return "运行中";
  if (tasks.state === "completed") return "最近任务已完成";
  if (tasks.state === "failed") return "最近任务失败";
  if (tasks.state === "cancelled") return "最近任务已取消";
  return "当前任务未运行";
}
