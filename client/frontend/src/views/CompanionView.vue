<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from "vue";
import { companionService } from "../services/bridge.js";
import type { GitHubSession } from "../types/companion.js";
import { cloneSettings, refreshCompanion, saveSettings, store, toast } from "../store.js";
import { taskStatusText } from "../services/codex-task.js";
import { fmtClockFromSeconds } from "../services/format.js";

const session = ref<GitHubSession | null>(null);
const loggingIn = ref(false);
let pollTimer: ReturnType<typeof setTimeout> | undefined;

async function persistGitHubLogin(login: string) {
  const current = store.settings;
  if (!current || !login || current.github.login === login) return;
  try {
    await saveSettings({ ...cloneSettings(current), github: { ...current.github, login } });
  } catch (error) {
    toast(`保存登录状态失败：${String(error)}`);
  }
}

const github = computed(() => store.companion?.github);
const codex = computed(() => store.companion?.codex);
const music = computed(() => store.companion?.music);
const calendar = computed(() => github.value?.data ?? null);
const wallLevels = computed(() => {
  if (!calendar.value) return [];
  return calendar.value.days.slice(-371).map(day => Math.max(0, Math.min(4, day.level)));
});
const quotaWindows = computed(() => codex.value?.quota.windows ?? []);

function quotaLabel(minutes?: number | null): string {
  if (!minutes) return "额度";
  if (minutes === 10080) return "周额度";
  if (minutes % 1440 === 0) return `${minutes / 1440}天额度`;
  if (minutes % 60 === 0) return `${minutes / 60}小时额度`;
  return `${minutes}分钟额度`;
}
const codexErrorText = computed(() => {
  switch (codex.value?.quota.health.error_code) {
    case "codex_unavailable": return "未找到 Codex CLI，请安装或启动 ChatGPT / Codex 客户端";
    case "auth_required": return "请先在 Codex 客户端完成登录";
    case "account_type_unsupported": return "当前账号类型不支持额度查询";
    case "source_version_unsupported": return "Codex 版本不支持额度查询";
    case "upstream_error": return "Codex 服务暂时不可用";
    case "quota_read_failed": return "额度读取失败，请稍后重试";
    default: return codex.value?.quota.health.error_code || "尚未获取 Codex 桌面端信息。";
  }
});

async function startLogin() {
  loggingIn.value = true;
  try {
    session.value = await companionService.githubLoginStart();
    if (session.value.state === "awaiting_authorization") schedulePoll();
  } catch (error) {
    toast(`登录失败：${String(error)}`);
  } finally {
    loggingIn.value = false;
  }
}

function schedulePoll() {
  clearTimeout(pollTimer);
  pollTimer = setTimeout(async () => {
    if (!session.value) return;
    try {
      session.value = await companionService.githubLoginPoll(session.value.id);
    } catch {
      session.value = await companionService.githubSession().catch(() => session.value);
    }
    if (session.value?.state === "awaiting_authorization") schedulePoll();
    else if (session.value?.state === "authorized") {
      toast(`GitHub 已登录：${session.value.login}`);
      await persistGitHubLogin(session.value.login ?? "");
      await refreshCompanion();
    }
  }, 1500);
}

async function cancelLogin() {
  clearTimeout(pollTimer);
  if (!session.value) return;
  try { session.value = await companionService.githubLoginCancel(session.value.id); } catch { session.value = null; }
}

async function logout() {
  const login = github.value?.data?.login;
  if (!login) return;
  try {
    await companionService.githubLogout(login);
    const current = store.settings;
    if (current?.github.login) {
      await saveSettings({ ...cloneSettings(current), github: { ...current.github, login: undefined } });
    }
    toast("已退出 GitHub 登录");
    await refreshCompanion();
  } catch (error) {
    toast(`退出失败：${String(error)}`);
  }
}

function fmtClock(seconds?: number | null): string {
  if (seconds === undefined || seconds === null) return "—";
  const m = Math.floor(seconds / 60), s = Math.floor(seconds % 60);
  return `${m}:${String(s).padStart(2, "0")}`;
}

// The snapshot carries the position at position_observed_at; advance it locally
// so the text tracks playback between the 5s companion polls.
const nowTick = ref(Date.now());
let clockTimer: ReturnType<typeof setInterval> | undefined;
const musicPosition = computed(() => {
  const current = music.value?.current;
  if (!current || current.position === null || current.position === undefined) return null;
  if (current.state !== "playing" || !current.position_observed_at) return current.position;
  const observed = Date.parse(current.position_observed_at);
  if (!Number.isFinite(observed)) return current.position;
  const value = current.position + Math.max(0, (nowTick.value - observed) / 1000);
  return current.duration != null ? Math.min(value, current.duration) : value;
});

