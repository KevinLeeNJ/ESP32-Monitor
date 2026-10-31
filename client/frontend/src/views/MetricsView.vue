<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { collector, store, toast, useCollectorRevision } from "../store.js";
import {
  deviceDependencies, ensureUploadConfig, metricGroup, metricGroups, metricNames, orderedRows,
  pruneDeviceDependents, rowCells, rowValue, selectedObjects, objectLabel,
} from "../services/collector-metrics.js";
import { isV2Server } from "../services/server-url.js";
import type { CollectorV2Capabilities, CollectorV2Metric } from "../types/collector.js";
import type { MetricRow } from "../services/collector-metrics.js";
import PixelSelect from "../components/PixelSelect.vue";

const activeTab = ref("CPU");
const intervalOptions = [5, 10, 30, 60].map(seconds => ({ value: seconds, label: `${seconds} 秒` }));
const busy = computed(() => collector.busy);
// CollectorController is a plain class: subscribe so draft edits and the
// snapshot poll re-render this view (object gating, checked states, values).
const revision = useCollectorRevision();

const caps = computed(() => { revision.value; return collector.capabilities?.v2 ?? null; });
const hardwareMonitor = computed(() => { revision.value; return collector.capabilities?.hardware_monitor; });
const v2Configured = computed(() => isV2Server(store.settings?.collector.upload_url ?? ""));
const stateNames: Record<string, string> = { healthy: "正常", degraded: "需关注", error: "异常", disabled: "未启用", starting: "启动中" };
const collectorState = computed(() => { revision.value; return stateNames[collector.snapshot?.state ?? ""] ?? collector.snapshot?.state ?? "加载中"; });
const draftInterval = computed({
  get: () => collector.draft?.interval_seconds ?? 10,
  set: (value: number) => {
    const draft = collector.draft;
    if (!draft) return;
    draft.interval_seconds = value;
    collector.notifyDraftChanged();
  },
});
const uploadEnabled = computed({
  get: () => { revision.value; return collector.draft?.upload?.enabled ?? false; },
  set: (value: boolean) => {
    const draft = collector.draft;
    if (!draft) return;
    ensureUploadConfig(draft).enabled = value;
    collector.notifyDraftChanged();
  },
});
const allRows = computed<MetricRow[]>(() => (caps.value ? orderedRows(caps.value as CollectorV2Capabilities) : []));
const tabs = computed(() => metricGroups.filter(group => allRows.value.some(row => metricGroup(row.metric) === group)));
watch(tabs, value => { if (!value.includes(activeTab.value)) activeTab.value = value[0] ?? "CPU"; }, { immediate: true });
const rows = computed<MetricRow[]>(() => allRows.value.filter(row => metricGroup(row.metric) === activeTab.value));

