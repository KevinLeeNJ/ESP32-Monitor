<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { collector, store, toast } from "../store.js";
import {
  deviceDependencies, metricGroup, metricGroups, metricNames, orderedRows,
  pruneDeviceDependents, rowValue, selectedObjects, validateMetricDraft,
} from "../services/collector-metrics.js";
import type { CollectorV2Capabilities, CollectorV2Metric } from "../types/collector.js";
import type { MetricRow } from "../services/collector-metrics.js";
import PixelSelect from "../components/PixelSelect.vue";

const activeTab = ref("CPU");
const draftInterval = ref(10);
const intervalOptions = [5, 10, 30, 60].map(seconds => ({ value: seconds, label: `${seconds} 秒` }));
const busy = ref(false);
// CollectorController is a plain class: subscribe so draft edits and the 2s
// snapshot poll re-render this view (object gating, checked states, values).
const revision = ref(0);

const caps = computed(() => collector.capabilities?.v2 ?? null);
const v2Configured = computed(() => (store.settings?.collector.upload_url ?? "").endsWith("/v2/collector/samples"));
const stateNames: Record<string, string> = { healthy: "正常", degraded: "需关注", error: "异常", disabled: "未启用", starting: "启动中" };
const collectorState = computed(() => stateNames[collector.snapshot?.state ?? ""] ?? collector.snapshot?.state ?? "加载中");
const uploadEnabled = computed({
  get: () => collector.draft?.upload?.enabled ?? false,
  set: (value: boolean) => {
    const draft = collector.draft;
    if (!draft) return;
    draft.upload ??= { enabled: false, optional: [] };
    draft.upload.enabled = value;
    collector.notifyDraftChanged();
  },
});
const allRows = computed<MetricRow[]>(() => (caps.value ? orderedRows(caps.value as CollectorV2Capabilities) : []));
const tabs = computed(() => metricGroups.filter(group => allRows.value.some(row => metricGroup(row.metric, caps.value as CollectorV2Capabilities) === group)));
watch(tabs, value => { if (!value.includes(activeTab.value)) activeTab.value = value[0] ?? "CPU"; }, { immediate: true });
const rows = computed<MetricRow[]>(() => allRows.value.filter(row => metricGroup(row.metric, caps.value as CollectorV2Capabilities) === activeTab.value));

const rowTitle = (row: MetricRow) => row.bundle?.title ?? metricNames[row.metric] ?? row.metric;
const isObjectRow = (row: MetricRow) => !!row.objects?.length && row.metric !== "battery.level";
function memberMetric(key: string): CollectorV2Metric | undefined {
  return caps.value?.metrics.find(metric => metric.metric === key);
}
function objectName(id: number): string {
  return caps.value?.objects.find(object => object.id === id)?.name ?? `对象 ${id}`;
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
// 电池状态电量与充电状态各占一行。
function batteryValue(): string {
  const batteries = (collector.snapshot?.v2_sample as Record<string, unknown> | undefined)?.bat;
  const list = Array.isArray(batteries) ? batteries.map(item => (item ?? {}) as Record<string, unknown>) : [];
  const level = Number(list[0]?.l);
  if (!list.length || !Number.isFinite(level)) return "—";
  return `${Math.round(level / 100)}%\n${list[0].c ? "充电中" : "未充电"}`;
}

async function rescan() {
  busy.value = true;
  await collector.rescan();
  busy.value = false;
  toast(collector.error ? `检测失败：${collector.error}` : "已更新本机指标能力");
}

async function apply() {
  if (!collector.draft) return;
  collector.draft.interval_seconds = draftInterval.value;
  const error = validateMetricDraft(collector.draft, collector.capabilities);
  if (error) { toast(error); return; }
  busy.value = true;
  await collector.apply();
  busy.value = false;
  toast(collector.error ? `保存失败：${collector.error}` : "采集配置已保存");
  if (!collector.error && collector.snapshot) draftInterval.value = collector.snapshot.config.interval_seconds;
}

onMounted(async () => {
  const unsubscribe = collector.subscribe(() => { revision.value++; });
  onUnmounted(unsubscribe);
  if (!collector.capabilities) await collector.rescan();
  if (collector.snapshot) draftInterval.value = collector.snapshot.config.interval_seconds;
});
</script>

<template>
  <div>
    <div class="heading">
      <div>
        <div class="eyebrow">COLLECTION</div>
        <h1>指标采集</h1>
        <p>总体 CPU、内存占用率和运行时间自动上传，其余按需选择。</p>
      </div>
      <button class="btn primary" :disabled="busy" @click="apply">保存</button>
    </div>
    <div class="banner">
      <div><b>仅显示当前可获取的指标。</b>可随时重新检测刷新。</div>
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
          <div><strong>启用远端指标上报</strong><small>需先在「Server 连接」配置上传地址；必选指标始终上传</small></div>
          <input class="switch" type="checkbox" aria-label="启用远端指标上报" v-model="uploadEnabled" :disabled="busy">
        </div>
        <div class="row">
          <div><strong>采样周期</strong><small>采集与上传周期，默认 10 秒</small></div>
          <PixelSelect v-model="draftInterval" :options="intervalOptions" label="采样周期" :disabled="busy" />
        </div>
        <div v-if="!caps" class="muted" data-testid="metrics-caps-empty">
          {{ v2Configured ? "正在读取本机指标能力…" : "请先在「Server 连接」配置上传地址，然后重新检测可选指标。" }}
        </div>
        <div v-if="!v2Configured" class="actions">
          <button class="btn" @click="store.page = 'server'">前往 Server 连接</button>
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
                    :checked="isSelected(row)" @change="toggleRow(row, ($event.target as HTMLInputElement).checked)">
                </label>
              </div>
              <div class="metric-objects">
                <div v-for="id in row.objects" :key="id" class="metric-object" :data-object-row="id">
                  <span class="metric-object-name">{{ objectName(id) }}</span>
                  <span class="value">{{ rowValue(row, caps!, collector.snapshot, id) }}</span>
                  <label v-if="row.metric !== 'core.usage'">
                    <input class="switch" type="checkbox" :aria-label="`上传 ${rowTitle(row)} ${objectName(id)}`"
                      :checked="objectChecked(row, id)" :disabled="objectDisabled(row, id)"
                      :title="objectDisabled(row, id) ? '请先启用该设备的主指标' : undefined"
                      @change="toggleObject(row, id, ($event.target as HTMLInputElement).checked)">
                  </label>
                  <span v-else class="metric-object-empty" aria-hidden="true"></span>
                </div>
              </div>
            </div>
            <div v-else class="metric-row" :data-metric-row="row.metric">
              <div class="metric-title"><strong>{{ rowTitle(row) }}</strong></div>
              <div class="value">{{ row.metric === "battery.level" ? batteryValue() : rowValue(row, caps!, collector.snapshot) }}</div>
              <div>
                <span v-if="row.required" class="pill required-metric" title="自动采集并上传">必选</span>
                <input v-else class="switch" type="checkbox" :aria-label="`上传 ${rowTitle(row)}`"
                  :checked="isSelected(row)" @change="toggleRow(row, ($event.target as HTMLInputElement).checked)">
              </div>
            </div>
          </template>
          <p class="hint">必选指标自动上传；选择 IP 地址会通过外部服务查询公网地址。</p>
        </div>
      </section>
    </div>
  </div>
</template>
