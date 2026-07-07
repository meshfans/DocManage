<script setup lang="ts">
/**
 * 欢迎首页（/welcome）
 *
 * 2026-06-27 v2.0 RBAC 重写：
 *   - 旧版只有 6 个全局统计 + 4 个固定快捷入口，所有登录用户看到的都一样
 *   - 新版按 RBAC 权限码动态渲染模块：用户无权限的模块既不显示卡片也不发请求
 *   - 统计字段从 6 个扩展到 14 个（覆盖客户分项 / 文档终态 / 第三方合同 / 模板 / 印章 / 媒体 / 流转 / 提醒）
 *   - 后端 dashboard.go 已同步扩展（best-effort COUNT，任意单 COUNT 失败不影响其它）
 * 2026-07-07 "三方文档/第三方合同"→"文档"用户可见文案统一
 *
 * RBAC 设计：
 *   - 模块级权限（hasAnyPerms / OR 语义）：任一权限命中即显示对应模块
 *   - 卡片级权限：每张统计卡片可独立按 perm 控制（hasPerms / AND 语义）
 *   - 入口权限：点击跳转的页面若用户无权限，路由守卫会在 router/index.ts 兜底跳 403
 */
import { ref, computed, onMounted } from "vue";
import { useRouter } from "vue-router";
import { ElMessage } from "element-plus";
import {
  User,
  Document,
  Warning,
  Bell,
  Clock,
  VideoCamera,
  PictureFilled,
  Refresh,
  DataAnalysis,
  Histogram,
  Avatar,
  House,
  Promotion,
  Files,
  Medal
} from "@element-plus/icons-vue";
import { http } from "@/utils/http";
import { hasAnyPerms, hasPerms } from "@/utils/auth";
import { useUserStore } from "@/store/modules/user";

defineOptions({
  name: "Welcome"
});

const router = useRouter();
const userStore = useUserStore();

// ==================== 状态 ====================
const loading = ref(false);
const lastUpdated = ref<number>(0);

// 完整的 DashboardStats 类型（与后端 backend/database/dashboard.go 一一对应）
// 2026-07-06 round2 精简：total_contracts/draft_contracts/filled_contracts/signed_contracts/
//   cancelled_contracts/archived_contracts/total_templates/total_seals/pending_flow_instances 全删
//   （主合同/模板/印章/流转模块下线）
type DashboardStats = {
  // 客户
  total_customers: number;
  individual_customers: number;
  enterprise_customers: number;
  // 文档
  total_third_party_contracts: number;
  // 媒体
  total_media: number;
  // 提醒
  active_reminder_subscriptions: number;
};

const stats = ref<DashboardStats>({
  total_customers: 0,
  individual_customers: 0,
  enterprise_customers: 0,
  total_third_party_contracts: 0,
  total_media: 0,
  active_reminder_subscriptions: 0
});

// 当前用户是否为 admin（用于显示"系统总览"全量面板）
const isAdmin = computed(() => userStore.roles?.includes("admin") ?? false);

// 当前用户昵称（用于问候）
const greeting = computed(() => {
  const h = new Date().getHours();
  if (h < 6) return "凌晨好";
  if (h < 12) return "早上好";
  if (h < 14) return "中午好";
  if (h < 18) return "下午好";
  return "晚上好";
});

// ==================== RBAC 模块定义 ====================
// 每个模块包含：
//   key: 唯一标识
//   title: 显示标题
//   perms: 模块级权限（OR 语义，任一命中即显示）
//   icon: Element Plus 图标
//   color: 渐变色
//   cards: 该模块下的统计卡片
type ModuleCard = {
  key: string;
  label: string;
  /** 该卡片独立权限（AND 语义，全部命中才显示）；空数组表示只要模块可见就显示 */
  perms?: string[];
  /** 取值函数：从 stats 中取数 */
  getValue: (s: DashboardStats) => number;
  /** 点击跳转路径 */
  path: string;
  /** 卡片角标颜色（可选） */
  tone?: "primary" | "success" | "warning" | "info" | "danger";
  /** 是否为重点数据（更大字号） */
  highlight?: boolean;
};

