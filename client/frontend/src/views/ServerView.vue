<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { appService } from "../services/bridge.js";
import type { ServerTestResult } from "../types/app.js";
import { cloneSettings, collector, saveSettings, store, toast, useCollectorRevision } from "../store.js";
import { serverBase } from "../services/server-url.js";
import { ensureUploadConfig } from "../services/collector-metrics.js";
import { fmtClockTime } from "../services/format.js";
import { errCode } from "../services/wails.js";

const baseURL = ref("");
const code = ref("");
const registering = ref(false);
const reRegistering = ref(false);
const formError = ref("");
const testing = ref(false);
const result = ref<ServerTestResult | null>(null);
// CollectorController is a plain class; subscribe so the snapshot poll updates
// upload errors and last-upload rows on this page.
const revision = useCollectorRevision();

const snap = computed(() => { revision.value; return collector.snapshot; });
const committed = computed(() => store.settings?.collector.upload_url ?? "");
const deviceName = computed(() => store.settings?.collector.device_name ?? "");
const registered = computed(() => Boolean(deviceName.value));
const toggling = ref(false);
const uploadOn = computed(() => Boolean(store.status?.collector_enabled && snap.value?.config.upload?.enabled));

watch(
  () => store.settings,
  settings => {
    if (settings && !baseURL.value) baseURL.value = serverBase(settings.collector.upload_url ?? "");
  },
  { immediate: true },
);

const codeFor = computed(() => code.value.replace(/\D/g, "").slice(0, 6));

function onCodeInput(event: Event) {
  code.value = (event.target as HTMLInputElement).value.replace(/\D/g, "").slice(0, 6);
  formError.value = "";
}

function registrationError(error: unknown): string {
  switch (errCode(error)) {
    case "invalid_code": return "验证码不正确";
    case "code_expired": return "验证码已过期，请在 Server 网页重新生成";
    case "rate_limited": return "尝试过于频繁，请稍后再试";
    case "server_unreachable": return "无法连接 Server，请检查地址和网络";
    case "invalid_server_url": return "Server 地址格式不正确";
    case "v1_server_unsupported": return "Server 仅支持 v2 协议，请升级 Server 后重新填写地址";
    case "invalid_server_response": return "Server 响应无效，请确认 Server 版本";
    case "registration_unsupported": return "Server 不支持验证码注册，请升级 Server";
    case "token_store_failed": return "无法在本机保存令牌";
    case "settings_save_failed": return "保存设置失败";
    case "agent_restart_failed": return "注册信息已保存，但 Agent 重启失败；请在「诊断」页重启 Agent";
    default: return `注册失败：${String(error)}`;
  }
}

async function register() {
  if (!baseURL.value.trim()) { formError.value = "请填写 Server 地址"; return; }
  if (codeFor.value.length !== 6) { formError.value = "请输入 6 位验证码"; return; }
  registering.value = true;
  formError.value = "";
  result.value = null;
  try {
    const registration = await appService.registerServer(baseURL.value.trim(), codeFor.value);
    store.settings = await appService.getSettings();
    baseURL.value = registration.server_url;
    code.value = "";
    reRegistering.value = false;
    await collector.refresh();
    const enabled = await setUpload(true);
    toast(enabled ? `已注册为「${registration.device_name}」，正在上传` : `已注册为「${registration.device_name}」，请手动开启采集与上传`);
  } catch (error) {
    formError.value = registrationError(error);
  } finally {
    registering.value = false;
  }
}

// 连接 Server 后可直接在本页开启采集与上传，无需再跳转其它页面。
async function setUpload(on: boolean): Promise<boolean> {
  if (!store.settings) { toast("Agent 设置尚未加载"); return false; }
  if (!committed.value) { toast("请先注册设备"); return false; }
  toggling.value = true;
  try {
    if (on && !store.settings.collector.enabled) {
      const next = cloneSettings(store.settings);
      next.collector.enabled = true;
      await saveSettings(next);
    }
    const draft = collector.draft;
    if (!draft) { toast("Agent 尚未就绪，请稍后重试"); return false; }
    ensureUploadConfig(draft).enabled = on;
    collector.notifyDraftChanged();
    if (await collector.apply()) return true;
    toast(`操作失败：${collector.error}`);
    return false;
  } catch (error) { toast(`操作失败：${String(error)}`); return false; }
  finally { toggling.value = false; }
}

async function testConnection() {
  if (!baseURL.value.trim()) { toast("请先填写 Server 地址"); return; }
  testing.value = true;
  result.value = null;
  try {
    result.value = await appService.serverTest(baseURL.value.trim());
  } catch (error) {
    result.value = { ok: false, status_code: 0, error: String(error), duration_ms: 0, url: "" };
  } finally {
    testing.value = false;
  }
}
</script>

