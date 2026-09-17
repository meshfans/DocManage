<script setup lang="ts">
import { ref, reactive, computed, onMounted, watch } from "vue";
import { useRoute } from "vue-router";
import { useNav } from "@/layout/hooks/useNav";
import LaySearch from "../lay-search/index.vue";
import LayNotice from "../lay-notice/index.vue";
import LayNavMix from "../lay-sidebar/NavMix.vue";
import LaySidebarFullScreen from "../lay-sidebar/components/SidebarFullScreen.vue";
import LaySidebarBreadCrumb from "../lay-sidebar/components/SidebarBreadCrumb.vue";
import LaySidebarTopCollapse from "../lay-sidebar/components/SidebarTopCollapse.vue";

import LogoutCircleRLine from "~icons/ri/logout-circle-r-line";
import LockPasswordLine from "~icons/ri/lock-password-line";
import Setting from "~icons/ri/settings-3-line";
import CloseLine from "~icons/ri/close-line";
import RefreshLine from "~icons/ri/refresh-line";
import EarthLine from "~icons/ri/earth-line";
import EyeLine from "~icons/ri/eye-line";
import EyeOffLine from "~icons/ri/eye-off-line";
// 后退
import ArrowLeftLine from "~icons/ri/arrow-left-line";
// 头像
import UserFill from "~icons/ri/user-fill";

import { ElMessage, type FormInstance, type FormRules } from "element-plus";
import { changePassword, getUserInfo } from "@/api/user";
import { uploadAvatar } from "@/api/user_extended";
import { PASSWORD_MIN, PASSWORD_MAX, validatePasswordStrength } from "@/utils/password";
import { isDocClient } from "@/utils/isDocClient";
import { bridgeCall } from "@/utils/docClientBridge";
import { refreshNavAvatar } from "@/layout/hooks/useNav";
import AvatarCropper from "@/components/AvatarCropper/index.vue";

const {
  layout,
  device,
  logout,
  onPanel,
  pureApp,
  username,
  userAvatar,
  avatarsStyle,
  toggleSideBar
} = useNav();

const route = useRoute();

// ==================== 修改密码 ====================
// 共享密码策略：8-18 位 + 至少 2 种字符（数字/字母/符号），与登录页、注册页统一
const pwdDialogVisible = ref(false);
const pwdFormRef = ref<FormInstance>();
const pwdSubmitting = ref(false);

const pwdForm = reactive({
  old_password: "",
  new_password: "",
  confirm_password: ""
});

const pwdRules: FormRules = {
  old_password: [{ required: true, message: "请输入旧密码", trigger: "blur" }],
  new_password: [
    { required: true, message: "请输入新密码", trigger: "blur" },
    {
      validator: (_rule, value, callback) => {
        if (!value) return callback();
        const err = validatePasswordStrength(value);
        if (err) callback(new Error(err));
        else callback();
      },
      trigger: "blur"
    }
  ],
  confirm_password: [
    { required: true, message: "请再次输入新密码", trigger: "blur" },
    {
      validator: (_rule, value, callback) => {
        if (!value) return callback();
        if (value !== pwdForm.new_password) {
          callback(new Error("两次输入的密码不一致"));
        } else {
          callback();
        }
      },
      trigger: "blur"
    }
  ]
};

const openChangePassword = () => {
  pwdDialogVisible.value = true;
  pwdForm.old_password = "";
  pwdForm.new_password = "";
  pwdForm.confirm_password = "";
};

const closeChangePassword = () => {
  pwdDialogVisible.value = false;
};

// ==================== 修改头像 ====================
const avatarUploading = ref(false);
const currentUserId = ref<number | null>(null);
const avatarCropperRef = ref();

