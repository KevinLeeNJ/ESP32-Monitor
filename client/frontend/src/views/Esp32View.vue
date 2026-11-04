<script setup lang="ts">
import { computed, nextTick, onUnmounted, ref, watch } from "vue";
import { monitorService } from "../services/bridge.js";
import type { DiscoveryResult, DiscoveredDevice, PairingStatus } from "../types/monitors.js";
import { cloneSettings, refreshDevices, saveSettings, store, toast } from "../store.js";
import { connectionLabel, connectionTypeLabel } from "../services/device-labels.js";
import { fmtClockTime } from "../services/format.js";

const discovering = ref(false);
const discovery = ref<DiscoveryResult | null>(null);
const localPairing = ref<PairingStatus | null>(null);
// Serial pairing starts before the Agent has a pairing window, so its poll must
// tolerate "closed" until the device posts its begin frame.
const pairingTransport = ref<"lan" | "serial" | null>(null);
const serialCancelRequested = ref(false);
const pairTarget = ref<DiscoveredDevice | null>(null);
const riskConfirmed = ref(false);
const riskDialog = ref<HTMLDialogElement | null>(null);
const pairingDialog = ref<HTMLDialogElement | null>(null);

const paired = computed(() => store.devices.filter(device => device.paired));
const companionOn = computed(() => Boolean(store.status?.companion_enabled));
// One combined list: network results first, serial ports after.
const discoveredDevices = computed(() => {
  const devices = [...(discovery.value?.devices ?? [])];
  return devices.sort((a, b) => (a.connection_type === "lan" ? 0 : 1) - (b.connection_type === "lan" ? 0 : 1));
});
function serialSession(device: DiscoveredDevice) {
  if (!device.device_id) return undefined;
  return store.devices.find(known => known.device_id === device.device_id && known.transport === "serial");
}
function serialOnline(device: DiscoveredDevice) {
  return serialSession(device)?.connection_state === "connected";
}
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
function startPairingPoll(awaitWindow = false) {
  stopPairingPoll();
  pairingPoll = setInterval(async () => {
    try {
      const status = await monitorService.pairingStatus();
      if (status.state === "pending") { localPairing.value = status; return; }
      if (status.state === "approved") {
        markPaired(status.device_id);
        localPairing.value = null;
        pairingTransport.value = null;
        stopPairingPoll();
        await refreshDevices();
        toast("配对成功");
        return;
      }
      if (status.state === "closed" && awaitWindow) return;
      localPairing.value = null;
      stopPairingPoll();
    } catch { /* transient IPC errors: keep polling */ }
  }, 1000);
}

// LAN and serial are two routes to the same Monitor. Pairing either one
// updates both rows instead of leaving the other route looking unpaired.
function markPaired(deviceID: string) {
  for (const device of discovery.value?.devices ?? []) {
    if (device.device_id === deviceID) device.paired = true;
  }
}

function stopPairingPoll() {
  clearInterval(pairingPoll);
  pairingPoll = undefined;
}

async function discover() {
  discovering.value = true;
  try {
    discovery.value = await monitorService.discover(10000);
    if (discovery.value.devices.length) toast(`发现 ${discovery.value.devices.length} 个连接`);
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
    pairingTransport.value = "lan";
    pairTarget.value = null;
    riskConfirmed.value = false;
    toast("配对已开始，等待设备显示短码…");
    startPairingPoll();
  } catch (error) {
    const message = String(error);
    if (message.includes("pairing_port_in_use")) {
      toast("LAN 配对失败：本机 TCP 18080 端口已被占用，请关闭占用该端口的程序后重试。");
    } else if (message.includes("session_conflict")) {
      markPaired(device.device_id);
      await refreshDevices();
      toast("该设备已配对");
    } else {
      toast(`开始配对失败：${message}`);
    }
  }
}

async function cancelPendingPairing() {
  const serial = pairingTransport.value === "serial";
  serialCancelRequested.value = serial;
  try {
    await monitorService.cancelPairing();
  } catch (error) {
    toast(`取消失败：${String(error)}`);
  }
  stopPairingPoll();
  localPairing.value = null;
  pairingTransport.value = null;
}

async function revoke(deviceID: string) {
  try {
    await monitorService.revoke(deviceID);
    await refreshDevices();
    toast("已解除配对");
  } catch (error) {
    toast(`解除失败：${String(error)}`);
  }
}

async function toggleDevice(deviceID: string, enabled: boolean) {
  try {
    await monitorService.setEnabled(deviceID, enabled);
    await refreshDevices();
    toast(enabled ? "设备已启用" : "设备已停用");
  } catch (error) {
    toast(`操作失败：${String(error)}`);
  }
}

