<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { cloneSettings, store, saveSettings, toast } from "../store.js";
import { appService } from "../services/bridge.js";
import type { GuiSettings } from "../types/app.js";

const saving = ref(false);
let applied: GuiSettings | null = null;

const settings = computed(() => store.settings ?? null);
watch(settings, value => {
  if (value && !saving.value) applied = cloneSettings(value);
}, { immediate: true, deep: true });

// 开关即生效：每次改动立即保存，必要时重启 Agent，避免再点一次「保存」。
async function persist() {
  const current = settings.value;
  if (!current) return;
  const previous = applied;
  saving.value = true;
  try {
    await saveSettings(cloneSettings(current));
    applied = cloneSettings(store.settings ?? current);
    toast("设置已保存");
  } catch (error) {
    if (previous) store.settings = cloneSettings(previous);
    toast(`保存失败：${String(error)}`);
  } finally {
    saving.value = false;
  }
}

async function restartAgent() {
  try {
    await appService.restartAgent();
    toast("Agent 已重启");
  } catch (error) {
    toast(`重启失败：${String(error)}`);
  }
}
</script>

<template>
  <div>
    <div class="heading">
      <div>
        <div class="eyebrow">LIFECYCLE</div>
        <h1>启动与后台</h1>
        <p>选择应用如何运行。关闭窗口后，后台可以继续工作。</p>
      </div>
    </div>
    <div class="grid">
      <section class="card span7" v-if="settings">
        <div class="card-title"><h2>运行方式</h2></div>
        <div class="row">
          <div><strong>指标采集</strong><small>在后台采集本机指标；上传地址在「Server 连接」配置</small></div>
          <input v-model="settings.collector.enabled" class="switch" type="checkbox" aria-label="指标采集" :disabled="saving" @change="persist">
        </div>
        <div class="row">
          <div><strong>系统播放</strong><small>读取当前系统播放，需要 macOS 授权</small></div>
          <input v-model="settings.music.enabled" class="switch" type="checkbox" aria-label="系统播放" :disabled="saving" @change="persist">
        </div>
        <div class="row">
          <div><strong>Codex 桌面端</strong><small>读取 Codex App Server 额度与任务</small></div>
          <input v-model="settings.codex.enabled" class="switch" type="checkbox" aria-label="Codex 桌面端" :disabled="saving" @change="persist">
        </div>
        <div class="row">
          <div><strong>上位机服务</strong><small>向已配对的 ESP32 提供数据源内容</small></div>
          <input v-model="settings.companion.enabled" class="switch" type="checkbox" aria-label="上位机服务" :disabled="saving" @change="persist">
        </div>
        <div class="row">
          <div><strong>后台运行</strong><small>关闭窗口后继续采集和服务设备</small></div>
          <input v-model="settings.launch.close_to_background" class="switch" type="checkbox" aria-label="后台运行" :disabled="saving" @change="persist">
        </div>
        <div class="row">
          <div><strong>登录后启动</strong><small>登录电脑后自动启动；自动启动时窗口保持隐藏，仅在后台运行</small></div>
          <input v-model="settings.launch.launch_at_login" class="switch" type="checkbox" aria-label="登录后启动" :disabled="saving" @change="persist">
        </div>
        <div class="row">
          <div><strong>状态栏图标</strong><small>在菜单栏或系统托盘常驻图标，可快速打开窗口或退出；需开启「后台运行」才能关闭窗口后继续驻留</small></div>
          <input v-model="settings.launch.show_in_status_bar" class="switch" type="checkbox" aria-label="状态栏图标" :disabled="saving" @change="persist">
        </div>
        <div class="section-note">登录项和状态栏图标即时生效；采集与上位机相关设置保存后 Agent 会自动重启。自动启动会在后台运行；需要界面时点状态栏图标或重新打开应用。</div>
      </section>
      <section class="card span5">
        <div class="card-title"><h2>后台状态</h2></div>
        <div class="row"><div><strong>Agent</strong><small>PID {{ store.status?.pid ?? "—" }} · 启动于 {{ store.status?.start_time ? new Date(store.status.start_time).toLocaleTimeString("zh-CN") : "—" }}</small></div><div class="row-right"><span class="pill" :class="store.statusError ? 'amber' : ''">{{ store.statusError ? "不可达" : "运行中" }}</span></div></div>
        <div class="row"><div><strong>采集服务</strong><small>{{ store.status?.collector_enabled ? "用户会话内运行" : "未启用" }}</small></div><div class="row-right"><span class="pill" :class="store.status?.collector_enabled ? '' : 'gray'">{{ store.status?.collector_enabled ? "运行中" : "未启用" }}</span></div></div>
        <div class="row"><div><strong>上位机服务</strong><small>{{ store.status?.companion_enabled ? "监听局域网接口" : "未启用" }}</small></div><div class="row-right"><span class="pill" :class="store.status?.companion_enabled ? '' : 'gray'">{{ store.status?.companion_enabled ? "运行中" : "未启用" }}</span></div></div>
        <div class="row"><div><strong>数据源</strong><small>GitHub / Codex / 播放</small></div><div class="row-right"><span class="value muted">{{ [store.status?.github_enabled, store.status?.codex_enabled, store.status?.music_enabled].filter(Boolean).length }} / 3 启用</span></div></div>
        <div class="actions">
          <button class="btn" @click="restartAgent">重启 Agent</button>
          <button class="btn" @click="appService.quit().catch((error: unknown) => toast(`退出失败：${String(error)}`))">退出应用</button>
        </div>
        <p class="hint">退出应用会停止采集并断开 ESP32 连接。</p>
      </section>
    </div>
  </div>
</template>