// 点击"修改头像" - 直接打开文件选择器（弹出系统文件选择）
const openChangeAvatar = async () => {
  // 确保拿到 userId（裁剪完成后需要）
  if (!currentUserId.value) {
    try {
      const res: any = await getUserInfo();
      if (res?.success && res.data?.id) {
        currentUserId.value = res.data.id;
      }
    } catch (e) {
      console.error("[avatar] failed to get user info", e);
    }
  }
  // 触发 AvatarCropper 内置的文件选择
  avatarCropperRef.value?.open();
};

// 裁剪确认后上传
const handleCropConfirm = async (blob: Blob) => {
  if (!currentUserId.value) {
    try {
      const res: any = await getUserInfo();
      if (res?.success && res.data?.id) {
        currentUserId.value = res.data.id;
      } else {
        ElMessage.error("获取用户信息失败");
        return;
      }
    } catch (e) {
      ElMessage.error("获取用户信息失败");
      return;
    }
  }

  avatarUploading.value = true;
  try {
    // blob 转 File
    const file = new File([blob], "avatar.webp", { type: "image/webp" });
    const res: any = await uploadAvatar(currentUserId.value, file);
    if (res?.success) {
      // 不写 store，直接调用 GET /api/users/:id/avatar 拉取最新头像 blob URL
      await refreshNavAvatar();
      ElMessage.success("头像上传成功");
    } else {
      ElMessage.error(res?.message || "头像上传失败");
    }
  } catch (e: any) {
    ElMessage.error(e?.message || "头像上传失败");
  } finally {
    avatarUploading.value = false;
  }
};

const submitChangePassword = async () => {
  if (!pwdFormRef.value) return;
  try {
    await pwdFormRef.value.validate();
  } catch {
    return;
  }
  pwdSubmitting.value = true;
  try {
    const res: any = await changePassword({
      old_password: pwdForm.old_password,
      new_password: pwdForm.new_password
    });
    if (res?.success) {
      ElMessage.success(res?.message || "密码修改成功");
      pwdDialogVisible.value = false;
    } else {
      ElMessage.error(res?.message || "密码修改失败");
    }
  } catch (e: any) {
    ElMessage.error(e?.message || "密码修改失败");
  } finally {
    pwdSubmitting.value = false;
  }
};

// ==================== 重置 DocManage Client ====================
// 只在桌面客户端内可见（通过 navigator.userAgent 识别）
// 点击后调用客户端 HTTP 桥 /reset 端点 → 清 localStorage(docmanage_url) + 回到输入界面
const showClientButton = computed(() => isDocClient());

const onDrag = (e: MouseEvent) => {
  // 只响应左键
  if (e.button !== 0) return;
  // 拖动区是专用 div（.navbar-drag），不会跟按钮冲突，不需要闪避
  bridgeCall({
    operation: "drag",
    silent: true,
    failed: (err) => console.warn("[drag] failed:", err)
  });
};

const onClose = () => {
  bridgeCall({
    operation: "close",
    silent: true,
    failed: (err) => console.warn("[close] failed:", err)
  });
};

// ==================== 地址栏（仅桌面客户端）====================
// 显示当前代理的 static_target，让用户知道"我在访问谁"
// 默认隐藏（点击 navbar 的眼睛图标切换显示）
// 配合 reset 按钮：调 reset op → navigate 回 ?mode=settings → 重新输入 URL
const showUrlBar = computed(() => isDocClient());
const urlBarVisible = ref(false); // 默认隐藏，眼睛图标切换
const proxyUrl = ref<string>("");

function toggleUrlBar() {
  urlBarVisible.value = !urlBarVisible.value;
  if (urlBarVisible.value) refreshProxyUrl();
}

function refreshProxyUrl() {
  if (!isDocClient()) return;
  bridgeCall({
    operation: "get_proxy_targets",
    silent: true,
    success: async (res) => {
      try {
        const raw = await res.text();
        if (!raw) return;
        const obj = JSON.parse(raw);
        proxyUrl.value = obj?.static || "";
      } catch (e) {
        console.warn("[navbar] parse get_proxy_targets failed:", e);
      }
    },
    failed: (err) => console.warn("[navbar] get_proxy_targets failed:", err)
  });
}

