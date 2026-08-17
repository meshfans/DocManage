<script setup lang="ts">
/**
 * MediaDetail 详情侧抽屉
 * - 元数据 + 3 哈希完整展示 + tags / bindings 编辑
 * - 校验 / 下载 / 软删 操作
 */
import { ref, watch, computed, onBeforeUnmount } from "vue";
import { ElMessage, ElMessageBox } from "element-plus";
import {
  CircleClose,
  Document,
  RefreshLeft,
  Refresh,
  Check,
  Close,
  Plus
} from "@element-plus/icons-vue";
import {
  getMedia,
  updateMedia,
  restoreMedia,
  verifyMedia,
  fetchMediaFile,
  fetchMediaThumb
} from "@/api/media";
import { getThirdPartyContract } from "@/api/third_party";
import type {
  Media,
  MediaHashes,
  MediaBinding,
  MediaTag,
  VerifyMediaResult
} from "@/api/media";

/** ObjectURL + revoke 句柄（用于照片/录像预览） */
interface MediaBlobUrl {
  url: string;
  revoke: () => void;
}

interface PresetTag {
  name: string;
  type: "primary" | "success" | "warning" | "danger" | "info";
}

const props = defineProps<{
  mediaId: number | null;
}>();

const emit = defineEmits<{
  close: [];
  updated: [];
  deleted: [];
}>();

const loading = ref(false);
const detail = ref<Media | null>(null);
const verifyResult = ref<VerifyMediaResult["data"] | null>(null);
const verifying = ref(false);
// 详情预览图：原图（photo/video）走 /api/media/:id/file，缩略图（audio）走 /api/media/:id/thumb
const previewUrl = ref<string>("");
const previewLoading = ref(false);

// 重新加载详情
async function load() {
  if (!props.mediaId) {
    detail.value = null;
    return;
  }
  loading.value = true;
  try {
    const res = await getMedia(props.mediaId);
    if (res.success) detail.value = res.data;
  } finally {
    loading.value = false;
  }
}

watch(() => props.mediaId, load, { immediate: true });

// 详情变更时拉取原图/缩略图 URL（不直接用 file_path，因为后端给的是相对路径）
watch(
  () => detail.value?.id,
  async id => {
    if (!id || !detail.value) {
      previewUrl.value = "";
      return;
    }
    previewLoading.value = true;
    try {
      // 先释放上一个 ObjectURL（避免内存泄漏）
      if (currentRevoke) {
        currentRevoke();
        currentRevoke = null;
      }
      let blobUrl: MediaBlobUrl;
      if (detail.value.type === "photo" || detail.value.type === "video") {
        // photo/video 走 /file 路由（直接流式）
        blobUrl = await fetchMediaFile(id);
      } else {
        // audio 走 /thumb 路由（后端兜底返回默认音频图标或空）
        blobUrl = await fetchMediaThumb(id);
      }
      previewUrl.value = blobUrl.url;
      currentRevoke = blobUrl.revoke;
    } catch {
      previewUrl.value = "";
    } finally {
      previewLoading.value = false;
    }
  },
  { immediate: true }
);

// 当前 ObjectURL 的 revoke 句柄（用于在切换/卸载时释放）
let currentRevoke: (() => void) | null = null;

// 组件卸载时释放（防止内存泄漏）
onBeforeUnmount(() => {
  if (currentRevoke) {
    currentRevoke();
    currentRevoke = null;
  }
  previewUrl.value = "";
});

// 编辑模式
const editMode = ref(false);
const editName = ref("");
const editRemark = ref("");

function enterEdit() {
  if (!detail.value) return;
  editName.value = detail.value.name;
  editRemark.value = detail.value.remark ?? "";
  editMode.value = true;
}
async function saveEdit() {
  if (!detail.value) return;
  const res = await updateMedia(detail.value.id, {
    name: editName.value,
    remark: editRemark.value
  });
  if (res.success) {
    ElMessage.success("已保存");
    editMode.value = false;
    await load();
    emit("updated");
  }
}

// onDelete / onDownload 已删（详情底部按钮精简，列表 hover 操作已够用）

const isDeleted = computed(
  () => detail.value?.status === "deleted"
);

