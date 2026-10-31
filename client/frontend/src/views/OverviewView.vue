<script setup lang="ts">
import { computed } from "vue";
import { collector, refreshCompanion, refreshDevices, store, toast, useCollectorRevision } from "../store.js";
import { rowCells, displayMetrics, objectLabel } from "../services/collector-metrics.js";
import type { MetricRow } from "../services/collector-metrics.js";
import { serverBase } from "../services/server-url.js";
import { connectionLabel } from "../services/device-labels.js";
import { fmtClockTime } from "../services/format.js";
import type { PageID } from "../store.js";

const revision = useCollectorRevision();

interface OverviewEntry { key: string; title: string; row: MetricRow; objectID?: number }

const caps = computed(() => { revision.value; return collector.capabilities?.v2; });
const rows = computed(() => caps.value ? displayMetrics(caps.value) : []);
// 对象型指标（磁盘、网卡、电池）一个设备一行，和设备页的呈现保持一致。
const overviewRows = computed<OverviewEntry[]>(() => {
  const entries: OverviewEntry[] = [];
  for (const row of rows.value.slice(0, 6)) {
    if (row.objects?.length) {
      for (const id of row.objects) entries.push({ key: `${row.metric}:${id}`, title: objectName(id), row, objectID: id });
    } else {
      entries.push({ key: row.metric, title: row.bundle?.title ?? row.metric, row });
    }
  }
  return entries;
});
const snap = computed(() => { revision.value; return collector.snapshot; });
const uploadPill = computed(() => {
  if (!store.status?.collector_enabled) return { text: "未启用", cls: "gray" };
  if (!snap.value) return { text: "等待采样", cls: "gray" };
  if (snap.value.state === "error") return { text: "采集错误", cls: "amber" };
  if (!snap.value.config.upload?.enabled) return { text: "未启用上传", cls: "gray" };
  if (snap.value.upload_error) return { text: "上传异常", cls: "amber" };
  return { text: snap.value.state === "healthy" ? "上传正常" : "已停止", cls: snap.value.state === "healthy" ? "" : "gray" };
});

function sampleAge(): string {
  const at = snap.value?.last_received_at;
  if (!at) return "—";
  const seconds = Math.max(0, Math.round((Date.now() - new Date(at).getTime()) / 1000));
  return seconds < 5 ? "刚刚" : `${seconds}\u00A0秒前`;
}

function objectName(id: number): string {
  return caps.value ? objectLabel(caps.value, id) : `对象 ${id}`;
}
function cells(row: MetricRow, objectID?: number) {
  return caps.value ? rowCells(row, caps.value, snap.value, objectID) : [];
}

function openPage(page: PageID) { store.page = page; }
async function reloadAll() {
  await Promise.all([refreshCompanion(), refreshDevices()]);
  toast("已刷新设备与数据源状态");
}
</script>