// Native dialogs provide focus trapping and restoration; the watcher keeps
// them open only while their corresponding pairing step is active.
watch([pairTarget, localPairing], async ([target, pairing]) => {
  await nextTick();
  if (target && !pairing) {
    if (!riskDialog.value?.open) riskDialog.value?.showModal();
  } else if (riskDialog.value?.open) {
    riskDialog.value.close();
  }
  if (pairing) {
    if (!pairingDialog.value?.open) pairingDialog.value?.showModal();
  } else if (pairingDialog.value?.open) {
    pairingDialog.value.close();
  }
}, { flush: "post" });

// A freshly paired Monitor ends the pairing session on its own watchdog while
// the Agent's serial watcher claims the port again. That handoff is expected,
// so a closed session is only a failure if the watcher never reconnects.
const handoffErrors = ["context canceled", "session_expired", "serial_disconnected", "serial_write_failed"];

async function serialReconnected(deviceID: string, timeoutMS: number) {
  const deadline = Date.now() + timeoutMS;
  while (Date.now() < deadline) {
    await new Promise(resolve => setTimeout(resolve, 1000));
    await refreshDevices();
    const known = store.devices.find(device => device.device_id === deviceID);
    if (!known) return false; // pairing was not stored, so the watcher cannot reconnect
    if (known.transport === "serial" && known.connection_state === "connected") return true;
  }
  return false;
}

