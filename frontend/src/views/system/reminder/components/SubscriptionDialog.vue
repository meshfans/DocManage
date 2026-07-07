<script setup lang="ts">
// 第十三阶段 v4：对象的提醒订阅管理 对话框（可复用）
//   - 父组件传入 linkType + linkId → 锁定
//   - 显示该对象的所有订阅（list + manage）
//   - 内嵌"新建订阅"表单
// 场景：
//   1. 列表页"提醒"按钮 → 弹出此 dialog（linkType + linkId 锁定）
//   2. /system/reminder 全量管理（不传 linkType/linkId，列表显示全量）

import { ref, watch, computed, onMounted } from "vue";
import { ElMessage } from "element-plus";
import {
  listReminderTemplates,
  createReminderSubscription,
  RECEIVER_TYPE_OPTIONS,
  LINK_TYPE_OPTIONS,
  type ReminderTemplate,
  type ReminderSubscription,
  type LinkType
} from "@/api/reminder";
import { Bell } from "@element-plus/icons-vue";
// 第十三阶段 v3：复用 flow/template.vue 的复选组件
import UserSelect from "@/components/UserSelect/index.vue";
import DepartmentSelect from "@/components/DepartmentSelect/index.vue";
// 第十三阶段 v4：内嵌列表（同一对象的所有订阅）
// SubscriptionTable 改用 emit('create') 通知父组件，避免循环引用
import SubscriptionTable from "./SubscriptionTable.vue";
// 优化 1：根据 link_type 查实体名（用于 dialog 标题）
import { getCustomerById } from "@/api/customer";
import { getThirdPartyContract } from "@/api/third_party";

type ReceiverType = ReminderSubscription["receiver_type"];

const props = defineProps<{
  visible: boolean;
  // 第十三阶段 v4：详情页入口直接传 link_type + link_id
  linkType?: LinkType;
  linkId?: number;
}>();

const emit = defineEmits<{
  (e: "update:visible", v: boolean): void;
  (e: "created"): void;
}>();

// ==================== 表单（新建订阅） ====================
const form = ref<{
  template_id: number | null;
  // 第十三阶段 v4：link_type + link_id
  link_type: LinkType;
  // UI 状态：手输 CSV（与 v2 receiver_id 一致）
  link_id: string;
  // 接收人
  receiver_type: ReceiverType;
  receiver_ids: number[]; // 复选组件的 UI 状态
  remark: string;
}>({
  template_id: null,
  link_type: "customer",
  link_id: "",
  receiver_type: "admin",
  receiver_ids: [],
  remark: ""
});

// ==================== 模板下拉 ====================
const templates = ref<ReminderTemplate[]>([]);
const templatesLoading = ref(false);
async function loadTemplates() {
  templatesLoading.value = true;
  try {
    const res: any = await listReminderTemplates();
    if (res.success) {
      templates.value = (res.data.list || []).filter((t: ReminderTemplate) => t.is_active);
    } else {
      ElMessage.error("加载模板失败");
    }
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.message || e?.message || e || "加载失败");
  } finally {
    templatesLoading.value = false;
  }
}

// ==================== 锁定（父组件传值时禁用 link_type + 预填 link_id） ====================
const lockLink = computed(() => !!props.linkType && !!props.linkId && props.linkId > 0);

const linkIDPlaceholder = computed(() => {
  if (form.value.link_type === "customer") return "客户 ID，多个用逗号分隔";
  if (form.value.link_type === "third_party_contract") return "文档 ID，多个用逗号分隔";
  return "请选择 link_type";
});

// ==================== 接收人是否需要选择 ====================
const needReceiver = computed(() => {
  return form.value.receiver_type === "department" || form.value.receiver_type === "user";
});

function onReceiverTypeChange() {
  form.value.receiver_ids = [];
}

function onLinkTypeChange() {
  if (!lockLink.value) {
    form.value.link_id = "";
  }
}

// ==================== 校验 link_id CSV 格式 ====================
function isValidLinkID(s: string): boolean {
  if (!s.trim()) return false;
  const parts = s.split(",").map(p => p.trim()).filter(p => p);
  if (parts.length === 0) return false;
  return parts.every(p => /^[1-9]\d*$/.test(p));
}

// ==================== Dialog 标题（优化 1：显示实体名） ====================
const linkEntityName = ref<string>(""); // 实体名（缓存，避免重复请求）