<template>
  <div>
    <div class="heading">
      <div>
        <div class="eyebrow">OVERVIEW</div>
        <h1>总览</h1>
        <p>查看采集和设备状态。</p>
      </div>
    </div>
    <div v-if="!store.status?.collector_enabled" class="banner">
      <div v-if="!store.settings?.collector.upload_url"><b>尚未连接 Server</b>　填写上传地址后即可开始采集。</div>
      <div v-else><b>尚未启用采集</b>　在「启动与后台」启用采集，并在「指标采集」选择要上传的指标。</div>
      <button class="btn" @click="openPage(store.settings?.collector.upload_url ? 'startup' : 'server')">{{ store.settings?.collector.upload_url ? "打开启动设置" : "配置 Server" }}</button>
    </div>
    <div class="grid">
      <section class="card span3">
        <div class="stat-head">采集状态<span class="stat-icon"><i class="pixelart-icons-font-chart-bar-big" aria-hidden="true"></i></span></div>
        <div class="stat-value">{{ store.status?.collector_enabled ? (snap?.state === "healthy" ? "运行中" : snap?.state === "error" ? "异常" : "已停止") : "未启用" }}</div>
        <div class="stat-sub">最近采样 {{ sampleAge() }} · 周期 {{ snap?.config.interval_seconds ?? "—" }}&nbsp;秒</div>
      </section>
      <section class="card span3">
        <div class="stat-head">最近上传<span class="stat-icon"><i class="pixelart-icons-font-upload" aria-hidden="true"></i></span></div>
        <div class="stat-value">{{ fmtClockTime(snap?.last_upload_at) }}</div>
        <div class="stat-sub">
          队列 {{ snap?.queue_depth ?? 0 }} · 丢弃 {{ snap?.dropped_metrics ?? 0 }}
          <template v-if="snap?.upload_error"> · {{ snap.upload_error }}</template>
        </div>
      </section>
      <section class="card span3">
        <div class="stat-head">Server 连接<span class="stat-icon"><i class="pixelart-icons-font-server" aria-hidden="true"></i></span></div>
        <div class="stat-value">{{ store.settings?.collector.upload_url ? "已配置" : "未配置" }}</div>
        <div class="stat-sub">{{ serverBase(store.settings?.collector.upload_url ?? "") || "在「Server 连接」中填写地址" }}</div>
      </section>
      <section class="card span3">
        <div class="stat-head">ESP32 设备<span class="stat-icon"><i class="pixelart-icons-font-cpu" aria-hidden="true"></i></span></div>
        <div class="stat-value">{{ store.devices.filter(device => device.paired).length }} 台</div>
        <div class="stat-sub">{{ store.devices.filter(device => device.confirmed_online).length }} 台在线</div>
      </section>

      <section class="card span8">
        <div class="card-title"><h2>本机指标</h2><button class="btn" @click="openPage('metrics')">管理指标</button></div>
        <div class="muted">最近一次采样 · 周期 {{ snap?.config.interval_seconds ?? "—" }}&nbsp;秒</div>
        <div v-if="!rows.length" class="muted">尚未获得指标能力，启用采集后自动检测。</div>
        <template v-else>
          <div v-for="entry in overviewRows" :key="entry.key" class="row overview-row">
            <div><strong>{{ entry.title }}</strong></div>
            <span class="value-grid">
              <span v-for="cell in cells(entry.row, entry.objectID)" :key="cell.key" class="value-cell">
                <span v-if="cell.label" class="value-cell-name">{{ cell.label }}</span>
                <span class="value-cell-value">{{ cell.value }}</span>
              </span>
            </span>
          </div>
        </template>
      </section>
      <section class="card span4">
        <div class="card-title"><h2>数据活动</h2><span class="pill">{{ uploadPill.text }}</span></div>
        <div class="muted">采样周期 {{ snap?.config.upload?.interval_seconds ?? snap?.config.interval_seconds ?? "—" }}&nbsp;秒</div>
        <div class="row"><div><strong>已选指标</strong><small>随每次采样一起上传</small></div><div class="row-right"><span class="value">{{ snap?.config.upload?.optional.length ?? 0 }} 项</span></div></div>
        <div class="row"><div><strong>上传地址</strong><small>{{ snap?.config.upload?.enabled ? "已启用上传" : "仅本地采集" }}</small></div><div class="row-right"><span class="value muted">{{ serverBase(store.settings?.collector.upload_url ?? "") || "—" }}</span></div></div>
        <div class="row"><div><strong>最近接收</strong><small>Agent 收到一轮采样</small></div><div class="row-right"><span class="value">{{ fmtClockTime(snap?.last_received_at) }}</span></div></div>
      </section>

      <section class="card span7">
        <div class="card-title"><h2>ESP32 设备</h2><button class="btn" @click="openPage('esp32')">设备详情</button></div>
        <div v-if="store.devicesError" class="section-note">设备状态读取失败，以下为上次结果。</div>
        <div v-if="!store.devices.length" class="muted">{{ store.devicesError ? "无法读取设备列表。" : "还没有已配对的设备。" }}</div>
        <div v-for="device in store.devices.slice(0, 3)" :key="device.device_id" class="device">
          <div class="device-icon" aria-hidden="true"><i class="pixelart-icons-font-cpu"></i></div>
          <div class="device-info">
            <h3>{{ device.device_name }}　<span class="pill" :class="device.confirmed_online ? '' : 'gray'">{{ device.confirmed_online ? "在线" : "离线" }}</span></h3>
            <p>{{ device.address || "—" }} · {{ connectionLabel(device.connection_state) }}<br>最近更新 {{ fmtClockTime(device.last_seen) }}</p>
          </div>
        </div>
      </section>
      <section class="card span5">
        <div class="card-title"><h2>上位机数据源</h2><button class="btn" @click="openPage('companion')">管理</button></div>
        <div class="row"><div><strong>GitHub 贡献墙</strong><small>{{ store.companion?.github.data?.login || "未登录" }} · 15 分钟刷新</small></div><div class="row-right"><span class="pill" :class="store.companion?.github.health.state === 'healthy' ? '' : 'gray'">{{ store.companion?.github.health.state === "healthy" ? "已授权" : "未就绪" }}</span></div></div>
        <div class="row"><div><strong>Codex 桌面端</strong><small>额度与所选任务</small></div><div class="row-right"><span class="pill" :class="store.companion?.codex.quota.health.state === 'healthy' ? '' : 'gray'">{{ store.companion?.codex.quota.health.state === "healthy" ? "已连接" : "未连接" }}</span></div></div>
        <div class="row"><div><strong>系统播放</strong><small>当前播放信息</small></div><div class="row-right"><span class="pill" :class="store.companion?.music.current?.state === 'playing' ? '' : 'gray'">{{ store.companion?.music.current?.state === "playing" ? "播放中" : "未播放" }}</span></div></div>
        <div class="actions"><button class="btn" @click="reloadAll">刷新数据源</button></div>
      </section>
    </div>
  </div>
</template>
