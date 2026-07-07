<script setup lang="ts">
import { ref, computed, onMounted } from "vue";
import { useRoute, useRouter } from "vue-router";
import { ElMessage } from "element-plus";
import {
  Upload,
  ArrowLeft,
  Download,
  Document
} from "@element-plus/icons-vue";
import {
  getThirdPartyContract,
  updateThirdPartyContract,
  uploadThirdPartyContractPdf,
  downloadThirdPartyContractPdf,
  type ThirdPartyContract
} from "@/api/third_party";
// 第六阶段 v2：移除 ContractLockPanel，文档暂不做归档/锁定
// 第十二阶段：文档详情页"提醒"入口（v4 调整：挪到列表层，详情页删除）
// 2026-07-07 用户可见文案："第三方合同"→"文档"

defineOptions({ name: "ThirdPartyContractDetail" });

const route = useRoute();
const router = useRouter();

const id = computed(() => {
  const v = route.query.thirdPartyId;
  if (v === "new" || !v) return 0;
  return Number(v);
});
const isEdit = computed(() => id.value > 0);

// 注：customer_id 由 /customer/contract?id=N 传入 URL，
// 此页面不再让用户选"对方"（避免重复操作）。
const fixedCustomerId = computed(() => {
  const v = route.query.customerId;
  return v ? Number(v) : 0;
});

// ==================== 表单字段 ====================
const title = ref("");
const type = ref<"paper" | "electronic">("paper");
const amount = ref(0);
const currency = ref("CNY");
const signDate = ref<string>("");       // 格式 "YYYY-MM-DD"，0 表示未填
const startDate = ref<string>("");
const endDate = ref<string>("");
const remark = ref("");
const status = ref("draft");

// 终态守卫（archived / cancelled）：终态合同不可修改表单 / 不可重新上传 / 不可保存
// 详情页可"查看"（下载 PDF / 跳回列表），但写操作被禁用
const isReadOnly = computed(
  () => isEdit.value && (status.value === "archived" || status.value === "cancelled")
);
const readOnlyLabel = computed(() => {
  if (status.value === "archived") return "已归档（只读）";
  if (status.value === "cancelled") return "已取消（只读）";
  return "";
});

// ==================== 文件 ====================
const filePath = ref("");
const fileSize = ref(0);
const fileSM3Hash = ref("");
const fileSHA256Hash = ref("");
const fileCombinedHash = ref("");
const uploading = ref(false);
const fileInput = ref<HTMLInputElement | null>(null);
const dragOver = ref(false);   // 拖拽高亮

onMounted(() => {
  if (isEdit.value) loadDetail();
});

async function loadDetail() {
  const res: any = await getThirdPartyContract(id.value);
  if (res.success) {
    const d = res.data;
    title.value = d.title;
    type.value = d.type;
    amount.value = d.amount;
    currency.value = d.currency;
    signDate.value = d.sign_date ? formatTs(d.sign_date) : "";
    startDate.value = d.start_date ? formatTs(d.start_date) : "";
    endDate.value = d.end_date ? formatTs(d.end_date) : "";
    remark.value = d.remark;
    filePath.value = d.file_path;
    fileSize.value = d.file_size;
    fileSM3Hash.value = d.file_sm3_hash;
    fileSHA256Hash.value = d.file_sha256_hash;
    fileCombinedHash.value = d.file_combined_hash;
    status.value = d.status;
  } else {
    ElMessage.error("加载失败");
  }
}

function formatTs(ts: number): string {
  const d = new Date(ts * 1000);
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, "0");
  const day = String(d.getDate()).padStart(2, "0");
  return `${y}-${m}-${day}`;
}

function tsFromDate(s: string): number {
  if (!s) return 0;
  const d = new Date(s);
  return isNaN(d.getTime()) ? 0 : Math.floor(d.getTime() / 1000);
}

// ==================== 文件上传 ====================
function pickFile() {
  fileInput.value?.click();
}

function onFileSelected(e: Event) {
  const target = e.target as HTMLInputElement;
  const file = target.files?.[0];
  if (file) handleFile(file);
  if (target) target.value = "";
}

function onDrop(e: DragEvent) {
  dragOver.value = false;
  const file = e.dataTransfer?.files?.[0];
  if (file) handleFile(file);
}

async function handleFile(file: File) {
  // 2026-06-23：终态守卫（防御性）
  if (isReadOnly.value) {
    ElMessage.warning(`文档${readOnlyLabel.value}，不可重新上传`);
    return;
  }
  if (file.type !== "application/pdf") {
    ElMessage.warning("只支持 PDF 文件");
    return;
  }
  if (file.size > 20 * 1024 * 1024) {
    ElMessage.warning("PDF 超过 20MB");
    return;
  }
  const base64 = await fileToBase64(file);
  uploading.value = true;
  try {
    if (isEdit.value) {
      const res: any = await uploadThirdPartyContractPdf(id.value, base64);
      if (res.success) {
        filePath.value = res.data.file_path;
        fileSize.value = res.data.file_size;
        fileSM3Hash.value = res.data.file_sm3_hash;
        fileSHA256Hash.value = res.data.file_sha256_hash;
        fileCombinedHash.value = res.data.file_combined_hash;
        ElMessage.success("已上传 PDF");
      } else {
        ElMessage.error(res.message || "上传失败");
      }
    } else {
      // 新建模式：暂存 base64，提交时再上传（如果当时 customerId 已知则可立即上传）
      pendingPdfBase64.value = base64;
      fileSize.value = file.size;
      filePath.value = "pending://upload-on-submit";
      ElMessage.success(`已选择：${(file.size / 1024).toFixed(1)}KB（提交时上传）`);
    }
  } finally {
    uploading.value = false;
  }
}

