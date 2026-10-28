<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from "vue";
import { monitorService } from "../services/bridge.js";
import type { DiscoveredDevice, PairingStatus } from "../types/monitors.js";
import { cloneSettings, saveSettings, store, toast } from "../store.js";
import { connectionLabel, connectionTypeLabel } from "../services/device-labels.js";

const discovering = ref(false);
const discovery = ref<{ devices: DiscoveredDevice[]; lan_error?: string; serial_error?: string; overflow: boolean } | null>(null);
const serialPorts = ref<DiscoveredDevice[]>([]);
const localPairing = ref<PairingStatus | null>(null);
const pairTarget = ref<DiscoveredDevice | null>(null);
const riskConfirmed = ref(false);

const paired = computed(() => store.devices.filter(device => device.paired));
const companionOn = computed(() => Boolean(store.status?.companion_enabled));
const enabling = ref(false);
async function enableCompanion() {
  if (!store.settings) { toast("Agent 设置尚未加载"); return; }
  enabling.value = true;
  try {
    const next = cloneSettings(store.settings);
    next.companion.enabled = true;
    await saveSettings(next);
    toast("上位机服务已启用");
  } catch (error) { toast(`启用失败：${String(error)}`); }
  finally { enabling.value = false; }
}

let pairingPoll: ReturnType<typeof setInterval> | undefined;

// The window opens before the device posts its begin, so the initial status
// carries an empty pairing_id/code; poll until the short code shows up.
function startPairingPoll() {
  stopPairingPoll();
  pairingPoll = setInterval(async () => {
    try {
      const status = await monitorService.pairingStatus();
      if (status.state === "pending") localPairing.value = status;
      else { localPairing.value = null; stopPairingPoll(); }
    } catch { /* transient IPC errors: keep polling */ }
  }, 1000);
}

function stopPairingPoll() {
  clearInterval(pairingPoll);
  pairingPoll = undefined;
}

async function discover() {
  discovering.value = true;
  try {
    discovery.value = await monitorService.discover(10000);
    serialPorts.value = await monitorService.serialPorts().catch(() => []);
    if (discovery.value.devices.length) toast(`发现 ${discovery.value.devices.length} 台设备`);
    else toast("未发现新设备");
  } catch (error) {
    toast(`发现失败：${String(error)}`);
  } finally {
    discovering.value = false;
  }
}

function requestPairing(device: DiscoveredDevice) {
  pairTarget.value = device;
  riskConfirmed.value = false;
}

function cancelPairingRequest() {
  pairTarget.value = null;
  riskConfirmed.value = false;
}

async function beginPairing() {
  const device = pairTarget.value;
  if (!device || !riskConfirmed.value) return;
  try {
    localPairing.value = await monitorService.beginPairing(device.device_id, true);
    pairTarget.value = null;
    riskConfirmed.value = false;
    toast("配对已开始，等待设备显示短码…");
    startPairingPoll();
  } catch (error) {
    toast(`开始配对失败：${String(error)}`);
  }
}

async function confirmPairing(approve: boolean) {
  if (!localPairing.value?.pairing_id) return;
  try {
    const status = await monitorService.confirmPairing(localPairing.value.pairing_id, approve);
    stopPairingPoll();
    localPairing.value = status.state === "pending" ? status : null;
    toast(approve ? "已确认配对" : "已拒绝配对");
  } catch (error) {
    toast(`确认失败：${String(error)}`);
  }
}

async function revoke(deviceID: string) {
  try {
    await monitorService.revoke(deviceID);
    toast("已解除配对");
  } catch (error) {
    toast(`解除失败：${String(error)}`);
  }
}

async function toggleDevice(deviceID: string, enabled: boolean) {
  try {
    await monitorService.setEnabled(deviceID, enabled);
    toast(enabled ? "设备已启用" : "设备已停用");
  } catch (error) {
    toast(`操作失败：${String(error)}`);
  }
}

async function probeAndConnect(port: DiscoveredDevice) {
  try {
    const device = await monitorService.probeSerial(port.port);
    await monitorService.connectSerial(device.device_id, device.device_id, true);
    toast(`串口 ${port.port} 已连接`);
  } catch (error) {
    toast(`串口连接失败：${String(error)}`);
  }
}

onMounted(() => { void store.devices; });
onUnmounted(stopPairingPoll);
</script>