// 根据 linkType 查实体名
async function fetchLinkEntityName() {
  linkEntityName.value = "";
  if (!lockLink.value || !props.linkType || !props.linkId) return;
  try {
    if (props.linkType === "customer") {
      const res: any = await getCustomerById(props.linkId);
      const c = res?.data || res;
      // 优先公司名 > 个人名 > phone
      linkEntityName.value = c?.company_name || c?.real_name || c?.phone || "";
    } else if (props.linkType === "third_party_contract") {
      const res: any = await getThirdPartyContract(props.linkId);
      const c = res?.data || res;
      // 文档有 title
      linkEntityName.value = c?.title || c?.contract_no || `文档 #${props.linkId}`;
    }
  } catch (e: any) {
    // 静默失败（不阻塞 dialog 打开）
    console.warn("[reminder] fetch link entity name failed:", e?.message || e);
  }
}

const dialogTitle = computed(() => {
  if (lockLink.value) {
    const typeLabel = LINK_TYPE_OPTIONS.find(o => o.value === props.linkType)?.label || props.linkType;
    // 显示实体名（如果有）+ ID 兜底
    const name = linkEntityName.value;
    const idStr = name ? `${name}（#${props.linkId}）` : `#${props.linkId}`;
    return `${typeLabel} ${idStr} 的提醒订阅`;
  }
  return "提醒订阅管理";
});

// ==================== 列表区域（子组件 SubscriptionTable） ====================
// refresh key 用于"创建/启停/删除"后强制刷新
const listRefreshKey = ref(0);
function onListChanged() {
  listRefreshKey.value++;
}

// ==================== 打开时重置表单 ====================
watch(
  () => props.visible,
  v => {
    if (v) {
      const initLinkType: LinkType = (props.linkType && props.linkId && props.linkId > 0)
        ? props.linkType
        : "customer";
      const initLinkID = (props.linkId && props.linkId > 0)
        ? String(props.linkId)
        : "";
      form.value = {
        template_id: null,
        link_type: initLinkType,
        link_id: initLinkID,
        receiver_type: "admin",
        receiver_ids: [],
        remark: ""
      };
      if (templates.value.length === 0) loadTemplates();
      // 每次打开强制刷新列表
      listRefreshKey.value++;
      // 优化 1：查实体名用于标题
      fetchLinkEntityName();
    }
  }
);

onMounted(loadTemplates);

// ==================== 保存 ====================
const saving = ref(false);

async function handleSave() {
  if (!form.value.template_id) {
    ElMessage.warning("请选择提醒模板");
    return;
  }
  if (!isValidLinkID(form.value.link_id)) {
    ElMessage.warning(
      `link_type=${form.value.link_type} 时 link_id 必填（${linkIDPlaceholder.value}）`
    );
    return;
  }
  if (needReceiver.value && form.value.receiver_ids.length === 0) {
    ElMessage.warning(
      `receiver_type=${form.value.receiver_type} 时必须选择至少一个接收人`
    );
    return;
  }
  const recvCSV = form.value.receiver_ids.join(",");
  saving.value = true;
  try {
    const res: any = await createReminderSubscription({
      template_id: form.value.template_id,
      link_type: form.value.link_type,
      link_id: form.value.link_id,
      receiver_type: form.value.receiver_type,
      receiver_id: needReceiver.value ? recvCSV : "",
      remark: form.value.remark
    });
    if (res.success) {
      ElMessage.success("订阅已创建");
      // 重置表单
      form.value = {
        template_id: null,
        link_type: lockLink.value ? props.linkType! : "customer",
        link_id: lockLink.value ? String(props.linkId) : "",
        receiver_type: "admin",
        receiver_ids: [],
        remark: ""
      };
      // 触发列表刷新
      onListChanged();
      emit("created");
    } else {
      ElMessage.error(res.message || "创建失败");
    }
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.message || e?.message || e || "创建失败");
  } finally {
    saving.value = false;
  }
}

function handleCancel() {
  emit("update:visible", false);
}
</script>