async function onRestore() {
  if (!detail.value) return;
  try {
    await ElMessageBox.confirm(
      `确定恢复「${detail.value.name}」？`,
      "恢复",
      {
        type: "info",
        confirmButtonText: "恢复",
        cancelButtonText: "取消",
        customClass: "media-confirm-msg"
      }
    );
    const res = await restoreMedia(detail.value.id);
    if (res.success) {
      ElMessage.success("已恢复");
      emit("updated");
      emit("close");
    }
  } catch {
    /* 用户取消 */
  }
}

async function onVerify() {
  if (!detail.value) return;
  verifying.value = true;
  try {
    const res = await verifyMedia(detail.value.id);
    if (res.success) verifyResult.value = res.data;
  } finally {
    verifying.value = false;
  }
}

function copy(text: string) {
  navigator.clipboard.writeText(text).then(
    () => ElMessage.success("已复制"),
    () => ElMessage.error("复制失败")
  );
}

function formatTime(ts: number): string {
  return new Date(ts * 1000).toLocaleString("zh-CN");
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}

const bindings = computed<MediaBinding[]>(() => {
  if (!detail.value) return [];
  return detail.value.bindings ?? [];
});

// ==================== 绑定编辑 ====================
// 类型字典：仅保留文档绑定
//   - third_party   → 关联到 third_party_contract 表
const BINDING_TYPES: { value: string; label: string }[] = [
  { value: "third_party", label: "文档" }
];
// 角色字典：精简为文档场景常用角色
const BINDING_ROLES: { value: string; label: string }[] = [
  { value: "attachment", label: "附件" },
  { value: "evidence",   label: "证据" },
  { value: "original",   label: "原件" },
  { value: "copy",       label: "副本" }
];

// local 副本：用于"暂存"未保存的绑定修改，避免每次改动都打后端
const localBindings = ref<MediaBinding[]>([]);
const bindingForm = ref<{
  target_type: "third_party";
  target_id: number;
  role: "attachment" | "evidence" | "original" | "copy";
  remark: string;
}>({
  target_type: "third_party",
  target_id: 0,
  role: "attachment",
  remark: ""
});
const bindingFormError = ref("");
const savingBindings = ref(false);
const bindingDirty = computed(() => {
  if (!detail.value) return false;
  const orig = detail.value.bindings ?? [];
  if (orig.length !== localBindings.value.length) return true;
  return JSON.stringify(orig) !== JSON.stringify(localBindings.value);
});

// 同步 detail → local（仅在 detail.id 变化时拉取最新）
watch(
  () => detail.value?.id,
  () => {
    if (detail.value) {
      localBindings.value = JSON.parse(JSON.stringify(detail.value.bindings ?? []));
    } else {
      localBindings.value = [];
    }
  },
  { immediate: true }
);

async function addBindingLocal() {
  bindingFormError.value = "";
  const { target_type, target_id, role, remark } = bindingForm.value;
  if (!target_type) {
    bindingFormError.value = "请选择类型";
    return;
  }
  if (!Number.isFinite(target_id) || target_id <= 0 || !Number.isInteger(target_id)) {
    bindingFormError.value = "目标 ID 必须为正整数";
    return;
  }
  if (!role) {
    bindingFormError.value = "请选择角色";
    return;
  }
  // 同 (type, id, role) 去重
  const exists = localBindings.value.some(
    b => b.target_type === target_type && b.target_id === target_id && b.role === role
  );
  if (exists) {
    bindingFormError.value = "已存在相同的绑定";
    return;
  }

  // 校验目标ID是否存在（仅支持文档）
  try {
    let targetName = "";
    if (target_type === "third_party") {
      // 文档 → third_party_contract表
      const res = await getThirdPartyContract(target_id);
      if (!res.success || !res.data) {
        bindingFormError.value = `文档 #${target_id} 不存在`;
        return;
      }
      targetName = res.data.title ?? `文档 #${target_id}`;
    }

    // 弹出确认框
    await ElMessageBox.confirm(
      `确定绑定到「${targetName}」（#${target_id}）？`,
      "绑定确认",
      {
        type: "info",
        confirmButtonText: "确认绑定",
        cancelButtonText: "取消",
        customClass: "media-confirm-msg"
      }
    );

    // 用户确认后添加到本地列表
    localBindings.value.push({
      target_type,
      target_id,
      role,
      remark: remark.trim()
    });
    // 重置表单（保留类型便于连续添加）
    bindingForm.value.target_id = 0;
    bindingForm.value.remark = "";
  } catch (e: any) {
    // 用户取消
    if (e !== "cancel" && e !== "close") {
      bindingFormError.value = "校验失败：" + (e?.message ?? e);
    }
  }
}

