import { computed } from "vue";
import { useUserStore } from "@/store/modules/user";

/**
 * 抽离"当前用户是否为 admin"判断。
 * 用法：const isAdmin = useIsAdmin();
 * 替代到处写 `userStore.username === "admin"`。
 */
export function useIsAdmin() {
  const userStore = useUserStore();
  return computed(() => userStore.username === "admin");
}