function onResetTarget() {
  bridgeCall({
    operation: "reset",
    silent: true,
    success: () => {
      // reset 后 navigate 到本地 ?mode=settings，App.vue 清 config；
      // 这里稍等一会儿再刷新（WebView navigate 是异步）
      setTimeout(refreshProxyUrl, 300);
    },
    failed: (err) => console.warn("[reset] failed:", err)
  });
}

onMounted(() => {
  // 默认隐藏，不主动拉取；用户点击眼睛图标才显示并拉取
});

// ==================== 后退 / 前进 ====================
// 直接调用浏览器原生 `window.history.back()` / `forward()`：
//   - vue-router 4 默认 HTML5 history 模式，router.push 内部用 history.pushState
//   - 所以浏览器历史栈和 vue-router 完全同步，无需自己维护
//   - 原生 API 天然支持 query/hash 还原（不会丢失）
//   - 普通浏览器和桌面客户端 WebView 都支持
//
// 优势（对比自建栈方案）：
//   - 代码从 60 行 → 10 行，无 watch / 标志位 / 栈管理
//   - 无"前进后退死循环"等状态同步 bug
//   - 用户在地址栏直接输入 URL 也能正确后退（之前的栈方案在这种情况下会"假死"）
//   - 跨标签页/多应用统一行为
//
// 简化（用户反馈）：仅保留后退，去掉前进按钮。
// 原因：浏览器没有可靠的 API 知道"是否还能前进"，强行用 canGoForward 会出现
//       "按钮可点但点了无效"的迷惑体验；用户要前进可用浏览器内置按钮（Cmd/Ctrl+→）
const canGoBack = ref(false);

function goBack() {
  if (!canGoBack.value) {
    ElMessage.info("已是第一页");
    return;
  }
  window.history.back();
}

// 监听路由变化，启用后退按钮
watch(
  () => route.fullPath,
  (newPath, oldPath) => {
    if (!oldPath) return; // 首次加载
    if (newPath === oldPath) return; // 同 fullPath 刷新

    // router.push 触发的跳转：能后退（pushState 把当前页加入历史栈）
    canGoBack.value = true;
  }
);
</script>