function removeBindingLocal(idx: number) {
  localBindings.value.splice(idx, 1);
}

function resetBindingsLocal() {
  if (!detail.value) return;
  localBindings.value = JSON.parse(JSON.stringify(detail.value.bindings ?? []));
  bindingFormError.value = "";
}

async function saveBindings() {
  if (!detail.value) return;
  savingBindings.value = true;
  try {
    const res = await updateMedia(detail.value.id, {
      bindings: localBindings.value
    });
    if (res.success) {
      ElMessage.success("绑定已保存");
      await load();
      emit("updated");
    } else {
      ElMessage.error("保存失败：" + (res.message ?? "未知错误"));
    }
  } catch (err: any) {
    ElMessage.error("保存失败：" + (err?.message ?? err));
  } finally {
    savingBindings.value = false;
  }
}

// 类型 / 角色字典（用于显示）
const typeLabelMap: Record<string, string> = Object.fromEntries(
  BINDING_TYPES.map(t => [t.value, t.label])
);
const roleLabelMap: Record<string, string> = Object.fromEntries(
  BINDING_ROLES.map(r => [r.value, r.label])
);

// ==================== 标签编辑 ====================
// 预制 tag 词典：用户可在 UI 上一键添加，也可在输入框自定义
// 顺序：高频在前；颜色按 el-tag 类型（success/info/warning/danger/primary）轮换
const PRESET_TAGS: PresetTag[] = [
  { name: "身份证",      type: "primary" },
  { name: "营业执照",    type: "success" },
  { name: "人像",        type: "info"    },
  { name: "签名照",      type: "warning" },
  { name: "证件照",      type: "primary" },
  { name: "合同页",      type: "success" },
  { name: "印章",        type: "danger"  },
  { name: "公章",        type: "danger"  },
  { name: "现场照片",    type: "info"    },
  { name: "票据",        type: "warning" },
  { name: "手写",        type: "info"    },
  { name: "客户档案",    type: "primary" }
];
// 名字 → type（用于已添加的 tag 渲染颜色）
const PRESET_TYPE_MAP: Record<string, string> = Object.fromEntries(
  PRESET_TAGS.map(t => [t.name, t.type])
);

const tagInput = ref("");
const savingTags = ref(false);
const currentTags = computed<MediaTag[]>(() => detail.value?.tags ?? []);

const presetRemaining = computed(() =>
  PRESET_TAGS.filter(p => !currentTags.value.some(t => t.name === p.name))
);

function addTag(name: string) {
  const v = (name ?? "").trim();
  if (!v) return;
  if (!detail.value) return;
  if (currentTags.value.some(t => t.name === v)) {
    ElMessage.warning(`标签「${v}」已存在`);
    return;
  }
  if (!detail.value.tags) detail.value.tags = [];
  // 写 color：预制 tag 用 el-tag type；自定义 tag 用 "info" 兜底
  // 后端 omitempty：自定义 tag 的 color 可省略（这里统一设值，逻辑更清晰）
  const color = PRESET_TYPE_MAP[v] || "info";
  detail.value.tags.push({ name: v, color });
}

function removeTag(name: string) {
  if (!detail.value?.tags) return;
  detail.value.tags = detail.value.tags.filter(t => t.name !== name);
}

function onTagInputEnter() {
  const v = tagInput.value.trim();
  if (!v) return;
  addTag(v);
  tagInput.value = "";
}

async function saveTags() {
  if (!detail.value) return;
  savingTags.value = true;
  try {
    const res = await updateMedia(detail.value.id, {
      tags: detail.value.tags ?? []
    });
    if (res.success) {
      ElMessage.success("标签已保存");
      // 关键：保存后强制 reload，确保本地 detail 与 DB 完全一致
      //  (避免 in-place push 与后端 omitempty 边界情况导致 UI 状态错位)
      await load();
      emit("updated");
    } else {
      ElMessage.error("保存失败：" + (res.message ?? "未知错误"));
    }
  } catch (err: any) {
    ElMessage.error(err?.response?.data?.message || err?.message || err || "保存失败");
  } finally {
    savingTags.value = false;
  }
}
</script>