type ModuleDef = {
  key: string;
  title: string;
  perms: string[];
  icon: any;
  color: string;
  description: string;
  cards: ModuleCard[];
  /**
   * 该模块在一行里占几列（≥md 断点）
   * - 2 = 大模块：业务核心、子卡片多（≥5 个），给 2 列以放大展示
   * - 3 = 默认值：子卡片少（1-2 个），用 3 列保持紧凑
   * 响应式：
   *   xs (<768px)   → 1 列（与 col-sm 一致）
   *   sm (≥768px)  → 2 列
   *   md (≥992px)  → 12/columns（2 列=12，3 列=8）
   *   lg (≥1200px) → 同 md
   *   xl (≥1920px) → 同 md
   */
  columns?: 2 | 3;
};

/**
 * 模块清单（按业务重要性排序）
 *
 * 设计原则：
 *   1. 每个模块对应左侧菜单的一个一级路由，与 router/modules/*.ts 保持 1:1
 *   2. 模块级权限直接复用 router/modules 的 meta.permissions（统一权威源）
 *   3. 卡片级权限可以更细（如 contract:list 已隐含 contract:detail，故不重复）
 */
const MODULES: ModuleDef[] = [
  // ==================== 文档列表（2026-07-06 round2：PDF/流转全删，仅保留三方）====================
  {
    key: "contract",
    title: "文档列表",
    perms: ["contract:detail"],
    icon: Document,
    color: "linear-gradient(135deg, #409EFF 0%, #66b1ff 100%)",
    description: "文档登记 + 归档",
    cards: [
      {
        key: "third_party_contracts",
        label: "文档总数",
        perms: ["contract:detail"],
        getValue: s => s.total_third_party_contracts,
        path: "/contract/list",
        highlight: true
      }
    ]
  },

  // ==================== 客户管理（2 列大模块）====================
  {
    key: "customer",
    title: "客户管理",
    perms: ["customer:list", "customer:detail"],
    icon: User,
    color: "linear-gradient(135deg, #67C23A 0%, #85ce61 100%)",
    description: "个人 / 企业客户档案",
    columns: 2,
    cards: [
      {
        key: "total_customers",
        label: "客户总数",
        perms: ["customer:list"],
        getValue: s => s.total_customers,
        path: "/customer/individual",
        highlight: true
      },
      {
        key: "individual_customers",
        label: "个人客户",
        perms: ["customer:list"],
        getValue: s => s.individual_customers,
        path: "/customer/individual",
        tone: "primary"
      },
      {
        key: "enterprise_customers",
        label: "企业客户",
        perms: ["customer:list"],
        getValue: s => s.enterprise_customers,
        path: "/customer/enterprise",
        tone: "warning"
      }
    ]
  },

  // 2026-07-06 round2 精简：template 模块（PDF 模板库）已下线
  // 2026-07-06 round2 精简：flow 模块（流转管理）已下线

  // ==================== 媒体库（2026-07-06 精简：从媒体中心改名）====================
  {
    key: "media",
    title: "媒体库",
    perms: ["media:list", "media:by-target", "media:detail"],
    icon: VideoCamera,
    color: "linear-gradient(135deg, #9c27b0 0%, #ba68c8 100%)",
    description: "照片 / 录像 / 音频证据库",
    cards: [
      {
        key: "total_media",
        label: "媒体总数",
        perms: ["media:list"],
        getValue: s => s.total_media,
        path: "/media/library",
        highlight: true
      }
    ]
  },

  // ==================== 提醒管理 ====================
  {
    key: "reminder",
    title: "提醒管理",
    perms: [
      "reminder:templates:list",
      "reminder:subscriptions:list",
      "reminder:logs"
    ],
    icon: Bell,
    color: "linear-gradient(135deg, #ff5722 0%, #ff8a65 100%)",
    description: "合同到期 / 提醒订阅 / 发送日志",
    cards: [
      {
        key: "active_reminder_subscriptions",
        label: "活跃订阅",
        perms: ["reminder:subscriptions:list"],
        getValue: s => s.active_reminder_subscriptions,
        path: "/system/reminder",
        tone: "warning",
        highlight: true
      }
    ]
  }
];