<template>
  <div class="navbar bg-[#fff] shadow-xs shadow-[rgba(0,21,41,0.08)]">
    <LaySidebarTopCollapse
      v-if="device === 'mobile'"
      class="hamburger-container"
      :is-active="pureApp.sidebar.opened"
      @toggleClick="toggleSideBar"
    />

    <LaySidebarBreadCrumb
      v-if="layout !== 'mix' && device !== 'mobile'"
      class="breadcrumb-container"
    />

    <LayNavMix v-if="layout === 'mix'" />

    <!-- 地址栏（仅 Tauri 客户端可见，默认隐藏，水平居中） -->
    <div
      v-if="showUrlBar && urlBarVisible"
      class="navbar-url"
      :title="proxyUrl"
    >
      <IconifyIconOffline :icon="EarthLine" class="url-icon" />
      <span class="url-text">{{ proxyUrl || "(未设置)" }}</span>
      <span
        class="url-reset navbar-bg-hover"
        title="重新输入 URL"
        @click.stop="onResetTarget"
      >
        <IconifyIconOffline :icon="RefreshLine" />
      </span>
    </div>

    <div v-if="layout === 'vertical'" class="vertical-header-right">
      <!-- 拖动区域 -->
      <div class="navbar-drag" @mousedown.stop="onDrag" v-if="showClientButton"></div>
      <!-- 后退按钮（仅 Tauri 客户端可见，参考 navbar-drag 的 v-if 条件） -->
      <div v-if="showClientButton" class="navbar-nav-buttons">
        <span
          class="nav-icon navbar-bg-hover"
          :class="{ 'is-disabled': !canGoBack }"
          title="后退"
          @click.stop="goBack"
        >
          <IconifyIconOffline :icon="ArrowLeftLine" />
        </span>
      </div>
      <!-- 菜单搜索 -->
      <LaySearch id="header-search" />
      <!-- 消息通知 -->
      <LayNotice id="header-notice" />
      <!-- 全屏 -->
      <LaySidebarFullScreen id="full-screen" />

      <!-- 显示/隐藏地址栏（仅 Tauri 客户端可见，关闭按钮前） -->
      <span
        v-if="showClientButton"
        class="set-icon navbar-bg-hover"
        :title="urlBarVisible ? '隐藏地址栏' : '显示地址栏'"
        @click.stop="toggleUrlBar"
      >
        <IconifyIconOffline :icon="urlBarVisible ? EyeOffLine : EyeLine" />
      </span>
      <!-- 关闭 -->
      <span
        v-if="showClientButton"
        class="set-icon navbar-bg-hover navbar-close"
        title="关闭 DocManage Client"
        @click.stop="onClose"
      >
        <IconifyIconOffline :icon="CloseLine" />
      </span>
      <!-- 退出登录 -->
      <el-dropdown trigger="click">
        <span class="el-dropdown-link navbar-bg-hover select-none">
          <img :src="userAvatar" :style="avatarsStyle" />
          <p v-if="username" class="dark:text-white">{{ username }}</p>
        </span>
        <template #dropdown>
          <el-dropdown-menu class="logout">
            <el-dropdown-item @click="openChangeAvatar">
              <IconifyIconOffline
                :icon="UserFill"
                style="margin: 5px"
              />
              修改头像
            </el-dropdown-item>
            <el-dropdown-item @click="openChangePassword">
              <IconifyIconOffline
                :icon="LockPasswordLine"
                style="margin: 5px"
              />
              修改密码
            </el-dropdown-item>
            <el-dropdown-item divided @click="logout">
              <IconifyIconOffline
                :icon="LogoutCircleRLine"
                style="margin: 5px"
              />
              退出系统
            </el-dropdown-item>
          </el-dropdown-menu>
        </template>
      </el-dropdown>

      <!-- 修改密码弹窗 -->
      <el-dialog
        v-model="pwdDialogVisible"
        title="修改密码"
        width="420px"
        :close-on-click-modal="false"
        @close="closeChangePassword"
      >
        <el-form
          ref="pwdFormRef"
          :model="pwdForm"
          :rules="pwdRules"
          label-width="84px"
        >
          <el-form-item label="用户名">
            <span>{{ username }}</span>
          </el-form-item>
          <el-form-item label="旧密码" prop="old_password">
            <el-input
              v-model="pwdForm.old_password"
              type="password"
              show-password
              placeholder="请输入当前密码"
              autocomplete="current-password"
            />
          </el-form-item>
          <el-form-item label="新密码" prop="new_password">
            <el-input
              v-model="pwdForm.new_password"
              type="password"
              show-password
              :placeholder="`${PASSWORD_MIN}-${PASSWORD_MAX} 位，至少含数字/字母/符号中的 2 种`"
              autocomplete="new-password"
            />
          </el-form-item>
          <el-form-item label="确认密码" prop="confirm_password">
            <el-input
              v-model="pwdForm.confirm_password"
              type="password"
              show-password
              placeholder="再次输入新密码"
              autocomplete="new-password"
            />
          </el-form-item>
        </el-form>
        <template #footer>
          <el-button @click="closeChangePassword">取消</el-button>
          <el-button
            type="primary"
            :loading="pwdSubmitting"
            @click="submitChangePassword"
          >
            确认修改
          </el-button>
        </template>
      </el-dialog>

      <!-- 头像裁剪弹窗（自包含文件选择 + 裁剪 + 上传确认） -->
      <AvatarCropper
        ref="avatarCropperRef"
        @confirm="handleCropConfirm"
      />
      <span
          class="set-icon navbar-bg-hover"
          title="打开系统配置"
          @click="onPanel"
        >
          <IconifyIconOffline :icon="Setting" />
        </span>
    </div>
  </div>
