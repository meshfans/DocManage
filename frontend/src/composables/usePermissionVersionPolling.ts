import { ref, onMounted, onUnmounted } from "vue";
import { ElNotification } from "element-plus";
import { getPermissionVersion } from "@/api/routes";
import { getPermVersion, setPermVersion } from "@/utils/auth";

/**
 * RBAC 权限版本轮询（Plan B：60s 周期）
 *
 * 流程：
 * 1. 每 60s 调 GET /api/rbac/permission-version
 * 2. 与 localStorage 缓存的 version 对比
 * 3. 不一致 → 弹 ElNotification 提示用户"权限已变更，点击立即刷新"
 * 4. 用户点击"立即刷新" → 调用 reloadCallback（默认 location.reload()）
 *
 * 使用：
 *   import { usePermissionVersionPolling } from "@/composables/usePermissionVersionPolling"
 *   usePermissionVersionPolling()
 *   // 或自定义 reload 行为：usePermissionVersionPolling({ reloadCallback: () => router.replace('/login') })
 */
export function usePermissionVersionPolling(options?: {
  intervalMs?: number;
  reloadCallback?: () => void;
}) {
  // 2026-06-28 调整默认：60s → 5min。
  // 理由：99% 轮询都是无效的（admin 不常改权限），5min 已能覆盖业务可接受的"权限变更感知延迟"。
  // 调用方可通过 options.intervalMs 覆盖（如 test 用 1000ms）。
  const intervalMs = options?.intervalMs ?? 5 * 60_000;
  const reloadCallback = options?.reloadCallback ?? (() => location.reload());
  const timer = ref<number | null>(null);
  const firstCheckTimer = ref<number | null>(null);
  const isPolling = ref(false);

  async function checkVersion() {
    if (isPolling.value) return;
    isPolling.value = true;
    try {
      const localVer = getPermVersion();
      // 首次登录前跳过（localStorage 没值）
      if (localVer == null) return;

      const res: any = await getPermissionVersion();
      const serverVer = res?.data?.version;
      if (!serverVer) return;

      if (serverVer !== localVer) {
        // 弹出通知（不自动 reload，等用户确认）
        // 使用 ElMessageBox.confirm 保证点了"立即刷新"后通知自动关闭
        // 同时给一个"忽略"按钮（60s 后下次轮询还会再弹）
        ElNotification({
          title: "您的权限被管理员更新了",
          message: "点击通知立即刷新页面，新权限将生效（否则下次轮询 60s 后会再提醒）",
          type: "warning",
          duration: 0, // 不自动关闭，等用户主动操作
          position: "bottom-right",
          customClass: "perm-version-notification",
          // 整个通知 div 可点；点完调 reloadCallback
          onClick: async () => {
            // 主动关闭所有通知（避免点完留在页面上）
            ElNotification.closeAll();
            // 2026-06-25 P1-7.1 修复：reload 前先调 /api/user/info 刷新 localStorage，
            // 避免 reload 后旧 roles 仍能匹配 meta.roles 错误显示菜单。
            try {
              const { useUserStoreHook } = await import("@/store/modules/user");
              await useUserStoreHook().refreshFromApi();
            } catch (e) {
              console.debug("[perm-version polling] refreshFromApi failed", e);
            }
            reloadCallback();
          }
        });
        // 同步更新 localStorage（避免重复弹）
        setPermVersion(serverVer);
      }
    } catch (e) {
      // 静默失败（轮询不应影响主流程）
      console.debug("[perm-version polling] check failed", e);
    } finally {
      isPolling.value = false;
    }
  }

  function start() {
    if (timer.value !== null) return;
    timer.value = window.setInterval(checkVersion, intervalMs);
    // 2026-06-28 调整：从 30s 延长到 60s，避免 mount 后立即发请求，与 layout 启动节奏对齐。
    firstCheckTimer.value = window.setTimeout(checkVersion, 60_000);
  }

  function stop() {
    if (timer.value !== null) {
      clearInterval(timer.value);
      timer.value = null;
    }
    if (firstCheckTimer.value !== null) {
      clearTimeout(firstCheckTimer.value);
      firstCheckTimer.value = null;
    }
  }

  onMounted(start);
  onUnmounted(stop);

  return { start, stop, checkVersion };
}