const rowTitle = (row: MetricRow) => row.bundle?.title ?? metricNames[row.metric] ?? row.metric;
const isObjectRow = (row: MetricRow) => !!row.objects?.length;
function memberMetric(key: string): CollectorV2Metric | undefined {
  return caps.value?.metrics.find(metric => metric.metric === key);
}
function objectName(id: number): string {
  return caps.value ? objectLabel(caps.value, id) : `对象 ${id}`;
}
function isSelected(row: MetricRow): boolean {
  const upload = collector.draft?.upload;
  if (!upload) return false;
  const members = row.bundle?.members ?? [row.metric];
  return upload.optional.some(selection => members.includes(selection.metric));
}
function toggleRow(row: MetricRow, checked: boolean) {
  const upload = collector.draft?.upload;
  if (!upload) return;
  const members = row.bundle?.members ?? [row.metric];
  if (checked) {
    for (const member of members) {
      let selection = upload.optional.find(item => item.metric === member);
      if (!selection) { selection = { metric: member }; upload.optional.push(selection); }
      const metric = memberMetric(member);
      if (metric?.objects) selection.objects = [...row.objects ?? metric.objects];
    }
  } else {
    upload.optional = upload.optional.filter(selection => !members.includes(selection.metric));
  }
  collector.notifyDraftChanged();
}
function toggleObject(row: MetricRow, id: number, checked: boolean) {
  const upload = collector.draft?.upload;
  if (!upload) return;
  const keys = row.bundle?.members ?? [row.metric];
  if (checked) {
    for (const key of keys) {
      if (memberMetric(key)?.objects) {
        let selection = upload.optional.find(item => item.metric === key);
        if (!selection) { selection = { metric: key, objects: [] }; upload.optional.push(selection); }
        selection.objects ??= [];
        if (!selection.objects.includes(id)) selection.objects.push(id);
      } else if (!upload.optional.some(item => item.metric === key)) {
        upload.optional.push({ metric: key });
      }
    }
  } else {
    for (const key of keys) {
      const selection = upload.optional.find(item => item.metric === key);
      if (!selection) continue;
      if (selection.objects) {
        selection.objects = selection.objects.filter(object => object !== id);
        if (!selection.objects.length) upload.optional = upload.optional.filter(item => item !== selection);
      } else if (key === row.metric) {
        upload.optional = upload.optional.filter(item => item !== selection);
      }
    }
    const objectKeys = keys.filter(key => memberMetric(key)?.objects);
    if (!objectKeys.some(key => upload.optional.find(item => item.metric === key)?.objects?.length)) {
      upload.optional = upload.optional.filter(item => !keys.includes(item.metric) || !!memberMetric(item.metric)?.objects);
    }
    pruneDeviceDependents(upload, row.metric, id);
  }
  collector.notifyDraftChanged();
}
function objectChecked(row: MetricRow, id: number): boolean {
  const upload = collector.draft?.upload;
  if (!upload) return false;
  const keys = (row.bundle?.members ?? [row.metric]).filter(key => memberMetric(key)?.objects);
  return keys.length > 0 && keys.every(key => upload.optional.find(item => item.metric === key)?.objects?.includes(id));
}
function objectDisabled(row: MetricRow, id: number): boolean {
  const parent = deviceDependencies[row.metric];
  return !!parent && !!collector.draft?.upload && !selectedObjects(collector.draft.upload, parent).includes(id);
}
// 每一格承载一个数值；组合指标按成员并排，由 CSS 决定每行几列。
function cells(row: MetricRow, id?: number) {
  return caps.value ? rowCells(row, caps.value, collector.snapshot, id) : [];
}

async function rescan() {
  await collector.rescan();
  toast(collector.error ? `检测失败：${collector.error}` : "已更新本机指标能力");
}

async function apply() {
  if (!collector.draft) return;
  const ok = await collector.apply();
  toast(ok ? "采集配置已保存" : `保存失败：${collector.error}`);
}

onMounted(async () => {
  if (!collector.capabilities) await collector.rescan();
});
</script>