const pendingPdfBase64 = ref("");

function fileToBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(reader.result as string);
    reader.onerror = reject;
    reader.readAsDataURL(file);
  });
}

// ==================== 提交 ====================
const submitting = ref(false);

async function handleSubmit() {
  if (!title.value.trim()) {
    ElMessage.warning("请输入标题");
    return;
  }
  if (fixedCustomerId.value <= 0) {
    ElMessage.warning("缺少 customer_id（应从 /customer/contract?id=N 进入）");
    return;
  }
  if (!isEdit.value && !pendingPdfBase64.value) {
    ElMessage.warning("请先上传已签署的 PDF 文件");
    return;
  }

  submitting.value = true;
  try {
    let contractId: number;
    if (isEdit.value) {
      const res: any = await updateThirdPartyContract(id.value, {
        title: title.value.trim(),
        type: type.value,
        customer_id: fixedCustomerId.value,
        amount: amount.value,
        currency: currency.value,
        sign_date: tsFromDate(signDate.value),
        start_date: tsFromDate(startDate.value),
        end_date: tsFromDate(endDate.value),
        remark: remark.value
      });
      if (!res.success) {
        ElMessage.error(res.message || "保存失败");
        return;
      }
      contractId = id.value;
    } else {
      // 新建：先创建骨架（空文件字段），拿到 id 后立刻上传 PDF
      const res: any = await createSkeleton(
        fixedCustomerId.value,
        title.value.trim(),
        type.value,
        amount.value,
        currency.value,
        signDate.value,
        startDate.value,
        endDate.value,
        remark.value
      );
      if (!res.success) {
        ElMessage.error(res.message || "创建失败");
        return;
      }
      contractId = res.data.id;

      if (pendingPdfBase64.value) {
        const upRes: any = await uploadThirdPartyContractPdf(contractId, pendingPdfBase64.value);
        if (!upRes.success) {
          ElMessage.warning("已创建但 PDF 上传失败：" + (upRes.message || ""));
        }
      }
    }
    ElMessage.success(isEdit.value ? "已保存" : "已创建");
    router.back();
  } catch (err: any) {
    ElMessage.error(err?.response?.data?.message || err?.message || err || "请求失败");
  } finally {
    submitting.value = false;
  }
}

// 抽出：建骨架（用于新建后立即上传）
async function createSkeleton(
  customerId: number,
  titleVal: string,
  typeVal: "paper" | "electronic",
  amountVal: number,
  currencyVal: string,
  signDateVal: string,
  startDateVal: string,
  endDateVal: string,
  remarkVal: string
) {
  const { createThirdPartyContract } = await import("@/api/third_party");
  return createThirdPartyContract({
    title: titleVal,
    type: typeVal,
    status: "draft",
    customer_id: customerId,
    amount: amountVal,
    currency: currencyVal,
    sign_date: tsFromDate(signDateVal),
    start_date: tsFromDate(startDateVal),
    end_date: tsFromDate(endDateVal),
    file_path: "pending://upload-on-submit",
    file_size: fileSize.value,
    file_sm3_hash: "",
    file_sha256_hash: "",
    file_combined_hash: "",
    remark: remarkVal
  });
}

async function handleDownload() {
  if (!isEdit.value || fileSize.value <= 0) {
    ElMessage.warning("该合同尚未上传 PDF");
    return;
  }
  try {
    const blob = (await downloadThirdPartyContractPdf(id.value)) as Blob;
    const url = window.URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `third_party_${id.value}.pdf`;
    document.body.appendChild(a);
    a.click();
    window.URL.revokeObjectURL(url);
    document.body.removeChild(a);
  } catch (err: any) {
    ElMessage.error(err?.response?.data?.message || err?.message || err || "下载失败");
  }
}

function goBack() {
  router.back();
}

// ==================== 第十二阶段：提醒订阅 Dialog ====================
// 第十三阶段 v4：详情页的"提醒"按钮已删除（挪到列表层）
// 保留注释以便历史参考
</script>