onMounted(() => { clockTimer = setInterval(() => { nowTick.value = Date.now(); }, 1000); });
onUnmounted(() => {
  clearTimeout(pollTimer);
  clearInterval(clockTimer);
});
</script>

<template>
  <div>
    <div class="heading">
      <div>
        <div class="eyebrow">COMPANION</div>
        <h1>上位机</h1>
        <p>管理 GitHub、Codex 与系统播放数据源；状态由 Agent 在后台刷新。</p>
      </div>
    </div>
    <div class="grid activity-layout">
      <section class="card span12">
        <div class="card-title"><h2>GitHub 贡献墙</h2>
          <span class="pill" :class="github?.health.state === 'healthy' ? '' : 'gray'">{{ github?.health.state === "healthy" ? "已授权" : "未就绪" }}</span>
        </div>
        <template v-if="calendar">
          <div class="wall-top">
            <div><strong>{{ calendar.login }}</strong><small>最近一年贡献 · 今天 {{ calendar.today ?? 0 }} 次</small></div>
            <button class="text-action" @click="logout">退出登录</button>
          </div>
          <div class="contribution-wall" aria-label="GitHub 贡献墙">
            <i v-for="(level, index) in wallLevels" :key="index" :class="`level-${level}`"></i>
          </div>
          <div class="wall-legend">较少 <i class="level-0"></i><i class="level-1"></i><i class="level-2"></i><i class="level-3"></i><i class="level-4"></i> 较多
            <span v-if="github?.stale">缓存数据 · 刷新中</span>
          </div>
        </template>
        <template v-else-if="session && session.state === 'awaiting_authorization'">
          <div class="source-empty">
            <strong>等待浏览器授权</strong>
            <p>在浏览器打开 {{ session.verification_uri }} 并输入代码。</p>
            <p class="code-big">{{ session.user_code }}</p>
            <div class="actions"><button class="btn" @click="cancelLogin">取消登录</button></div>
          </div>
        </template>
        <template v-else>
          <div class="source-empty">
            <div class="source-icon"><i class="pixelart-icons-font-github" aria-hidden="true"></i></div>
            <strong>连接 GitHub</strong>
            <p>登录后，已配对的 ESP32 可显示贡献墙。</p>
            <button class="btn primary" :disabled="loggingIn" @click="startLogin">登录 GitHub</button>
            <p v-if="session?.error_code" class="muted">{{ session.error_code }}</p>
          </div>
        </template>
      </section>

      <section class="card span6">
        <div class="card-title"><h2>Codex</h2>
          <span class="pill" :class="codex?.quota.health.state === 'healthy' ? '' : 'gray'">{{ codex?.quota.health.state === "healthy" ? "已连接" : "未连接" }}</span>
        </div>
        <template v-if="quotaWindows.length">
          <div v-for="window in quotaWindows" :key="window.bucket_id" class="quota">
            <div><span>{{ quotaLabel(window.window_minutes) }} <small>剩余</small></span><strong>{{ Math.round(window.remaining_percent ?? 0) }}%</strong></div>
            <div class="quota-bar"><i :style="{ width: `${Math.max(0, Math.min(100, window.remaining_percent ?? 0))}%` }"></i></div>
            <small v-if="window.resets_at">将于 {{ fmtClockFromSeconds(window.resets_at) }} 重置</small>
          </div>
        </template>
        <template v-else>
          <div class="source-empty compact">
            <div class="source-icon"><i class="pixelart-icons-font-circle" aria-hidden="true"></i></div>
            <strong>未连接</strong>
            <p>{{ codexErrorText }}</p>
          </div>
        </template>
        <div class="task-status">
          <span class="task-dot"></span>
          <div><small>当前任务</small>
            <strong>{{ taskStatusText(codex?.tasks) }}</strong>
          </div>
        </div>
      </section>

      <section class="card span6">
        <div class="card-title"><h2>系统当前播放</h2>
          <span class="pill" :class="music?.current?.state === 'playing' ? '' : 'gray'">{{ music?.current?.state === "playing" ? "播放中" : "未播放" }}</span>
        </div>
        <template v-if="music?.current">
          <div class="music-meta">
            <div><strong>{{ music.current.title || "未知曲目" }}</strong><small>{{ music.current.artist || "未知艺术家" }}</small></div>
          </div>
          <div class="music-source">{{ music.current.source || music.adapter }}</div>
          <div class="music-times"><span>{{ fmtClock(musicPosition) }}</span><span>{{ fmtClock(music.current.duration) }}</span></div>
        </template>
        <template v-else>
          <div class="source-empty compact">
            <div class="source-icon"><i class="pixelart-icons-font-music" aria-hidden="true"></i></div>
            <strong>{{ music?.health.state === "disabled" ? "未启用" : "未播放" }}</strong>
            <p>{{ music?.health.error_code || "当前没有歌曲信息。" }}</p>
          </div>
        </template>
      </section>
    </div>
  </div>
</template>
