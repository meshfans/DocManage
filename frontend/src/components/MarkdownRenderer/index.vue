<script setup lang="ts">
/**
 * MarkdownRenderer - 安全的 Markdown 渲染组件
 * 先转义 HTML，再做 Markdown 转换，避免 XSS
 */

const props = defineProps<{
  content: string;
}>();

function formatMarkdown(text: string): string {
  if (!text) return "";
  // 1. 先 HTML 转义（避免 XSS）
  let html = text
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;");
  // 2. 再做 Markdown 转换（在转义后的安全字符串上操作）
  html = html
    // 粗体 **text**
    .replace(/\*\*(.+?)\*\*/g, "<strong>$1</strong>")
    // 斜体 *text*（排除已处理的粗体内部）
    .replace(/(?<!\*)\*(?!\*\*)(.+?)(?<!\*)\*(?!\*)/g, "<em>$1</em>")
    // 行内代码 `code`
    .replace(/`([^`]+)`/g, "<code>$1</code>")
    // 标题 ### / ## / #
    .replace(/^### (.+)$/gm, "<h3>$1</h3>")
    .replace(/^## (.+)$/gm, "<h2>$1</h2>")
    .replace(/^# (.+)$/gm, "<h1>$1</h1>")
    // 无序列表 - 或 *
    .replace(/^[\-\*] (.+)$/gm, "<li>$1</li>")
    // 有序列表 1.
    .replace(/^\d+\. (.+)$/gm, "<li>$1</li>")
    // 换行
    .replace(/\n/g, "<br>");
  return html;
}
</script>

<template>
  <span v-html="formatMarkdown(props.content)" />
</template>
