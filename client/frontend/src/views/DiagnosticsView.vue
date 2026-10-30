<script setup lang="ts">
import { computed, ref } from "vue";
import { appService } from "../services/bridge.js";
import type { DiagnosticsReport } from "../types/app.js";
import { restartAgent, toast } from "../store.js";
import { fmtBytes, fmtTime } from "../services/format.js";

const report = ref<DiagnosticsReport | null>(null);
const loading = ref(false);
const exporting = ref(false);

const collector = computed(() => report.value?.collector ?? null);

async function load() {
  loading.value = true;
  try {
    report.value = await appService.diagnostics();
  } catch (error) {
    toast(`读取诊断失败：${String(error)}`);
  } finally {
    loading.value = false;
  }
}

async function exportLogs() {
  exporting.value = true;
  try {
    const result = await appService.exportLogs();
    if (result.canceled) return;
    toast(`已导出 ${fmtBytes(result.bytes)}：${result.path}`);
  } catch (error) {
    toast(`导出失败：${String(error)}`);
  } finally {
    exporting.value = false;
  }
}

function statePill(state?: string): { text: string; cls: string } {
  switch (state) {
    case "healthy": return { text: "正常", cls: "" };
    case "degraded": case "starting": return { text: "需关注", cls: "amber" };
    case "error": return { text: "异常", cls: "amber" };
    case "permission_denied": return { text: "权限不足", cls: "amber" };
    case "unsupported": return { text: "不支持", cls: "gray" };
    case "disabled": return { text: "未启用", cls: "gray" };
    default: return { text: state || "—", cls: "gray" };
  }
}
</script>

<template>
  <div>
    <div class="heading">
      <div>
        <div class="eyebrow">SUPPORT</div>
        <h1>诊断</h1>
        <p>查看脱敏状态、排查问题并重启服务。</p>
      </div>
      <button class="btn primary" :disabled="loading" @click="load">{{ loading ? "读取中…" : "读取诊断报告" }}</button>
    </div>
    <div class="grid">
      <section class="card span7">
        <div class="card-title"><h2>系统状态</h2><span v-if="report" class="pill">生成于 {{ fmtTime(report.generated_at) }}</span></div>
        <div v-if="!report" class="muted">点击「读取诊断报告」查看最新脱敏状态；内容不含令牌、凭据或文件路径。</div>
        <template v-else>
          <div class="row"><div><strong>Agent 进程</strong><small>PID {{ report.pid }}</small></div><div class="row-right"><span class="pill">正常</span></div></div>
          <template v-if="collector">
            <div class="row"><div><strong>采集配置</strong><small>配置版本 {{ collector.revision }} · Profile 版本 {{ collector.profile_revision }} · 周期 {{ collector.interval_seconds }} 秒</small></div><div class="row-right"><span class="pill" :class="statePill(collector.state).cls">{{ statePill(collector.state).text }}</span></div></div>
            <div class="row"><div><strong>上传队列</strong><small>待上传 {{ collector.queue_depth }} · 已丢弃 {{ collector.dropped_metrics }}</small></div><div class="row-right"><span class="pill" :class="collector.upload_error ? 'amber' : ''">{{ collector.upload_error || "正常" }}</span></div></div>
            <div class="row"><div><strong>最近采样 / 上传</strong><small>Agent 内部时间</small></div><div class="row-right"><span class="value muted">{{ fmtTime(collector.last_received_at) }} / {{ fmtTime(collector.last_upload_at) }}</span></div></div>
          </template>
          <div class="row"><div><strong>ESP32 设备</strong><small>{{ report.monitor ? `共 ${report.monitor.device_count} 台 · 已配对 ${report.monitor.paired_count} · 在线 ${report.monitor.online_count}` : "未启用" }}</small></div><div class="row-right"><span class="pill" :class="report.monitor?.online_count ? '' : 'gray'">{{ report.monitor?.online_count ? "有设备在线" : "无在线设备" }}</span></div></div>
        </template>
      </section>
      <section class="card span5">
        <div class="card-title"><h2>提供者健康</h2></div>
        <div v-if="!report" class="muted">读取报告后显示。</div>
        <template v-else>
          <div class="row"><div><strong>GitHub</strong><small>{{ report.companion.github.error_code || "贡献墙数据源" }}</small></div><div class="row-right"><span class="pill" :class="statePill(report.companion.github.state).cls">{{ statePill(report.companion.github.state).text }}</span></div></div>
          <div class="row"><div><strong>Codex</strong><small>{{ report.companion.codex.error_code || "额度与任务" }}</small></div><div class="row-right"><span class="pill" :class="statePill(report.companion.codex.state).cls">{{ statePill(report.companion.codex.state).text }}</span></div></div>
          <div class="row"><div><strong>系统播放</strong><small>{{ report.companion.music.error_code || "系统播放适配器" }}</small></div><div class="row-right"><span class="pill" :class="statePill(report.companion.music.state).cls">{{ statePill(report.companion.music.state).text }}</span></div></div>
        </template>
      </section>
      <section class="card span12">
        <div class="card-title"><h2>排障操作</h2></div>
        <div class="row"><div><strong>重启 Agent</strong><small>重新加载采集器与能力检测；配置保持不变</small></div><div class="row-right"><button class="btn" @click="restartAgent">重启</button></div></div>
        <div class="row"><div><strong>导出日志</strong><small>遇到问题时，把最近的运行记录打包发给开发者协助排查</small></div><div class="row-right"><button class="btn" :disabled="exporting" @click="exportLogs">{{ exporting ? "导出中…" : "导出" }}</button></div></div>
      </section>
    </div>
  </div>
</template>