/**
 * 过滤出当前用户有权访问的模块
 * - 模块级权限用 OR（hasAnyPerms），任一命中即保留
 * - 卡片级权限用 AND（hasPerms），全部命中才显示
 * - 2026-06-27 v2.1：值为 0 的子卡片隐藏（避免空模块视觉噪音）
 *   例：客户总数 0 时，整个"客户管理"模块隐藏（无可点数据）
 *   例：用户只有 1 个文档时，仅显示"文档总数"+"草稿"，其它 0 值卡片隐藏
 *   注：仅在首次成功加载（lastUpdated > 0）后启用 0 值过滤，
 *     防止"请求中 → 全 0 → 整页空白"的视觉闪烁
 */
const visibleModules = computed(() => {
  const hideZero = lastUpdated.value > 0;
  // 单卡过滤：权限 + 0 值
  const filterCard = (c: ModuleCard) => {
    // 权限：未定义 / 空数组 → 放行；否则 AND 命中
    const passesPerm =
      !c.perms || c.perms.length === 0 || hasPerms(c.perms);
    if (!passesPerm) return false;
    // 0 值过滤（仅在首次加载完成后生效）
    if (hideZero && c.getValue(stats.value) === 0) return false;
    return true;
  };
  return MODULES.filter(m => {
    if (!hasAnyPerms(m.perms)) return false;
    // 二次过滤：模块下没有任何可见卡片，则整个模块隐藏
    return m.cards.some(filterCard);
  }).map(m => ({
    ...m,
    cards: m.cards.filter(filterCard)
  }));
});

/**
 * 顶部 hero 卡片（最高优先级 / 全局摘要）
 * 仅当用户拥有对应业务的查看权限时显示
 * 2026-07-06 round2 精简：主合同相关 hero（total_contracts / signed_contracts /
 *   archived_contracts / draft_contracts）已下线；保留"文档总数" + "客户总数"
 */
const heroCards = computed(() => {
  const cards: Array<{
    key: string;
    label: string;
    value: number;
    path: string;
    icon: any;
    color: string;
    perms: string[];
  }> = [
    {
      key: "total_third_party_contracts",
      label: "文档总数",
      value: stats.value.total_third_party_contracts,
      path: "/contract/list",
      icon: Document,
      color: "linear-gradient(135deg, #409EFF 0%, #66b1ff 100%)",
      perms: ["contract:detail"]
    },
    {
      key: "total_customers",
      label: "客户总数",
      value: stats.value.total_customers,
      path: "/customer/individual",
      icon: User,
      color: "linear-gradient(135deg, #9c27b0 0%, #ba68c8 100%)",
      perms: ["customer:list"]
    }
  ];
  return cards.filter(c => hasPerms(c.perms));
});

// ==================== 快捷操作（RBAC 驱动）====================
type QuickAction = {
  title: string;
  desc: string;
  icon: any;
  color: string;
  path: string;
  /** 任一权限命中即显示（OR 语义） */
  perms: string[];
};

const ALL_ACTIONS: QuickAction[] = [
  // 2026-07-06 round2 精简：删除"新建文档"（主合同编辑页已下线）+ "我的待办"（流转模块下线）
  {
    title: "新建客户",
    desc: "添加个人 / 企业客户",
    icon: Avatar,
    color: "#67C23A",
    path: "/customer/individual",
    perms: ["customer:create"]
  },
  {
    title: "新建提醒",
    desc: "订阅合同到期提醒",
    icon: Bell,
    color: "#ff5722",
    path: "/system/reminder",
    perms: ["reminder:subscriptions:create"]
  },
  {
    title: "上传媒体",
    desc: "照片 / 录像 / 音频证据",
    icon: PictureFilled,
    color: "#9c27b0",
    path: "/media/library",
    perms: ["media:upload"]
  },
  {
    title: "部门管理",
    desc: "组织架构维护",
    icon: House,
    color: "#17a2b8",
    path: "/user-mgmt/departments",
    perms: ["dept:list"]
  }
];

const visibleActions = computed(() =>
  ALL_ACTIONS.filter(a => hasAnyPerms(a.perms))
);

