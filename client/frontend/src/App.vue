<script setup lang="ts">
import { computed, onMounted, onUnmounted, type Component } from "vue";
import { pages, quitApp, store, startPolling, stopPolling } from "./store.js";
import type { PageID } from "./store.js";
import OverviewView from "./views/OverviewView.vue";
import MetricsView from "./views/MetricsView.vue";
import ServerView from "./views/ServerView.vue";
import CompanionView from "./views/CompanionView.vue";
import Esp32View from "./views/Esp32View.vue";
import StartupView from "./views/StartupView.vue";
import DiagnosticsView from "./views/DiagnosticsView.vue";
import appIcon from "../../assets/icons/app-icon-128.png";

const views: Record<PageID, Component> = {
  overview: OverviewView, metrics: MetricsView, server: ServerView, companion: CompanionView,
  esp32: Esp32View, startup: StartupView, diagnostics: DiagnosticsView,
};

const view = computed(() => views[store.page] ?? OverviewView);
const pageTitle = computed(() => pages.find(([id]) => id === store.page)?.[1] ?? "");

function goto(page: PageID) { store.page = page; }

onMounted(startPolling);
onUnmounted(stopPolling);
</script>

<template>
  <div class="shell">
    <aside class="sidebar">
      <div class="brand">
        <img class="logo brand-icon" :src="appIcon" alt="" aria-hidden="true">
        <div><strong>ESP32 MONITOR</strong><small>DESKTOP CLIENT</small></div>
      </div>
      <div class="nav-label">工作空间</div>
      <nav class="nav" aria-label="主导航">
        <button v-for="[id, name, icon] in pages" :key="id" :class="{ active: store.page === id }"
          :aria-current="store.page === id ? 'page' : undefined" @click="goto(id)">
          <span class="nav-icon"><i :class="icon" aria-hidden="true"></i></span>{{ name }}
        </button>
      </nav>
      <div class="sidefoot">
        V1 · 本地控制台
        <div class="sidefoot-actions"><button class="text-action" @click="quitApp">退出应用</button></div>
      </div>
    </aside>
    <main class="main">
      <header class="topbar">
        <div class="crumb">工作空间　/　{{ pageTitle }}</div>
        <div class="topright">
          <span class="pill" :class="store.statusError ? 'amber' : store.status ? '' : 'gray'">
            {{ store.statusError ? "Agent 不可达" : store.status ? "Agent 运行中" : "连接 Agent…" }}
          </span>
        </div>
      </header>
      <div class="content">
        <component :is="view" />
      </div>
    </main>
    <div v-if="store.toast" class="toast" role="status">{{ store.toast }}</div>
  </div>
</template>