<template>
  <div>
    <div class="heading">
      <div>
        <div class="eyebrow">COLLECTION</div>
        <h1>指标采集</h1>
        <p>本机指标实时预览；配置 Server 后可选择需要上报的项目。</p>
      </div>
      <button class="btn primary" :disabled="busy" @click="apply">保存</button>
    </div>
    <div class="banner">
      <div><b>仅显示当前可获取的指标。</b>可随时重新检测刷新。</div>
      <button class="btn" :disabled="busy" @click="rescan">重新检测</button>
    </div>
    <div v-if="!v2Configured" class="banner warning" data-testid="metrics-upload-needs-server">
      <div><b>远端上报尚未配置。</b>当前指标仅在本机预览；配置并注册 Server 后即可开启上传。</div>
      <button class="btn" @click="store.page = 'server'">前往 Server 连接</button>
    </div>
    <div v-if="hardwareMonitor" class="banner warning" data-testid="lhm-runtime-required">
      <div><b>Windows 硬件传感器需要处理。</b>{{ hardwareMonitor.message || "请检查 .NET Framework 和后台 Agent 的启动权限后重新检测。" }}</div>
      <button class="btn" :disabled="busy" @click="rescan">重新检测</button>
    </div>
    <div class="grid">
      <section class="card span12" :data-revision="revision">
        <div class="card-title"><h2>采集配置</h2>
          <span class="pill" :class="collector.snapshot?.state === 'healthy' ? '' : 'gray'">
            {{ collector.error || collector.pollError || collectorState }}
          </span>
        </div>
        <div class="row">
          <div><strong>启用远端指标上报</strong><small>{{ v2Configured ? "开启后必选指标会自动上传" : "请先在「Server 连接」配置并注册 Server" }}</small></div>
          <input class="switch" type="checkbox" aria-label="启用远端指标上报" v-model="uploadEnabled" :disabled="busy || !v2Configured"
            :title="!v2Configured ? '请先配置并注册 Server' : undefined">
        </div>
        <div class="row">
          <div><strong>采样周期</strong><small>采集与上传周期，默认 10 秒</small></div>
          <PixelSelect v-model="draftInterval" :options="intervalOptions" label="采样周期" :disabled="busy" />
        </div>
        <div v-if="!caps" class="muted" data-testid="metrics-caps-empty">
          {{ collector.error ? (collector.error.includes("ipc_unavailable") ? "Agent 正在重启，正在自动重试读取本机指标能力…" : `读取本机指标能力失败：${collector.error}`) : "正在读取本机指标能力…" }}
        </div>
        <div v-if="!caps && collector.error" class="actions">
          <button class="btn" :disabled="busy" @click="rescan">重新检测</button>
        </div>
        <div v-if="tabs.length" class="tabs" role="tablist">
          <button v-for="tab in tabs" :key="tab" role="tab" :aria-selected="tab === activeTab"
            :class="{ active: tab === activeTab }" @click="activeTab = tab">{{ tab }}</button>
        </div>
        <div v-if="rows.length" :key="activeTab" class="metric-content metric-slide-in">
          <div class="table-head"><span>指标 / 对象</span><span>当前值</span><span>上传</span></div>
          <template v-for="row in rows" :key="row.metric">
            <div v-if="isObjectRow(row)" class="metric-row object-row" :data-metric-row="row.metric">
              <div class="metric-title">
                <strong>{{ rowTitle(row) }}</strong>
                <label v-if="row.metric === 'core.usage'" class="row-switch">
                  <input class="switch" type="checkbox" :aria-label="`上传 ${rowTitle(row)}`"
                    :checked="isSelected(row)" :disabled="busy || !v2Configured"
                    @change="toggleRow(row, ($event.target as HTMLInputElement).checked)">
                </label>
              </div>
              <div v-if="row.metric === 'core.usage'" class="value-grid core-grid">
                <span v-for="id in row.objects" :key="id" class="value-cell" :data-object-row="id">
                  <span class="value-cell-name">{{ objectName(id) }}</span>
                  <span class="value-cell-value">{{ rowValue(row, caps!, collector.snapshot, id) }}</span>
                </span>
              </div>
              <div v-else class="metric-objects">
                <div v-for="id in row.objects" :key="id" class="metric-object" :data-object-row="id">
                  <span class="metric-object-name">{{ objectName(id) }}</span>
                  <span class="value-grid">
                    <span v-for="cell in cells(row, id)" :key="cell.key" class="value-cell">
                      <span v-if="cell.label" class="value-cell-name">{{ cell.label }}</span>
                      <span class="value-cell-value">{{ cell.value }}</span>
                    </span>
                  </span>
                  <label>
                    <input class="switch" type="checkbox" :aria-label="`上传 ${rowTitle(row)} ${objectName(id)}`"
                      :checked="objectChecked(row, id)" :disabled="busy || !v2Configured || objectDisabled(row, id)"
                      :title="objectDisabled(row, id) ? '请先启用该设备的主指标' : undefined"
                      @change="toggleObject(row, id, ($event.target as HTMLInputElement).checked)">
                  </label>
                </div>
              </div>
            </div>
            <div v-else class="metric-row" :data-metric-row="row.metric">
              <div class="metric-title"><strong>{{ rowTitle(row) }}</strong></div>
              <div class="value-grid">
                <span v-for="cell in cells(row)" :key="cell.key" class="value-cell">
                  <span v-if="cell.label" class="value-cell-name">{{ cell.label }}</span>
                  <span class="value-cell-value">{{ cell.value }}</span>
                </span>
              </div>
              <div>
                <span v-if="row.required" class="pill required-metric" title="自动采集并上传">必选</span>
                <input v-else class="switch" type="checkbox" :aria-label="`上传 ${rowTitle(row)}`"
                  :checked="isSelected(row)" :disabled="busy || !v2Configured"
                  @change="toggleRow(row, ($event.target as HTMLInputElement).checked)">
              </div>
            </div>
          </template>
          <p class="hint">{{ v2Configured ? "必选指标自动上传；选择 IP 地址会通过外部服务查询公网地址。" : "当前仅预览本机指标；配置 Server 后可选择上传内容。" }}</p>
        </div>
      </section>
    </div>
  </div>
</template>