// ==================== 方法 ====================
// 2026-07-06 精简：原 /api/dashboard/stats（聚合主合同）已下线。
//   改为按模块单独调用各模块的列表接口，统计字段独立填充。
async function loadStats() {
  loading.value = true;
  try {
    // 文档总数
    const tpRes: any = await http.request("get", "/api/third-party/contracts", { params: { page: 1, page_size: 1 } });
    if (tpRes.success && tpRes.data) {
      stats.value.total_third_party_contracts = tpRes.data.total ?? 0;
    }
    // 媒体总数
    const mRes: any = await http.request("get", "/api/media", { params: { page: 1, page_size: 1 } });
    if (mRes.success && mRes.data) {
      stats.value.total_media = mRes.data.total ?? 0;
    }
    lastUpdated.value = Date.now();
  } catch (error: any) {
    console.error("加载统计数据失败", error);
    ElMessage.warning(
      "统计数据加载失败：部分模块可能无法显示（" + (error?.message || error) + "）"
    );
  } finally {
    loading.value = false;
  }
}

function onRefresh() {
  loadStats();
}

function goTo(path: string) {
  if (!path) return;
  router.push(path);
}

function formatTime(ts: number | null | undefined) {
  if (!ts) return "—";
  const d = new Date(ts);
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(
    d.getHours()
  )}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

onMounted(() => {
  loadStats();
});
</script>