<template>
  <div>
    <div class="heading">
      <div>
        <div class="eyebrow">CONNECTION</div>
        <h1>Server 连接</h1>
        <p>在 Server 网页添加设备，输入 6 位验证码完成注册；令牌由本机自动保管。</p>
      </div>
    </div>
    <div class="grid">
      <section class="card span7">
        <div class="card-title"><h2>{{ registered && !reRegistering ? "已验证设备" : "注册设备" }}</h2></div>
        <div class="field">
          <label for="server-url">Server 地址</label>
          <div class="pixel-input-frame"><input id="server-url" v-model="baseURL" type="url" placeholder="https://" autocomplete="off" :disabled="registering" @input="formError = ''"></div>
          <small class="muted">建议使用 HTTPS。</small>
        </div>
        <div v-if="!registered || reRegistering" class="field">
          <label for="server-code">设备验证码</label>
          <div class="pixel-input-frame"><input id="server-code" :value="code" type="text" inputmode="numeric" maxlength="6" placeholder="6 位数字" autocomplete="one-time-code" :disabled="registering" @input="onCodeInput" @keyup.enter="register"></div>
          <small class="muted">在 Server 网页点击「添加设备」后获取，10 分钟内有效。</small>
        </div>
        <div v-else class="row">
          <div><strong>设备名称</strong><small>由 Server 分配；更换设备时重新注册即可</small></div>
          <div class="row-right"><span class="value">{{ deviceName }}</span></div>
        </div>
        <div v-if="formError" class="form-error" role="alert">{{ formError }}</div>
        <div class="actions">
          <button v-if="!registered || reRegistering" class="btn primary" :disabled="registering" @click="register">{{ registering ? "注册中…" : "注册设备" }}</button>
          <button v-else class="btn primary" @click="reRegistering = true; formError = ''">重新注册</button>
          <button v-if="reRegistering" class="btn" :disabled="registering" @click="reRegistering = false; code = ''; formError = ''">取消</button>
          <button class="btn" :disabled="testing || registering" @click="testConnection">{{ testing ? "测试中…" : "测试连接" }}</button>
        </div>
        <div v-if="result" class="row" data-testid="server-test-result">
          <div><strong>{{ result.ok ? "连接成功" : "连接失败" }}</strong><small>{{ result.url || "—" }}</small></div>
          <div class="row-right">
            <span class="pill" :class="result.ok ? '' : 'amber'">{{ result.ok ? `HTTP ${result.status_code}` : (result.error || "失败") }}</span>
            <small class="muted">　{{ result.duration_ms }} ms</small>
          </div>
        </div>
      </section>
      <section class="card span5">
        <div class="card-title"><h2>连接状态</h2></div>
        <div class="row"><div><strong>注册状态</strong><small>{{ registered ? "验证码已换取上传令牌" : "尚未注册" }}</small></div><div class="row-right"><span class="pill" :class="registered ? '' : 'amber'">{{ registered ? "已注册" : "未注册" }}</span></div></div>
        <div class="row"><div><strong>已保存地址</strong><small>Agent 上传使用的 Server 地址</small></div><div class="row-right"><span class="value">{{ serverBase(committed) || "未配置" }}</span></div></div>
        <div class="row"><div><strong>采集与上传</strong><small>{{ uploadOn ? "正在按周期采集并上传指标" : "开启后自动启用采集；关闭后仅在本机采集" }}</small></div><div class="row-right"><input class="switch" type="checkbox" :checked="uploadOn" :disabled="toggling || !registered || !committed" aria-label="采集与上传" @change="setUpload(($event.target as HTMLInputElement).checked)"></div></div>
        <div class="row"><div><strong>上传指标</strong><small>必选指标始终上传，可选项在「指标采集」中调整</small></div><div class="row-right"><button class="btn" @click="store.page = 'metrics'">管理指标</button></div></div>
        <div class="row"><div><strong>上传错误</strong><small>Agent 最近一次上传</small></div><div class="row-right"><span class="value muted">{{ snap?.upload_error || "无" }}</span></div></div>
        <div class="row"><div><strong>最近上传</strong><small>最近一次成功上传</small></div><div class="row-right"><span class="value">{{ fmtClockTime(snap?.last_upload_at) }}</span></div></div>
        <div class="section-note">验证码错误或过期时，请回到 Server 网页重新生成。401 / 403 表示令牌已失效，需要重新注册设备。</div>
      </section>
    </div>
  </div>
</template>

<style scoped>
.form-error{margin-top:4px;padding:9px 12px;background:#f7e4e0;border-left:3px solid #a33a2c;color:#8f3327;font-size:12px;line-height:1.55}
</style>
