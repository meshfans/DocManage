<script setup lang="ts">
import { ref, computed } from "vue";
import { IconifyIconOffline } from "@/components/ReIcon";
import logoImg from "@/assets/logo.png";

defineOptions({ name: "SystemAbout" });

// 版本 + 构建时间：vite 构建期注入（build/utils.ts.__APP_INFO__）
declare const __APP_INFO__: {
  pkg: { name: string; version: string };
  lastBuildTime: string;
};

const version = ref(__APP_INFO__.pkg.version ?? "0.0.0");
const buildTime = ref(__APP_INFO__.lastBuildTime ?? "");
const buildTimeFormatted = ref(buildTime.value || "—");
const buildYear = computed(() => {
  const t = buildTime.value ? new Date(buildTime.value) : null;
  return t && !isNaN(t.getTime()) ? t.getFullYear() : new Date().getFullYear();
});

/**
 * 联系方式（只展示，不点击）
 */
interface ContactItem {
  icon: string;
  label: string;
  display: string;
  href: string | null;
}

/** 判断是否为需要新窗口打开的 http(s) 链接 */
function isExternalHttp(href: string): boolean {
  return /^https?:\/\//i.test(href);
}

const contacts: ContactItem[] = [
  {
    icon: "ri:mail-line",
    label: "邮箱",
    display: "rfr@163.com",
    href: "mailto:rfr@163.com"
  },
  {
    icon: "ri:global-line",
    label: "网站",
    display: "https://www.meshfans.com/",
    href: "https://www.meshfans.com/"
  },
  {
    icon: "ri:wechat-2-line",
    label: "微信",
    display: "13964827272",
    href: null
  }
];
</script>

<template>
  <div class="about-wrapper">
    <div class="about-card">
      <!-- 左侧：Logo + 名称 -->
      <aside class="aside">
        <img :src="logoImg" alt="DocManage" class="logo" />
        <h1 class="app-name">DocManage</h1>
        <p class="app-sub">
          文档管理系统
        </p>
        <p class="version">v{{ version }} · {{ buildTimeFormatted }}</p>
      </aside>

      <!-- 右侧：联系方式 + 版权 -->
      <main class="main">
        <section class="section">
          <h2 class="section-title">联系方式</h2>
          <ul class="contact-list">
            <li v-for="c in contacts" :key="c.label" class="contact-row">
              <IconifyIconOffline :icon="c.icon" class="contact-icon" />
              <span class="contact-label">{{ c.label }}</span>
              <span class="contact-sep">·</span>
              <a
                v-if="c.href"
                :href="c.href"
                class="contact-value contact-link"
                :target="isExternalHttp(c.href) ? '_blank' : undefined"
                :rel="isExternalHttp(c.href) ? 'noopener noreferrer' : undefined"
                >{{ c.display }}</a
              >
              <span v-else class="contact-value">{{ c.display }}</span>
            </li>
          </ul>
        </section>

        <div class="divider" />

        <section class="section copyright-section">
          <h2 class="section-title">版权信息</h2>
          <p class="copyright-line">
            <strong>DocManage</strong> 版权所有 © 2018-{{ buildYear }}
          </p>
          <p class="copyright-sub">
            本软件受中华人民共和国著作权法及相关国际条约保护
          </p>
        </section>
      </main>
    </div>
  </div>
</template>

<style scoped>
.about-wrapper {
  display: flex;
  justify-content: center;
  align-items: center;
  width: 100%;
  height: 100%;
  min-height: 100%;
  padding: 24px 16px;
  box-sizing: border-box;
}

.about-card {
  display: flex;
  align-items: stretch;
  width: 100%;
  max-width: 760px;
  background: var(--el-bg-color);
  border-radius: 12px;
  overflow: hidden;
  box-shadow:
    0 1px 2px rgba(0, 0, 0, 0.04),
    0 4px 16px rgba(0, 0, 0, 0.06);
  transition:
    background 0.2s,
    box-shadow 0.2s;
}

/* html.dark 下加深 box-shadow */
:global(html.dark) .about-card {
  box-shadow:
    0 1px 2px rgba(0, 0, 0, 0.3),
    0 4px 16px rgba(0, 0, 0, 0.45);
}

.aside {
  width: 220px;
  background: var(--el-fill-color-light);
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 32px 20px;
  text-align: center;
  flex-shrink: 0;
}
.logo {
  width: 72px;
  height: 72px;
  border-radius: 16px;
  margin-bottom: 14px;
}
.app-name {
  margin: 0 0 6px;
  font-size: 22px;
  font-weight: 600;
  color: var(--el-text-color-primary);
}
.app-sub {
  margin: 0 0 10px;
  font-size: 12px;
  color: var(--el-text-color-regular);
  line-height: 1.5;
}
.version {
  margin: 0;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

.main {
  flex: 1;
  padding: 28px 32px;
  min-width: 0;
}
.section + .section {
  margin-top: 16px;
}
.section-title {
  margin: 0 0 12px;
  font-size: 13px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  letter-spacing: 0.5px;
}

.contact-list {
  list-style: none;
  margin: 0;
  padding: 0;
}
.contact-row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 0;
  font-size: 14px;
  color: var(--el-text-color-primary);
}
.contact-row + .contact-row {
  border-top: 1px dashed var(--el-border-color-lighter);
}
.contact-icon {
  font-size: 18px;
  color: var(--el-color-primary);
  flex-shrink: 0;
}
.contact-label {
  font-weight: 500;
  color: var(--el-text-color-regular);
}
.contact-sep {
  color: var(--el-text-color-placeholder);
}
.contact-value {
  color: var(--el-text-color-primary);
  word-break: break-all;
  font-family: ui-monospace, "SF Mono", Consolas, monospace;
  font-size: 13px;
}
.contact-link {
  text-decoration: none;
  color: inherit;
  cursor: pointer;
  border-bottom: 1px dashed transparent;
  transition:
    border-color 0.18s,
    color 0.18s;
}
.contact-link:hover {
  color: var(--el-color-primary);
  border-bottom-color: var(--el-color-primary);
}

.divider {
  height: 1px;
  background: var(--el-border-color-lighter);
  margin: 20px 0 0;
}

.copyright-section {
  margin-top: 16px;
}
.copyright-line {
  margin: 0 0 6px;
  font-size: 14px;
  color: var(--el-text-color-primary);
}
.copyright-sub {
  margin: 0;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}

@media (max-width: 720px) {
  .about-card {
    flex-direction: column;
    max-width: 420px;
  }
  .aside {
    width: 100%;
    padding: 24px 20px;
  }
  .main {
    padding: 20px 24px;
  }
}
</style>