<template>
  <div class="tp-detail">
    <el-card>
      <template #header>
        <div class="card-header">
          <el-button :icon="ArrowLeft" link @click="goBack" />
          <span class="page-title">
            {{ isEdit ? "编辑文档" : "新建文档" }}
          </span>
          <el-tag v-if="isEdit" :type="status === 'signed' ? 'success' : 'info'" size="small">
            {{ status }}
          </el-tag>
          <el-tag v-if="isReadOnly" :type="status === 'archived' ? 'info' : 'danger'" size="small" effect="dark">
            {{ readOnlyLabel }}
          </el-tag>
        </div>
      </template>

      <el-form label-width="100px" style="max-width: 720px">
        <el-form-item label="标题" required>
          <el-input v-model="title" maxlength="200" show-word-limit :disabled="isReadOnly" />
        </el-form-item>

        <el-form-item label="类型">
          <el-radio-group v-model="type" :disabled="isReadOnly">
            <el-radio-button label="paper">纸质</el-radio-button>
            <el-radio-button label="electronic">电子</el-radio-button>
          </el-radio-group>
        </el-form-item>

        <el-form-item label="金额">
          <el-input-number
            v-model="amount"
            :min="0"
            :precision="2"
            :step="1000"
            :disabled="isReadOnly"
            style="width: 200px"
          />
          <el-select v-model="currency" :disabled="isReadOnly" style="width: 100px; margin-left: 8px">
            <el-option label="CNY" value="CNY" />
            <el-option label="USD" value="USD" />
            <el-option label="EUR" value="EUR" />
          </el-select>
        </el-form-item>

        <el-form-item label="签署日期">
          <el-date-picker
            v-model="signDate"
            type="date"
            placeholder="选择签署日期"
            value-format="YYYY-MM-DD"
            :disabled="isReadOnly"
            style="width: 200px"
          />
        </el-form-item>

        <el-form-item label="生效日期">
          <el-date-picker
            v-model="startDate"
            type="date"
            placeholder="选择生效日期"
            value-format="YYYY-MM-DD"
            :disabled="isReadOnly"
            style="width: 200px"
          />
        </el-form-item>

        <el-form-item label="到期日期">
          <el-date-picker
            v-model="endDate"
            type="date"
            placeholder="选择到期日期"
            value-format="YYYY-MM-DD"
            :disabled="isReadOnly"
            style="width: 200px"
          />
        </el-form-item>

        <el-form-item label="已签署 PDF" required>
          <!--
            v5 拖拽/点击 上传：参考新建印章的 PNG 上传
            1. 有文件：显示已上传 + 文件大小 + 下载按钮
            2. 无文件：拖拽/点击区域（虚线边框 + 中央图标）
          -->
          <div v-if="fileSize > 0" class="file-info">
            <el-tag type="success" size="small">
              <el-icon style="margin-right: 4px"><Document /></el-icon>
              已上传 {{ (fileSize / 1024).toFixed(1) }} KB
            </el-tag>
            <el-button
              v-if="isEdit"
              link
              type="primary"
              size="small"
              :icon="Download"
              style="margin-left: 8px"
              @click="handleDownload"
            >
              下载
            </el-button>
            <el-button
              link
              type="primary"
              size="small"
              style="margin-left: 8px"
              @click="pickFile"
            >
              重新上传
            </el-button>
          </div>
          <div
            v-else
            class="dropzone"
            :class="{ 'is-dragover': dragOver }"
            @click="pickFile"
            @dragover.prevent="dragOver = true"
            @dragleave.prevent="dragOver = false"
            @drop.prevent="onDrop"
          >
            <el-icon class="dropzone-icon"><Upload /></el-icon>
            <div class="dropzone-text">
              {{ uploading ? "上传中..." : "点击或拖拽 PDF 到此处（≤20MB）" }}
            </div>
          </div>
          <input
            ref="fileInput"
            type="file"
            accept="application/pdf"
            style="display: none"
            @change="onFileSelected"
          />
        </el-form-item>

        <el-form-item label="备注">
          <el-input v-model="remark" type="textarea" :rows="3" maxlength="500" show-word-limit />
        </el-form-item>
      </el-form>
    </el-card>

    <div class="footer-actions">
      <el-button @click="goBack">返回</el-button>
      <el-button v-if="!isReadOnly" type="primary" :loading="submitting" @click="handleSubmit">
        {{ isEdit ? "保存" : "创建" }}
      </el-button>
    </div>
  </div>
</template>

<style scoped>
.tp-detail {
  padding: 16px;
}
.card-header {
  display: flex;
  align-items: center;
  gap: 12px;
}
.page-title {
  font-weight: 600;
  font-size: 16px;
}
.footer-actions {
  margin-top: 16px;
  display: flex;
  justify-content: flex-end;
  gap: 12px;
}
.file-info {
  display: flex;
  align-items: center;
}
.dropzone {
  width: 320px;
  height: 120px;
  border: 2px dashed #dcdfe6;
  border-radius: 8px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  color: #909399;
  transition: border-color 0.2s, background-color 0.2s;
}
.dropzone:hover,
.dropzone.is-dragover {
  border-color: #c00000;
  background-color: #fef0f0;
  color: #c00000;
}
.dropzone-icon {
  font-size: 32px;
  margin-bottom: 8px;
}
.dropzone-text {
  font-size: 13px;
}
</style>