async function probeAndConnect(port: DiscoveredDevice) {
  let serialPairing = false;
  let deviceID = "";
  let deviceName = "";
  try {
    const device = await monitorService.probeSerial(port.port);
    deviceID = device.device_id;
    deviceName = device.name;
    if (!device.paired) {
      serialPairing = true;
      serialCancelRequested.value = false;
      pairingTransport.value = "serial";
      localPairing.value = {
        device_id: device.device_id, pairing_id: "", code: "", state: "pending", expires_in_ms: 120000,
      };
      startPairingPoll(true);
    }
    await monitorService.connectSerial(port.port, device.device_id, true);
    const item = discovery.value?.devices.find(d => d.connection_type === "serial" && d.port === port.port);
    if (item) {
      item.device_id = device.device_id;
      item.name = device.name;
      item.paired = true;
      item.state = "connected";
    }
    await refreshDevices();
    toast(`串口 ${port.port} 已连接`);
  } catch (error) {
    const message = String(error);
    if (serialCancelRequested.value) {
      toast("已取消串口配对");
    } else if (message.includes("serial_permission_denied")) {
      toast(`无权访问串口 ${port.port}。请检查串口设备权限（如 dialout 用户组或 udev 规则），调整后重新登录并重试。`);
    } else if (message.includes("serial_port_busy")) {
      toast("串口正在连接，请稍候");
    } else if (serialPairing && handoffErrors.some(reason => message.includes(reason)) && await serialReconnected(deviceID, 12000)) {
      const item = discovery.value?.devices.find(d => d.connection_type === "serial" && d.port === port.port);
      if (item) {
        item.device_id = deviceID;
        item.name = deviceName;
        item.paired = true;
        item.state = "connected";
      }
      toast(`串口 ${port.port} 已连接`);
    } else {
      toast(`串口连接失败：${message}`);
    }
  } finally {
    if (serialPairing) {
      stopPairingPoll();
      localPairing.value = null;
      pairingTransport.value = null;
      serialCancelRequested.value = false;
    }
  }
}

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
        <div v-if="store.devicesError" class="section-note">设备状态读取失败，以下为上次结果。</div>
        <div v-if="!paired.length" class="muted">{{ store.devicesError ? "无法读取设备列表。" : "还没有已配对的设备。使用「发现设备」开始。" }}</div>
        <div v-for="device in paired" :key="device.device_id" class="device">
          <div class="device-icon" aria-hidden="true"><i class="pixelart-icons-font-cpu"></i></div>
          <div class="device-info">
            <h3>{{ device.device_name }}　<span class="pill" :class="device.confirmed_online && !device.pairing_lost ? '' : 'gray'">{{ device.pairing_lost ? "待重新配对" : (device.confirmed_online ? "在线" : "离线") }}</span></h3>
            <p>{{ device.device_id }}<br>{{ device.address || "—" }}{{ device.transport === "serial" ? " · 串口" : device.transport === "lan" ? " · 局域网" : "" }} · {{ connectionLabel(device.connection_state) }}<br>最近在线 {{ fmtClockTime(device.last_seen) }}</p>
          </div>
          <div class="actions">
            <button class="btn" @click="toggleDevice(device.device_id, !device.enabled)">{{ device.enabled ? "停用" : "启用" }}</button>
            <button class="btn" @click="revoke(device.device_id)">解除配对</button>
          </div>
        </div>
      </section>
      <section class="card span5">
        <div class="card-title"><h2>发现结果</h2></div>
        <div v-if="discovery?.lan_error" class="section-note">局域网扫描失败：{{ discovery.lan_error }}</div>
        <div v-if="discovery?.serial_error" class="section-note">串口扫描失败：{{ discovery.serial_error }}</div>
        <div v-if="discovery?.overflow" class="section-note">设备数量过多，仅显示前若干台。</div>
        <div v-if="!discoveredDevices.length" class="muted">点击「发现设备」扫描局域网与串口。</div>
        <div v-for="device in discoveredDevices" :key="device.connection_type + ':' + (device.device_id || device.port)" class="row discovery-row">
          <div v-if="device.connection_type === 'serial'">
            <strong>{{ device.port }}</strong>
            <small>串口 · {{ device.name || "串口设备" }}{{ device.vid ? ` · VID ${device.vid}` : "" }}{{ device.pid ? ` PID ${device.pid}` : "" }}{{ serialOnline(device) ? " · 已连接" : device.paired ? " · 已配对" : "" }}</small>
          </div>
          <div v-else>
            <strong>{{ device.name || device.device_id }}</strong>
            <small>{{ connectionTypeLabel(device.connection_type) }} · {{ device.address || device.port || "—" }}{{ device.identity_verified ? " · 身份已验证" : "" }}</small>
          </div>
          <div class="row-right">
            <template v-if="device.connection_type === 'serial'">
              <span v-if="serialSession(device)" class="pill">{{ serialOnline(device) ? "已连接" : "连接中" }}</span>
              <button v-else class="btn" @click="probeAndConnect(device)">{{ device.paired ? "连接" : "探测并连接" }}</button>
            </template>
            <template v-else>
              <button v-if="!device.paired" class="btn" @click="requestPairing(device)">配对</button>
              <span v-else-if="serialSession(device)" class="pill">{{ serialOnline(device) ? "串口已连接" : "串口连接中" }}</span>
              <span v-else class="pill">已配对</span>
            </template>
          </div>
        </div>
      </section>
    </div>
    <Teleport to="body">
      <dialog ref="riskDialog" class="modal-dialog" aria-labelledby="pair-risk-title" @cancel.prevent="cancelPairingRequest">
        <section v-if="pairTarget" class="card modal-card">
          <div class="card-title"><h2 id="pair-risk-title">配对风险确认</h2><span class="pill amber">{{ pairTarget.name || pairTarget.device_id }}</span></div>
          <p class="muted">配对会在局域网内明文传输密钥，请仅在可信网络中操作。勾选表示你已了解该风险，并确认与这台设备配对。</p>
          <label class="row modal-check">
            <span><strong>我已了解局域网明文传输风险</strong></span>
            <input class="switch" type="checkbox" v-model="riskConfirmed" aria-label="确认局域网明文传输风险">
          </label>
          <div class="actions modal-actions">
            <button class="btn danger" :disabled="!riskConfirmed" @click="beginPairing">确认并开始配对</button>
            <button class="btn" @click="cancelPairingRequest">取消</button>
          </div>
        </section>
      </dialog>
      <dialog ref="pairingDialog" class="modal-dialog" aria-labelledby="pairing-title" @cancel.prevent="cancelPendingPairing">
        <section v-if="localPairing" class="card modal-card">
          <div class="card-title">
            <h2 id="pairing-title">配对确认</h2>
            <span class="pill">{{ pairingTransport === "serial" ? "串口" : "局域网" }}</span>
          </div>
          <p class="muted">设备短码：<strong class="code-big">{{ localPairing.code || "…" }}</strong>　{{ localPairing.code ? "请核对设备屏幕上的短码，然后在设备上按 KEY 确认；按 BOOT 取消。" : "等待设备发起配对…" }}</p>
          <div class="actions modal-actions">
            <button class="btn" @click="cancelPendingPairing">取消等待</button>
          </div>
        </section>
      </dialog>
    </Teleport>
  </div>
</template>

<style scoped>
.modal-dialog{width:min(560px,calc(100vw - 48px));max-width:none;max-height:none;padding:0;border:0;background:transparent;overflow:visible}
.modal-dialog::backdrop{background:#11191599}
.modal-card{width:100%;max-height:calc(100vh - 48px);overflow:auto;padding:24px;box-shadow:0 24px 70px #0006}
.modal-check{border-bottom:0;padding:8px 0 18px}
.modal-actions{justify-content:flex-end;margin-top:8px}
/* The pixel theme paints button faces on ::after, so set its custom colors. */
.btn.danger{--button-frame:#6f241b;--button-face:#8f3327;color:#fff}
.btn.danger:hover{--button-frame:#571b14;--button-face:#a13c2e}
.btn.danger:disabled{--button-frame:#8f453b;--button-face:#a8564a;color:#fff;cursor:not-allowed}
.code-big{font-family:inherit;font-size:24px;letter-spacing:.18em;color:#243c35}
</style>