<template>
  <transition name="drawer-fade">
    <div v-if="mediaId" class="media-detail-mask" @click.self="emit('close')">
      <aside class="media-detail">
        <header class="detail-header">
          <div class="detail-title">
            <el-icon><Document /></el-icon>
            <span>{{ detail?.name ?? "加载中…" }}</span>
          </div>
          <button class="detail-close" @click="emit('close')" title="关闭">
            <el-icon :size="18"><CircleClose /></el-icon>
          </button>
        </header>

        <div v-if="loading" class="detail-loading">
          <el-icon><Refresh /></el-icon>
        </div>

        <div v-else-if="detail" class="detail-body">
          <!-- 缩略图预览：原图走 /api/media/:id/file 后端路由（与 thumb 一致） -->
          <div v-if="detail.type === 'photo' || detail.type === 'video'"
               class="detail-preview">
            <img
              v-if="detail.type === 'photo'"
              :src="previewUrl"
              :alt="detail.name"
            />
            <video
              v-else
              :src="previewUrl"
              controls
              preload="metadata"
            />
          </div>

          <!-- 标签页：元数据 / 3 哈希 / 绑定 / 审计 -->
          <el-tabs class="detail-tabs">
            <!-- 元数据 -->
            <el-tab-pane label="元数据">
              <div v-if="!editMode" class="meta-list">
                <div class="meta-row">
                  <span class="meta-key">名称</span>
                  <span class="meta-val">{{ detail.name }}</span>
                </div>
                <div class="meta-row">
                  <span class="meta-key">原始名</span>
                  <span class="meta-val">{{ detail.original_name || "—" }}</span>
                </div>
                <div class="meta-row">
                  <span class="meta-key">类型</span>
                  <span class="meta-val">
                    <el-tag size="small" effect="plain">
                      {{ detail.type === "photo" ? "照片" : detail.type === "video" ? "录像" : "音频" }}
                    </el-tag>
                  </span>
                </div>
                <div class="meta-row">
                  <span class="meta-key">MIME</span>
                  <span class="meta-val mono">{{ detail.mime_type || "—" }}</span>
                </div>
                <div class="meta-row">
                  <span class="meta-key">尺寸</span>
                  <span class="meta-val mono">
                    {{ detail.width || "?" }} × {{ detail.height || "?" }} px
                  </span>
                </div>
                <div class="meta-row">
                  <span class="meta-key">大小</span>
                  <span class="meta-val mono">{{ formatSize(detail.file_size) }}</span>
                </div>
                <div class="meta-row" v-if="detail.type === 'video'">
                  <span class="meta-key">时长</span>
                  <span class="meta-val mono">{{ detail.duration }} 秒</span>
                </div>
                <div class="meta-row">
                  <span class="meta-key">来源</span>
                  <span class="meta-val">
                    <el-tag size="small" effect="plain">
                      {{ detail.source === "camera" ? "摄像头" : "上传" }}
                    </el-tag>
                  </span>
                </div>
                <div class="meta-row" v-if="detail.watermark_text">
                  <span class="meta-key">水印</span>
                  <span class="meta-val mono small">{{ detail.watermark_text }}</span>
                </div>
                <div class="meta-row">
                  <span class="meta-key">拍摄时间</span>
                  <span class="meta-val mono">{{ formatTime(detail.taken_at) }}</span>
                </div>
                <div class="meta-row">
                  <span class="meta-key">创建时间</span>
                  <span class="meta-val mono">{{ formatTime(detail.created_at) }}</span>
                </div>
                <div class="meta-row">
                  <span class="meta-key">SnowID</span>
                  <span class="meta-val mono small">{{ detail.snowid }}</span>
                </div>
                <div class="meta-row">
                  <span class="meta-key">访问 / 下载</span>
                  <span class="meta-val mono">
                    {{ detail.view_count }} 次 / {{ detail.download_count }} 次
                  </span>
                </div>
                <div class="meta-row full" v-if="detail.remark">
                  <span class="meta-key">备注</span>
                  <span class="meta-val">{{ detail.remark }}</span>
                </div>
              </div>

              <el-form v-else label-position="top" class="edit-form">
                <el-form-item label="名称">
                  <el-input v-model="editName" />
                </el-form-item>
                <el-form-item label="备注">
                  <el-input v-model="editRemark" type="textarea" :rows="3" />
                </el-form-item>
                <el-form-item>
                  <el-button type="primary" @click="saveEdit">保存</el-button>
                  <el-button @click="editMode = false">取消</el-button>
                </el-form-item>
              </el-form>
            </el-tab-pane>

            <!-- 3 种哈希 -->
            <el-tab-pane label="3 种哈希">
              <div class="hash-block">
                <div class="hash-row">
                  <span class="hash-label">SM3</span>
                  <code class="hash-value">{{ detail.hash_sm3 || "—" }}</code>
                  <el-button
                    v-if="detail.hash_sm3"
                    size="small"
                    link
                    @click="copy(detail.hash_sm3!)"
                  >复制</el-button>
                </div>
                <div class="hash-row">
                  <span class="hash-label">SHA-256</span>
                  <code class="hash-value">{{ detail.hash_sha256 || "—" }}</code>
                  <el-button
                    v-if="detail.hash_sha256"
                    size="small"
                    link
                    @click="copy(detail.hash_sha256!)"
                  >复制</el-button>
                </div>
                <div class="hash-row">
                  <span class="hash-label">Combined</span>
                  <code class="hash-value">{{ detail.hash_combined || "—" }}</code>
                  <el-button
                    v-if="detail.hash_combined"
                    size="small"
                    link
                    @click="copy(detail.hash_combined!)"
                  >复制</el-button>
                </div>

                <el-divider />

                <el-button
                  type="primary"
                  :icon="Refresh"
                  :loading="verifying"
                  @click="onVerify"
                >
                  三哈希校验
                </el-button>
                <p v-if="verifyResult" class="verify-result" :class="verifyResult.valid ? 'ok' : 'fail'">
                  <el-icon><Check v-if="verifyResult.valid" /><Close v-else /></el-icon>
                  {{ verifyResult.valid ? "校验通过" : "校验失败" }}
                  （SM3 {{ verifyResult.sm3_match ? "✓" : "✗" }} ·
                    SHA256 {{ verifyResult.sha256_match ? "✓" : "✗" }} ·
                    Combined {{ verifyResult.combined_match ? "✓" : "✗" }}）
                </p>

                <el-divider />

              </div>
            </el-tab-pane>

            <!-- 标签编辑 -->
            <el-tab-pane :label="`标签 (${currentTags.length})`">
              <div class="tag-editor">
                <!-- 当前已添加的 tag（可移除） -->
                <div class="tag-section">
                  <div class="tag-section-title">已添加 ({{ currentTags.length }})</div>
                  <div v-if="currentTags.length === 0" class="empty-inline">
                    暂未添加任何标签
                  </div>
                  <div v-else class="tag-chips">
                    <el-tag
                      v-for="t in currentTags"
                      :key="t.name"
                      :type="(PRESET_TYPE_MAP[t.name] as any) || 'info'"
                      size="default"
                      closable
                      @close="removeTag(t.name)"
                    >
                      {{ t.name }}
                    </el-tag>
                  </div>
                </div>

                <!-- 预制 tag 一键添加（已添加的自动隐藏） -->
                <div class="tag-section">
                  <div class="tag-section-title">预制标签</div>
                  <div v-if="presetRemaining.length === 0" class="empty-inline">
                    全部预制标签已添加
                  </div>
                  <div v-else class="tag-chips">
                    <el-tag
                      v-for="p in presetRemaining"
                      :key="p.name"
                      :type="(p.type as any)"
                      size="default"
                      effect="plain"
                      class="tag-addable"
                      @click="addTag(p.name)"
                    >
                      <el-icon :size="11" style="margin-right: 2px;"><Plus /></el-icon>
                      {{ p.name }}
                    </el-tag>
                  </div>
                </div>

                <!-- 自定义 tag 输入 -->
                <div class="tag-section">
                  <div class="tag-section-title">自定义标签</div>
                  <el-input
                    v-model="tagInput"
                    placeholder="输入标签名后按 Enter 添加"
                    clearable
                    @keyup.enter="onTagInputEnter"
                    @clear="tagInput = ''"
                  >
                    <template #append>
                      <el-button @click="onTagInputEnter">添加</el-button>
                    </template>
                  </el-input>
                </div>

                <el-divider />

                <el-button
                  type="primary"
                  :icon="Check"
                  :loading="savingTags"
                  @click="saveTags"
                >
                  保存标签
                </el-button>
              </div>
            </el-tab-pane>

            <!-- 绑定 -->
            <el-tab-pane :label="`绑定 (${localBindings.length})`">
              <div class="binding-editor">
                <!-- 已有绑定列表（local，可临时删） -->
                <div v-if="localBindings.length > 0" class="binding-section">
                  <ul class="bindings-list">
                    <li
                      v-for="(b, i) in localBindings"
                      :key="`${b.target_type}-${b.target_id}-${b.role}`"
                      class="binding-item"
                    >
                      <el-tag size="small" type="primary" effect="plain">
                        {{ typeLabelMap[b.target_type] ?? b.target_type }}
                      </el-tag>
                      <span class="binding-id">#{{ b.target_id }}</span>
                      <el-tag size="small" effect="plain">
                        {{ roleLabelMap[b.role] ?? b.role }}
                      </el-tag>
                      <span v-if="b.remark" class="binding-remark">{{ b.remark }}</span>
                      <el-button
                        link
                        type="danger"
                        @click="removeBindingLocal(i)"
                      >移除</el-button>
                    </li>
                  </ul>
                </div>
                <div v-else class="empty-inline">
                  暂未绑定到任何文档
                </div>

                <el-divider />

                <!-- 添加表单 -->
                <div class="binding-add-form">
                  <span class="form-label">添加新绑定</span>
                  <div class="form-row">
                    <!--
                      :teleported="false" 显式拒绝 Teleport：因为 .detail-body 在
                      z-index:9998 的 .media-detail-mask 内（带 backdrop-filter 创建独立 stacking context），
                      默认 Teleport 的 popper 落到 body 会被 mask 遮盖。改为就地渲染后 popper 跟随
                      form-row 的 z-index（最高），能稳定显示。
                    -->
                    <el-select
                      v-model="bindingForm.target_type"
                      :teleported="false"
                      size="default"
                      style="width: 110px"
                    >
                      <el-option
                        v-for="t in BINDING_TYPES"
                        :key="t.value"
                        :label="t.label"
                        :value="t.value"
                      />
                    </el-select>
                    <el-input-number
                      v-model="bindingForm.target_id"
                      :min="1"
                      :step="1"
                      controls-position="right"
                      placeholder="目标 ID"
                      style="width: 140px"
                    />
                    <el-select
                      v-model="bindingForm.role"
                      :teleported="false"
                      size="default"
                      style="width: 130px"
                    >
                      <el-option
                        v-for="r in BINDING_ROLES"
                        :key="r.value"
                        :label="r.label"
                        :value="r.value"
                      />
                    </el-select>
                    <el-input
                      v-model="bindingForm.remark"
                      placeholder="备注（可选）"
                      clearable
                      style="flex: 1; min-width: 120px"
                      @keyup.enter="addBindingLocal"
                    />
                    <el-button
                      type="primary"
                      :icon="Plus"
                      @click="addBindingLocal"
                    >添加</el-button>
                  </div>
                  <p v-if="bindingFormError" class="form-error">
                    {{ bindingFormError }}
                  </p>
                </div>

                <el-divider />

                <!-- 保存 / 重置 -->
                <div class="binding-actions">
                  <el-button
                    type="primary"
                    :icon="Check"
                    :loading="savingBindings"
                    :disabled="!bindingDirty"
                    @click="saveBindings"
                  >保存绑定</el-button>
                  <el-button :icon="RefreshLeft" :disabled="!bindingDirty" @click="resetBindingsLocal">
                    重置
                  </el-button>
                  <span v-if="bindingDirty" class="binding-dirty-hint">有未保存的修改</span>
                </div>
              </div>
            </el-tab-pane>

            <!-- 审计 -->
            <el-tab-pane :label="`审计 (${(detail.audit ?? []).length})`">
              <div v-if="(detail.audit ?? []).length === 0" class="empty-inline">
                暂无审计日志
              </div>
              <ul v-else class="audit-list">
                <li
                  v-for="(a, i) in (detail.audit ?? []).slice(-100).reverse()"
                  :key="i"
                  class="audit-item"
                >
                  <el-tag size="small" effect="plain">{{ a.action }}</el-tag>
                  <span class="audit-actor">用户 #{{ a.actor_id }}</span>
                  <span class="audit-time mono">{{ formatTime(a.ts) }}</span>
                </li>
              </ul>
            </el-tab-pane>
          </el-tabs>
        </div>

        <!--
          底部按钮已精简：去掉 下载 + 删除（与列表 hover 操作重复，且抽屉底部按钮位置容易被 mask 挡住）
            保留：恢复（仅 deleted 项，与列表互斥——列表不显示 deleted 项时无法恢复）和编辑
        -->
        <footer v-if="detail && !loading" class="detail-footer">
          <template v-if="isDeleted">
            <el-button
              :icon="RefreshLeft"
              type="success"
              plain
              @click="onRestore"
            >恢复</el-button>
          </template>
          <el-button v-else-if="!editMode" @click="enterEdit">编辑</el-button>
          <div class="footer-spacer" />
        </footer>
      </aside>
    </div>
  </transition>