<template>
  <div class="welcome-container" v-loading="loading">
    <!-- ==================== 顶部 Hero ==================== -->
    <div class="welcome-hero">
      <div class="hero-content">
        <div class="hero-greeting">
          <h1>{{ greeting }}，{{ userStore.nickname || userStore.username || "用户" }} 👋</h1>
          <p class="hero-subtitle">
            模小范文档管理系统工作台
          </p>
        </div>
        <div class="hero-meta">
          <div class="hero-meta-row">
            <el-icon><Clock /></el-icon>
            <span>最后更新：{{ formatTime(lastUpdated) }}</span>
          </div>
          <el-button
            type="primary"
            :icon="Refresh"
            circle
            size="default"
            :loading="loading"
            @click="onRefresh"
            title="刷新数据"
          />
        </div>
      </div>
      <!-- 装饰：背景层叠渐变 + 浮动光圈 -->
      <div class="hero-deco hero-deco-1"></div>
      <div class="hero-deco hero-deco-2"></div>
    </div>

    <!-- ==================== Hero 统计（核心摘要）==================== -->
    <div v-if="heroCards.length > 0" class="hero-stats">
      <div
        v-for="card in heroCards"
        :key="card.key"
        class="hero-stat"
        @click="goTo(card.path)"
      >
        <div class="hero-stat-icon" :style="{ background: card.color }">
          <el-icon :size="28"><component :is="card.icon" /></el-icon>
        </div>
        <div class="hero-stat-body">
          <div class="hero-stat-value">{{ card.value }}</div>
          <div class="hero-stat-label">{{ card.label }}</div>
        </div>
      </div>
    </div>

    <!-- ==================== 模块化统计卡片 ==================== -->
    <div class="modules-section">
      <div class="section-title">
        <el-icon><DataAnalysis /></el-icon>
        <span>业务模块概览</span>
        <el-tag size="small" type="info" effect="plain">
          按权限显示（{{ visibleModules.length }}/{{ MODULES.length }}）
        </el-tag>
      </div>

      <!-- 用户没有任何模块可见时的兜底 -->
      <el-empty
        v-if="visibleModules.length === 0"
        description="当前账号未分配业务模块权限，请联系管理员"
        :image-size="100"
      />

      <el-row :gutter="16" v-else>
        <el-col
          v-for="mod in visibleModules"
          :key="mod.key"
          :xs="24"
          :sm="12"
          :md="mod.columns === 2 ? 12 : 8"
          :lg="mod.columns === 2 ? 12 : 8"
          :xl="mod.columns === 2 ? 12 : 8"
        >
          <el-card class="module-card" shadow="hover">
            <template #header>
              <div class="module-card-header">
                <div class="module-card-title">
                  <div
                    class="module-card-icon"
                    :style="{ background: mod.color }"
                  >
                    <el-icon :size="20"><component :is="mod.icon" /></el-icon>
                  </div>
                  <div>
                    <div class="module-card-name">{{ mod.title }}</div>
                    <div class="module-card-desc">{{ mod.description }}</div>
                  </div>
                </div>
              </div>
            </template>

            <div class="module-cards">
              <div
                v-for="card in mod.cards"
                :key="card.key"
                class="stat-tile"
                :class="{
                  'stat-tile-highlight': card.highlight,
                  [`stat-tile-${card.tone || 'default'}`]: true
                }"
                @click="goTo(card.path)"
              >
                <div class="stat-tile-label">{{ card.label }}</div>
                <div class="stat-tile-value">
                  {{ card.getValue(stats) }}
                </div>
              </div>
            </div>
          </el-card>
        </el-col>
      </el-row>
    </div>

    <!-- ==================== 快捷操作 ==================== -->
    <div v-if="visibleActions.length > 0" class="actions-section">
      <div class="section-title">
        <el-icon><Promotion /></el-icon>
        <span>快捷操作</span>
        <el-tag size="small" type="info" effect="plain">
          按权限显示（{{ visibleActions.length }}/{{ ALL_ACTIONS.length }}）
        </el-tag>
      </div>
      <el-row :gutter="16">
        <el-col
          v-for="action in visibleActions"
          :key="action.path"
          :xs="12"
          :sm="8"
          :md="6"
          :lg="4"
        >
          <div class="action-tile" @click="goTo(action.path)">
            <div
              class="action-tile-icon"
              :style="{ background: action.color }"
            >
              <el-icon :size="22"><component :is="action.icon" /></el-icon>
            </div>
            <div class="action-tile-body">
              <div class="action-tile-title">{{ action.title }}</div>
              <div class="action-tile-desc">{{ action.desc }}</div>
            </div>
          </div>
        </el-col>
      </el-row>
    </div>

    <!-- ==================== 管理员专属：系统状态 ==================== -->
    <div v-if="isAdmin" class="admin-section">
      <div class="section-title">
        <el-icon><Histogram /></el-icon>
        <span>管理员视图 · 系统状态</span>
        <el-tag size="small" type="warning" effect="dark">admin only</el-tag>
      </div>
      <el-row :gutter="20">
        <!-- 2026-07-06 round3 精简：审计日志 admin-tile 删除 -->
        <el-col :xs="24" :sm="12" :md="6">
          <div
            class="admin-tile"
            @click="goTo('/system/scheduled-tasks')"
            v-perms="'scheduled:list'"
          >
            <el-icon class="admin-tile-icon" :size="22" color="#67C23A"
              ><Clock
            /></el-icon>
            <div class="admin-tile-body">
              <div class="admin-tile-label">定时任务</div>
              <div class="admin-tile-desc">调度器 / 演练</div>
            </div>
          </div>
        </el-col>
        <el-col :xs="24" :sm="12" :md="6">
          <div
            class="admin-tile"
            @click="goTo('/system/backup')"
            v-perms="'backup:list'"
          >
            <el-icon class="admin-tile-icon" :size="22" color="#E6A23C"
              ><Files
            /></el-icon>
            <div class="admin-tile-body">
              <div class="admin-tile-label">备份管理</div>
              <div class="admin-tile-desc">全量 / 增量 / 演练</div>
            </div>
          </div>
        </el-col>
        <el-col :xs="24" :sm="12" :md="6">
          <div
            class="admin-tile"
            @click="goTo('/user-mgmt/roles')"
            v-perms="'rbac:roles:list'"
          >
            <el-icon class="admin-tile-icon" :size="22" color="#F56C6C"
              ><Medal
            /></el-icon>
            <div class="admin-tile-body">
              <div class="admin-tile-label">角色 / 权限</div>
              <div class="admin-tile-desc">RBAC 角色权限管理</div>
            </div>
          </div>
        </el-col>
      </el-row>
    </div>
  </div>
</template>

<style scoped>
/* ==================== 容器 ==================== */
.welcome-container {
  padding: 20px;
  max-width: 1600px;
  margin: 0 auto;
}

/* ==================== Hero ==================== */
.welcome-hero {
  position: relative;
  overflow: hidden;
  padding: 36px 32px;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 50%, #f093fb 100%);
  border-radius: 16px;
  color: #fff;
  margin-bottom: 24px;
  box-shadow: 0 8px 32px rgba(102, 126, 234, 0.3);
}

.hero-content {
  position: relative;
  z-index: 2;
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 16px;
}

.hero-greeting h1 {
  margin: 0 0 8px 0;
  font-size: 28px;
  font-weight: 700;
  letter-spacing: 0.5px;
}