<template>
  <el-dialog
    :model-value="visible"
    :title="dialogTitle"
    width="900"
    :close-on-click-modal="false"
    @update:model-value="(v: boolean) => emit('update:visible', v)"
    @close="handleCancel"
  >
    <!-- 上半：列表（已有订阅） -->
    <div class="reminder-list-section">
      <div class="section-title">
        <el-icon><Bell /></el-icon>
        <span>已有订阅（可启停/删除）</span>
      </div>
      <SubscriptionTable
        :key="listRefreshKey"
        :link-type="lockLink ? props.linkType : undefined"
        :link-id="lockLink ? props.linkId : undefined"
        :hide-create="true"
        :hide-header-hint="true"
        @refresh="onListChanged"
      />
    </div>

    <el-divider />

    <!-- 下半：新建订阅表单 -->
    <div class="reminder-form-section">
      <div class="section-title">+ 新建订阅</div>
      <el-form :model="form" label-width="100px" label-position="right">
        <el-form-item label="提醒模板" required>
          <el-select
            v-model="form.template_id"
            placeholder="请选择提醒模板"
            style="width: 100%"
            :loading="templatesLoading"
          >
            <el-option
              v-for="t in templates"
              :key="t.id"
              :label="`${t.name}（提前 ${t.advance_days} 天）`"
              :value="t.id"
            />
          </el-select>
        </el-form-item>

        <el-form-item label="订阅对象类型" required>
          <el-select
            v-model="form.link_type"
            style="width: 100%"
            :disabled="lockLink"
            @change="onLinkTypeChange"
          >
            <el-option
              v-for="opt in LINK_TYPE_OPTIONS"
              :key="opt.value"
              :label="opt.label"
              :value="opt.value"
            >
              <div style="display: flex; flex-direction: column; gap: 2px">
                <span>{{ opt.label }}</span>
                <span style="font-size: 12px; color: #999">{{ opt.description }}</span>
              </div>
            </el-option>
          </el-select>
        </el-form-item>

        <el-form-item label="订阅对象 ID" required>
          <el-input
            v-model="form.link_id"
            :disabled="lockLink"
            :placeholder="linkIDPlaceholder"
            style="width: 100%"
            clearable
          />
          <div v-if="lockLink" class="hint">
            已锁定为当前对象（type={{ props.linkType }}，id=#{{ props.linkId }}）
          </div>
          <div v-else class="hint">
            支持多个 ID，用英文逗号分隔（如 "1,2,3"），同类型多对象订阅
          </div>
        </el-form-item>

        <el-form-item label="接收人" required>
          <el-select
            v-model="form.receiver_type"
            style="width: 100%"
            @change="onReceiverTypeChange"
          >
            <el-option
              v-for="opt in RECEIVER_TYPE_OPTIONS"
              :key="opt.value"
              :label="opt.label"
              :value="opt.value"
            >
              <div style="display: flex; flex-direction: column; gap: 2px">
                <span>{{ opt.label }}</span>
                <span style="font-size: 12px; color: #999">{{ opt.description }}</span>
              </div>
            </el-option>
          </el-select>
        </el-form-item>

        <el-form-item
          v-if="form.receiver_type === 'department'"
          label="选择部门"
          required
        >
          <DepartmentSelect
            v-model="form.receiver_ids"
            :multiple="true"
            placeholder="添加部门"
          />
        </el-form-item>

        <el-form-item
          v-else-if="form.receiver_type === 'user'"
          label="选择用户"
          required
        >
          <UserSelect
            v-model="form.receiver_ids"
            :multiple="true"
            placeholder="添加用户"
          />
        </el-form-item>

        <el-form-item label="备注">
          <el-input
            v-model="form.remark"
            type="textarea"
            :rows="2"
            maxlength="200"
            show-word-limit
            placeholder="可选，例如：续签提前通知"
          />
        </el-form-item>

        <el-form-item>
          <el-button
            type="primary"
            :loading="saving"
            :icon="undefined as any"
            @click="handleSave"
            :disabled="!form.template_id"
          >
            创建订阅
          </el-button>
          <el-button @click="handleCancel">关闭</el-button>
        </el-form-item>
      </el-form>
    </div>
  </el-dialog>
</template>

<style scoped>
.section-title {
  display: flex;
  align-items: center;
  gap: 6px;
  font-weight: 600;
  font-size: 14px;
  margin-bottom: 12px;
  color: var(--el-text-color-primary);
}
.reminder-list-section {
  max-height: 360px;
  overflow-y: auto;
  border: 1px solid var(--el-border-color-lighter);
  border-radius: 4px;
  padding: 12px;
  background: var(--el-fill-color-blank);
}
.hint {
  margin-top: 4px;
  font-size: 12px;
  color: var(--el-text-color-secondary);
}
</style>