</template>

<style scoped>
.media-detail-mask {
  position: fixed;
  inset: 0;
  z-index: 9998;
  background: rgba(0, 0, 0, 0.3);
  backdrop-filter: blur(2px);
  display: flex;
  justify-content: flex-end;
}

.media-detail {
  width: 600px;
  max-width: 90vw;
  height: 100%;
  background: var(--el-bg-color, #fff);
  display: flex;
  flex-direction: column;
  box-shadow: -8px 0 24px rgba(0, 0, 0, 0.1);
}

.detail-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 16px 20px;
  border-bottom: 1px solid var(--el-border-color-lighter, #ebeef5);
  background: var(--el-fill-color-light, #f5f7fa);
}
.detail-title {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  font-size: 15px;
  font-weight: 600;
  flex: 1;
  min-width: 0;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.detail-close {
  appearance: none;
  border: 0;
  background: transparent;
  cursor: pointer;
  color: var(--el-text-color-secondary, #606266);
  padding: 4px;
  border-radius: 4px;
}
.detail-close:hover {
  background: var(--el-fill-color, #f0f2f5);
  color: var(--el-color-danger, #f56c6c);
}

.detail-loading {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--el-text-color-secondary, #606266);
}

.detail-body {
  flex: 1;
  overflow-y: auto;
  padding: 0 20px 16px;
}

.detail-preview {
  margin: 16px 0;
  background: #0a0a0a;
  border-radius: 8px;
  overflow: hidden;
  text-align: center;
}
.detail-preview img,
.detail-preview video {
  max-width: 100%;
  max-height: 240px;
  display: block;
  margin: 0 auto;
}

.detail-tabs {
  margin-top: 8px;
}

.meta-list {
  padding: 8px 0;
}
.meta-row {
  display: grid;
  grid-template-columns: 100px 1fr;
  align-items: center;
  gap: 12px;
  padding: 10px 0;
  border-bottom: 1px dashed var(--el-border-color-lighter, #ebeef5);
  font-size: 13px;
}
.meta-row.full {
  grid-template-columns: 100px 1fr;
}
.meta-key {
  color: var(--el-text-color-secondary, #606266);
  font-weight: 500;
}
.meta-val {
  color: var(--el-text-color-primary, #1f2329);
}
.mono {
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
  font-size: 12.5px;
}
.small {
  font-size: 11.5px;
  word-break: break-all;
}

.edit-form {
  padding: 8px 0;
}

.hash-block {
  padding: 12px 0;
}
.hash-row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 0;
  border-bottom: 1px dashed var(--el-border-color-lighter, #ebeef5);
}
.hash-row:last-of-type {
  border-bottom: 0;
}
.hash-label {
  width: 90px;
  font-weight: 600;
  color: var(--el-color-primary, #409eff);
  font-size: 12px;
}
.hash-value {
  flex: 1;
  font-family: ui-monospace, SFMono-Regular, Consolas, monospace;
  font-size: 11.5px;
  background: var(--el-fill-color-light, #f5f7fa);
  padding: 6px 8px;
  border-radius: 4px;
  word-break: break-all;
}

.verify-result {
  margin-top: 12px;
  padding: 8px 12px;
  border-radius: 6px;
  font-size: 12px;
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.verify-result.ok {
  color: #16a34a;
  background: rgba(22, 163, 74, 0.1);
}
.verify-result.fail {
  color: #ef4444;
  background: rgba(239, 68, 68, 0.1);
}


.empty-inline {
  padding: 32px 0;
  text-align: center;
  color: var(--el-text-color-secondary, #606266);
  font-size: 12.5px;
}

/* ===== 标签编辑器 ===== */
.tag-editor {
  padding: 8px 0;
}
.tag-section {
  margin-bottom: 18px;
}
.tag-section-title {
  font-size: 12px;
  font-weight: 600;
  color: var(--el-text-color-secondary, #606266);
  letter-spacing: 0.04em;
  text-transform: uppercase;
  margin-bottom: 10px;
}
.tag-chips {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
  min-height: 32px;
}
.tag-addable {
  cursor: pointer;
  transition: transform 140ms ease, box-shadow 140ms ease;
  user-select: none;
}
.tag-addable:hover {
  transform: translateY(-1px);
  box-shadow: 0 2px 6px rgba(15, 23, 42, 0.12);
}
.tag-addable:active {
  transform: translateY(0);
}

.bindings-list,
.audit-list {
  margin: 0;
  padding: 0;
  list-style: none;
}
.binding-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 12px;
  border-radius: 6px;
  background: var(--el-fill-color-blank, #fff);
  border: 1px solid var(--el-border-color-lighter, #ebeef5);
  margin-bottom: 6px;
  font-size: 12.5px;
  transition: border-color 140ms ease, background 140ms ease;
}
.binding-item:hover {
  border-color: var(--el-color-primary-light-5, #c6e2ff);
  background: rgba(64, 158, 255, 0.02);
}
.binding-id {
  color: var(--el-text-color-primary, #1f2329);
  font-family: ui-monospace, monospace;
  font-weight: 600;
  font-size: 12.5px;
}
.binding-remark {
  flex: 1;
  color: var(--el-text-color-secondary, #909399);
  font-size: 12px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* 绑定添加表单 */
.binding-add-form {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

/* 绑定作用说明（首屏的提示卡片） */
.binding-help {
  margin-bottom: 16px;
}
.binding-help :deep(.el-alert__content) {
  font-size: 12.5px;
  line-height: 1.7;
  color: var(--el-text-color-regular, #606266);
}
.binding-help-list {
  margin: 8px 0 0;
  padding-left: 20px;
  font-size: 12px;
  color: var(--el-text-color-regular, #606266);
}
.binding-help-list li {
  margin-bottom: 2px;
}
.binding-help-list li b {
  color: var(--el-color-primary, #409eff);
  font-weight: 600;
  margin-right: 4px;
}
.binding-add-form .form-label {
  font-size: 12px;
  font-weight: 600;
  color: var(--el-text-color-secondary, #606266);
  letter-spacing: 0.04em;
  text-transform: uppercase;
}
.binding-add-form .form-row {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  position: relative;
  z-index: 10;
}
.binding-add-form .form-error {
  margin: 4px 0 0;
  font-size: 12px;
  color: var(--el-color-danger, #f56c6c);
  font-weight: 500;
}

/* 绑定底部操作 */
.binding-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}
.binding-dirty-hint {
  margin-left: auto;
  font-size: 12px;
  color: var(--el-color-warning, #e6a23c);
  font-weight: 500;
}

.audit-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 6px 0;
  font-size: 12px;
}
.audit-actor {
  color: var(--el-text-color-primary, #1f2329);
}
.audit-time {
  color: var(--el-text-color-secondary, #606266);
  font-size: 11.5px;
}

/* ============================================================
   媒体库全局：ElMessageBox.confirm 弹层
   .media-detail-mask 用了 backdrop-filter 创建独立 stacking context，
   ElMessageBox 默认 z-index 2001 会被 mask (9998) 遮盖。
   解决：Element Plus MessageBox 的 overlay 使用 .el-overlay.is-message-box，
   需要同时提升 message-box 和其 overlay 的 z-index。
   ============================================================ */
:global(.el-message-box.media-confirm-msg),
:global(.media-confirm-msg.el-message-box),
:global(.el-overlay.is-message-box) {
  z-index: 99999 !important;
}

.detail-footer {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 12px 20px;
  border-top: 1px solid var(--el-border-color-lighter, #ebeef5);
  background: var(--el-bg-color, #fff);
}
.footer-spacer {
  flex: 1;
}

.drawer-fade-enter-active,
.drawer-fade-leave-active {
  transition: opacity 200ms ease;
}
.drawer-fade-enter-active .media-detail,
.drawer-fade-leave-active .media-detail {
  transition: transform 240ms cubic-bezier(0.2, 0.8, 0.2, 1);
}
.drawer-fade-enter-from,
.drawer-fade-leave-to {
  opacity: 0;
}
.drawer-fade-enter-from .media-detail,
.drawer-fade-leave-to .media-detail {
  transform: translateX(100%);
}
</style>
