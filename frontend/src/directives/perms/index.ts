import { hasPerms } from "@/utils/auth";
import type { Directive, DirectiveBinding } from "vue";

export const perms: Directive = {
  mounted(el: HTMLElement, binding: DirectiveBinding<string | Array<string>>) {
    const { value } = binding;
    if (value) {
      if (!hasPerms(value)) {
        // 先尝试 el.parentNode（普通元素）
        if (el.parentNode) {
          el.parentNode.removeChild(el);
        } else {
          // 组件场景：el 可能没有 parentNode，尝试 parentElement
          el.parentElement?.removeChild(el);
        }
      }
    } else {
      throw new Error(
        "[Directive: perms]: need perms! Like v-perms=\"['btn.add','btn.edit']\""
      );
    }
  }
};