.hero-subtitle {
  margin: 0;
  font-size: 14px;
  opacity: 0.85;
  letter-spacing: 0.3px;
}

.hero-meta {
  display: flex;
  align-items: center;
  gap: 12px;
  background: rgba(255, 255, 255, 0.15);
  backdrop-filter: blur(8px);
  padding: 6px 14px 6px 12px;
  border-radius: 24px;
  border: 1px solid rgba(255, 255, 255, 0.2);
}

.hero-meta-row {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  color: rgba(255, 255, 255, 0.92);
}

/* 装饰光圈 */
.hero-deco {
  position: absolute;
  border-radius: 50%;
  background: radial-gradient(
    circle,
    rgba(255, 255, 255, 0.25) 0%,
    transparent 70%
  );
  pointer-events: none;
  z-index: 1;
}
.hero-deco-1 {
  width: 240px;
  height: 240px;
  top: -80px;
  right: -60px;
}
.hero-deco-2 {
  width: 160px;
  height: 160px;
  bottom: -50px;
  left: 35%;
}

/* ==================== Hero 统计 ==================== */
.hero-stats {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 16px;
  margin-bottom: 32px;
}

.hero-stat {
  display: flex;
  align-items: center;
  padding: 20px 22px;
  background: var(--el-bg-color);
  border-radius: 14px;
  cursor: pointer;
  transition: all 0.3s ease;
  border: 1px solid var(--el-border-color-lighter);
}

.hero-stat:hover {
  transform: translateY(-4px);
  /* 2026-06-27 v2.3 dark mode 适配：阴影从纯黑改为半透明黑色变量
   *   - 浅色模式：rgba(0,0,0,0.12) 与原一致
   *   - 深色模式：var(--el-box-shadow) / var(--el-box-shadow-light) 自动适配
   *     由于纯黑阴影在深色背景上几乎不可见，此处叠加两层以兼容两种模式
   */
  box-shadow:
    0 12px 28px rgba(0, 0, 0, 0.12),
    0 0 0 1px rgba(64, 158, 255, 0.08);
  border-color: transparent;
}

.hero-stat-icon {
  width: 56px;
  height: 56px;
  border-radius: 12px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: white;
  margin-right: 16px;
  flex-shrink: 0;
}

.hero-stat-body {
  flex: 1;
  min-width: 0;
}

.hero-stat-value {
  font-size: 30px;
  font-weight: 700;
  color: var(--el-text-color-primary);
  line-height: 1.1;
}

.hero-stat-label {
  font-size: 13px;
  color: var(--el-text-color-secondary);
  margin-top: 4px;
}

/* ==================== Section 标题 ==================== */
.section-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 16px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  margin: 24px 0 16px 0;
  padding-left: 4px;
}

.section-title .el-icon {
  font-size: 18px;
  color: var(--el-color-primary);
}

/* ==================== 模块卡片 ==================== */
/* 2026-06-27 v2.2：行间 margin 由 .modules-section 控制（不放 .module-card 上）
 *   - .module-card 是单个卡（高度 100%），加 margin-bottom 会撑高 col，
 *     导致同行的卡片高度不一致
 *   - .modules-section 管整段外边距，把整个 el-row 当一行看待，
 *     多行之间通过 section margin 自然分开
 */
.modules-section .el-row{
  gap: 16px 0;
}
.actions-section,
.admin-section {
  margin-bottom: 8px;
}

.module-card {
  height: 100%;
  border-radius: 12px;
  transition: all 0.3s ease;
}

.module-card :deep(.el-card__header) {
  padding: 16px 18px;
  border-bottom: 1px solid var(--el-border-color-lighter);
}

.module-card-header {
  width: 100%;
}

.module-card-title {
  display: flex;
  align-items: center;
  gap: 12px;
}

.module-card-icon {
  width: 40px;
  height: 40px;
  border-radius: 10px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: white;
  flex-shrink: 0;
}

.module-card-name {
  font-size: 15px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  line-height: 1.2;
}

.module-card-desc {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-top: 2px;
}

.module-cards {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(110px, 1fr));
  gap: 10px;
}

.stat-tile {
  padding: 12px 14px;
  background: var(--el-fill-color-light);
  border-radius: 8px;
  cursor: pointer;
  transition: all 0.25s ease;
  border: 1px solid transparent;
  position: relative;
  overflow: hidden;
}

