import { h, defineComponent } from "vue";
import { Icon as IconifyIcon, addIcon } from "@iconify/vue";

// Iconify Icon在Vue里本地使用（用于内网环境）
export default defineComponent({
  name: "IconifyIconOffline",
  components: { IconifyIcon },
  props: {
    icon: {
      default: null
    }
  },
  render() {
    const attrs = this.$attrs;
    const icon = this.icon;

    // 字符串 icon：走 IconifyIcon 组件渲染（已通过 addIcon 注册到本地缓存）
    if (typeof icon === "string") {
      return h(
        IconifyIcon,
        {
          icon,
          "aria-hidden": false,
          style: attrs?.style
            ? Object.assign(attrs.style, { outline: "none" })
            : { outline: "none" },
          ...attrs
        },
        { default: () => [] }
      );
    }

    // Vue 组件对象：直接渲染（由 unplugin-icons 按需导入）
    if (typeof icon === "object" && icon !== null && typeof (icon as any).render === "function") {
      return h(
        icon as any,
        {
          "aria-hidden": false,
          style: attrs?.style
            ? Object.assign(attrs.style, { outline: "none" })
            : { outline: "none" },
          ...attrs
        },
        { default: () => [] }
      );
    }

    // iconify 图标对象（有 body 字段）：注册到本地缓存后渲染
    if (typeof icon === "object" && icon !== null && (icon as any).body) {
      const name = (icon as any).name || "";
      if (name) {
        addIcon(name, icon as any);
      }
      return h(
        IconifyIcon,
        {
          icon: name,
          "aria-hidden": false,
          style: attrs?.style
            ? Object.assign(attrs.style, { outline: "none" })
            : { outline: "none" },
          ...attrs
        },
        { default: () => [] }
      );
    }

    return null;
  }
});
