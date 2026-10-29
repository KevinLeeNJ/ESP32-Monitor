import test from "node:test";
import assert from "node:assert/strict";
import { taskStatusText } from "../dist/services/codex-task.js";

test("task status distinguishes unavailable, active, waiting and idle", () => {
  const task = (state, active_count, health = "healthy") => ({ state, active_count, health: { state: health } });
  assert.equal(taskStatusText(undefined), "任务监控未启用");
  assert.equal(taskStatusText(task("unknown", null)), "任务状态不可用");
  assert.equal(taskStatusText(task("running", 1)), "运行中");
  assert.equal(taskStatusText(task("waiting_approval", 1)), "等待批准");
  assert.equal(taskStatusText(task("waiting_input", 1)), "等待输入");
  assert.equal(taskStatusText(task("idle", 0)), "当前任务未运行");
});