.stat-tile:hover {
  background: var(--el-color-primary-light-9);
  transform: translateY(-2px);
  border-color: var(--el-color-primary-light-5);
}

.stat-tile-highlight {
  /* 2026-06-27 v2.3 dark mode 适配：用 primary-light-* CSS 变量替代硬编码浅蓝
   *   - 浅色模式：var(--el-color-primary-light-9) ≈ #e6f0ff（接近原 #f0f7ff）
   *   - 深色模式：var(--el-color-primary-light-9) 自动转为深色主色微透明
   */
  background: linear-gradient(
    135deg,
    var(--el-color-primary-light-9) 0%,
    var(--el-color-primary-light-8) 100%
  );
  border-color: var(--el-color-primary-light-7);
}

.stat-tile-highlight:hover {
  background: linear-gradient(
    135deg,
    var(--el-color-primary-light-8) 0%,
    var(--el-color-primary-light-7) 100%
  );
}

.stat-tile-label {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-bottom: 6px;
}

.stat-tile-value {
  font-size: 22px;
  font-weight: 700;
  color: var(--el-text-color-primary);
  line-height: 1.1;
}

.stat-tile-highlight .stat-tile-value {
  font-size: 26px;
  color: var(--el-color-primary);
}

/* tone 强调色（用于状态点） */
.stat-tile-primary .stat-tile-value {
  color: var(--el-color-primary);
}
.stat-tile-success .stat-tile-value {
  color: var(--el-color-success);
}
.stat-tile-warning .stat-tile-value {
  color: var(--el-color-warning);
}
.stat-tile-info .stat-tile-value {
  color: var(--el-color-info);
}
.stat-tile-danger .stat-tile-value {
  color: var(--el-color-danger);
}

/* ==================== 快捷操作 ==================== */
.action-tile {
  display: flex;
  align-items: center;
  padding: 14px 16px;
  background: var(--el-bg-color);
  border-radius: 10px;
  cursor: pointer;
  transition: all 0.25s ease;
  margin-bottom: 16px;
  border: 1px solid var(--el-border-color-lighter);
}

.action-tile:hover {
  transform: translateY(-2px);
  /* 2026-06-27 v2.3 dark mode 适配：双层阴影兼容浅深色模式 */
  box-shadow:
    0 6px 18px rgba(0, 0, 0, 0.08),
    0 0 0 1px rgba(64, 158, 255, 0.06);
  border-color: transparent;
}

.action-tile-icon {
  width: 40px;
  height: 40px;
  border-radius: 8px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: white;
  margin-right: 12px;
  flex-shrink: 0;
}

.action-tile-body {
  flex: 1;
  min-width: 0;
  overflow: hidden;
}

.action-tile-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--el-text-color-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.action-tile-desc {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-top: 2px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* ==================== 管理员视图 ==================== */
.admin-tile {
  display: flex;
  align-items: center;
  padding: 16px 18px;
  background: var(--el-bg-color);
  border-radius: 10px;
  cursor: pointer;
  transition: all 0.25s ease;
  margin-bottom: 16px;
  border: 1px dashed var(--el-color-warning-light-7);
}

.admin-tile:hover {
  transform: translateY(-2px);
  box-shadow: 0 6px 18px rgba(230, 162, 60, 0.18);
  border-style: solid;
}

.admin-tile-icon {
  margin-right: 12px;
  flex-shrink: 0;
}

.admin-tile-body {
  flex: 1;
  min-width: 0;
}

.admin-tile-label {
  font-size: 14px;
  font-weight: 600;
  color: var(--el-text-color-primary);
}

.admin-tile-desc {
  font-size: 12px;
  color: var(--el-text-color-secondary);
  margin-top: 2px;
}

/* ==================== 响应式 ==================== */
@media (max-width: 768px) {
  .welcome-hero {
    padding: 24px 20px;
  }
  .hero-content {
    flex-direction: column;
    align-items: flex-start;
  }
  .hero-greeting h1 {
    font-size: 22px;
  }
  .hero-stats {
    grid-template-columns: 1fr 1fr;
  }
  .hero-stat-value {
    font-size: 24px;
  }
}
</style>