<template>
  <div>
    <div class="heading">
      <div>
        <div class="eyebrow">DEVICES</div>
        <h1>ESP32</h1>
        <p>发现、配对并管理 ESP32 Monitor 设备。</p>
      </div>
      <button class="btn primary" :disabled="discovering" @click="discover">{{ discovering ? "搜索中…" : "发现设备" }}</button>
    </div>
    <div v-if="!companionOn" class="banner">
      <div><b>上位机服务未启用</b>　ESP32 需要该服务才能连接并显示数据。</div>
      <button class="btn" :disabled="enabling" @click="enableCompanion">{{ enabling ? "启用中…" : "启用服务" }}</button>
    </div>
    <div class="grid">
      <section class="card span7">
        <div class="card-title"><h2>已配对设备</h2></div>
        <div v-if="!paired.length" class="muted">还没有已配对的设备。使用「发现设备」开始。</div>
        <div v-for="device in paired" :key="device.device_id" class="device">
          <div class="device-icon" aria-hidden="true"><i class="pixelart-icons-font-cpu"></i></div>
          <div class="device-info">
            <h3>{{ device.device_name }}　<span class="pill" :class="device.confirmed_online ? '' : 'gray'">{{ device.confirmed_online ? "在线" : "离线" }}</span></h3>
            <p>{{ device.device_id }}<br>{{ device.address || "—" }} · {{ connectionLabel(device.connection_state) }}<br>最近在线 {{ device.last_seen ? new Date(device.last_seen).toLocaleTimeString("zh-CN") : "—" }}</p>
          </div>
          <div class="actions">
            <button class="btn" @click="toggleDevice(device.device_id, false)">停用</button>
            <button class="btn" @click="revoke(device.device_id)">解除配对</button>
          </div>
        </div>
      </section>
      <section class="card span5">
        <div class="card-title"><h2>发现结果</h2></div>
        <div v-if="discovery?.lan_error" class="section-note">局域网扫描失败：{{ discovery.lan_error }}</div>
        <div v-if="discovery?.serial_error" class="section-note">串口扫描失败：{{ discovery.serial_error }}</div>
        <div v-if="discovery?.overflow" class="section-note">设备数量过多，仅显示前若干台。</div>
        <div v-if="!discovery?.devices.length" class="muted">点击「发现设备」扫描局域网与串口。</div>
        <div v-for="device in discovery?.devices ?? []" :key="device.device_id + device.port" class="row">
          <div><strong>{{ device.name || device.device_id }}</strong><small>{{ connectionTypeLabel(device.connection_type) }} · {{ device.address || device.port }}{{ device.identity_verified ? " · 身份已验证" : "" }}</small></div>
          <div class="row-right">
            <button v-if="!device.paired" class="btn" @click="requestPairing(device)">配对</button>
            <span v-else class="pill">已配对</span>
          </div>
        </div>
        <template v-if="serialPorts.length">
          <div class="card-title" style="margin-top:16px"><h2>可用串口</h2></div>
          <div v-for="port in serialPorts" :key="port.port" class="row">
            <div><strong>{{ port.port }}</strong><small>{{ port.name || "串口设备" }}</small></div>
            <div class="row-right"><button class="btn" @click="probeAndConnect(port)">探测并连接</button></div>
          </div>
        </template>
      </section>
      <section v-if="pairTarget && !localPairing" class="card span12">
        <div class="card-title"><h2>配对风险确认</h2></div>
        <div class="row">
          <div><strong>局域网明文传输警告</strong><small>配对会在局域网内明文传输密钥，请仅在可信网络中操作。勾选表示你已了解该风险，并确认配对 {{ pairTarget.name || pairTarget.device_id }}。</small></div>
          <input class="switch" type="checkbox" v-model="riskConfirmed" aria-label="确认局域网明文传输风险">
        </div>
        <div class="actions">
          <button class="btn primary" :disabled="!riskConfirmed" @click="beginPairing">开始配对</button>
          <button class="btn" @click="cancelPairingRequest">取消</button>
        </div>
      </section>
      <section v-if="localPairing" class="card span12">
        <div class="card-title"><h2>进行中的配对</h2><span class="pill">{{ connectionLabel(localPairing.state) }}</span></div>
        <p class="muted">设备短码：<strong class="code-big">{{ localPairing.code || "…" }}</strong>　{{ localPairing.code ? "请核对设备屏幕上的短码后确认。" : "等待设备发起配对…" }}</p>
        <div class="actions">
          <button class="btn primary" :disabled="!localPairing.pairing_id" @click="confirmPairing(true)">确认配对</button>
          <button class="btn" :disabled="!localPairing.pairing_id" @click="confirmPairing(false)">拒绝</button>
        </div>
      </section>
    </div>
  </div>
</template>