</template>

<style lang="scss" scoped>
.navbar {
  width: 100%;
  height: 48px;
  overflow: hidden;
  position: relative;  // 让 navbar-url 用 absolute 居中
  .navbar-drag{flex: 1;height:100%;display: flex !important;}
  .navbar-drag:hover{cursor: move;}
  // DocManage Client 关闭按钮 hover 红色（仅在桌面客户端内显示）
  .navbar-close:hover {
    background: #e81123 !important;
    color: #fff !important;
  }

  .hamburger-container {
    float: left;
    height: 100%;
    line-height: 48px;
    cursor: pointer;
  }

  .vertical-header-right {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    min-width: 280px;
    height: 48px;
    color: #000000d9;

    .el-dropdown-link {
      display: flex;
      align-items: center;
      justify-content: space-around;
      height: 48px;
      padding: 10px;
      color: #000000d9;
      cursor: pointer;

      p {
        font-size: 14px;
      }

      img {
        width: 22px;
        height: 22px;
        border-radius: 50%;
      }
    }
  }

  .breadcrumb-container {
    float: left;
    margin-left: 16px;
  }
}

.logout {
  width: 120px;

  ::v-deep(.el-dropdown-menu__item) {
    display: inline-flex;
    flex-wrap: wrap;
    min-width: 100%;
  }
}

/* ===== 后退 / 前进 按钮（地址栏左边） ===== */
.navbar-nav-buttons {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  margin: 0 8px;
  flex-shrink: 0;

  .nav-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 28px;
    height: 28px;
    border-radius: 50%;
    cursor: pointer;
    color: var(--el-text-color-regular, #606266);
    transition: background 160ms ease, color 160ms ease, transform 160ms ease;
    user-select: none;

    &:hover {
      background: var(--el-fill-color, #f0f2f5);
      color: var(--el-color-primary, #409eff);
    }

    &:active {
      transform: scale(0.92);
    }

    &.is-disabled {
      color: var(--el-text-color-placeholder, #a8abb2);
      cursor: not-allowed;
      opacity: 0.5;

      &:hover {
        background: transparent;
        color: var(--el-text-color-placeholder, #a8abb2);
        transform: none;
      }
    }

    svg {
      width: 16px;
      height: 16px;
    }
  }
}

/* ===== 地址栏（仅 Tauri 客户端，默认隐藏，水平居中） ===== */
.navbar-url {
  // 水平居中：用 absolute + left:50% + transform
  // 不占 navbar 文档流，避免挤压左侧 breadcrumb / 右侧按钮
  position: fixed;
  left: calc(50%  - 120px);
  margin: 8px 0;
  z-index: 10;

  display: flex;
  align-items: center;
  height: 32px;
  max-width: 240px;
  padding: 0 10px;
  background: var(--el-fill-color-light, #f5f7fa);
  border: 1px solid var(--el-border-color-lighter, #ebeef5);
  border-radius: 16px;
  font-size: 12px;
  color: var(--el-text-color-regular, #606266);
  gap: 6px;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.06);

  .url-icon {
    font-size: 14px;
    flex-shrink: 0;
    color: var(--el-color-primary);
  }

  .url-text {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
  }

  .url-reset {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 22px;
    height: 22px;
    border-radius: 50%;
    cursor: pointer;
    flex-shrink: 0;
    color: var(--el-text-color-secondary);
    transition: all 0.2s;

    &:hover {
      color: var(--el-color-primary);
      background: var(--el-color-primary-light-9, #ecf5ff);
    }
  }
}

/* 修改头像弹窗样式已移除 - 直接使用 AvatarCropper */
</style>
