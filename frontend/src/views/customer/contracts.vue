<script setup lang="ts">
import { ref, computed, onMounted } from "vue";
import { useRoute, useRouter } from "vue-router";
import { ElMessage } from "element-plus";
import {
  getCustomerById,
  type Customer
} from "@/api/customer";
import ThirdPartyContractList from "@/views/contract/components/ThirdPartyContractList.vue";
import {
  createThirdPartyContract,
  uploadThirdPartyContractPdf,
  bulkDownloadThirdPartyContracts,
  type ThirdPartyContract
} from "@/api/third_party";
import { calculateAge } from "@/utils/date";
import { Upload } from "@element-plus/icons-vue";

defineOptions({
  name: "CustomerContracts"
});

const route = useRoute();
const router = useRouter();

// ============ 客户信息 ============
// 用 computed 直接从 route 算 id（不再 ref + onMounted 异步赋值，
// 解决子组件首次 mount 拿不到 id 而拉全量的时序问题）。
// 无效 id（undefined / 非数字 / ≤0）→ 0，子组件据此走"全量获取"分支。
const customerId = computed(() => {
  const n = parseInt(route.query.id as string, 10);
  return Number.isFinite(n) && n > 0 ? n : 0;
});
const customer = ref<Customer | null>(null);

// ============ 关联文档列表 tab（纯本地状态） ============
type TabKey = "third";
const activeTab = ref<TabKey>("third");

const addButtonLabel = computed(() => "添加文档");
const showAddButton = computed(() => true);

// ============ 文档创建模态 ============
const thirdCreateModalOpen = ref(false);
const tcForm = ref({
  title: "",
  type: "paper" as "paper" | "electronic",
  amount: 0,
  currency: "CNY",
  sign_date: "" as string,
  start_date: "" as string,
  end_date: "" as string,
  remark: ""
});
const tcPdfBase64 = ref("");
const tcPdfSize = ref(0);
const tcDragOver = ref(false);
const tcFileInput = ref<HTMLInputElement | null>(null);
const tcSubmitting = ref(false);

function initThirdCreateForm() {
  tcForm.value = {
    title: "",
    type: "paper",
    amount: 0,
    currency: "CNY",
    sign_date: "",
    start_date: "",
    end_date: "",
    remark: ""
  };
  tcPdfBase64.value = "";
  tcPdfSize.value = 0;
  tcDragOver.value = false;
}

function tsFromDate(s: string): number {
  if (!s) return 0;
  const d = new Date(s);
  return isNaN(d.getTime()) ? 0 : Math.floor(d.getTime() / 1000);
}

// ============ 客户信息展示辅助 ============
const customerType = computed<"individual" | "enterprise">(
  () => (customer.value?.customer_type as "individual" | "enterprise") || "individual"
);
const isEnterprise = computed(() => customerType.value === "enterprise");
const isIndividual = computed(() => customerType.value === "individual");
const displayName = computed<string>(() => {
  if (!customer.value) return "";
  if (isEnterprise.value) {
    return customer.value.company_name || customer.value.real_name || customer.value.phone || "";
  }
  return customer.value.real_name || customer.value.phone || "";
});
const customerTypeLabel = computed(() => (isEnterprise.value ? "企业" : "个人"));

// ============ 数据加载 ============
const loadCustomer = async () => {
  // 无有效 id → 不拉客户信息，子组件会全量获取（错误处理）
  if (customerId.value <= 0) {
    customer.value = null;
    return;
  }
  try {
    const res = await getCustomerById(customerId.value);
    if (res.success) {
      customer.value = res.data;
    }
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.message || e?.message || e || "加载客户信息失败");
  }
};

// ============ 文档：添加弹窗 ============
function openThirdCreateModal() {
  initThirdCreateForm();
  thirdCreateModalOpen.value = true;
}

function tcPickFile() {
  tcFileInput.value?.click();
}

function tcOnFileSelected(e: Event) {
  const target = e.target as HTMLInputElement;
  const file = target.files?.[0];
  if (file) handleTcFile(file);
  if (target) target.value = "";
}

function tcOnDrop(e: DragEvent) {
  tcDragOver.value = false;
  const file = e.dataTransfer?.files?.[0];
  if (file) handleTcFile(file);
}

async function handleTcFile(file: File) {
  if (file.type !== "application/pdf") {
    ElMessage.warning("只支持 PDF 文件");
    return;
  }
  if (file.size > 20 * 1024 * 1024) {
    ElMessage.warning("PDF 超过 20MB");
    return;
  }
  tcPdfBase64.value = await fileToBase64(file);
  tcPdfSize.value = file.size;
  ElMessage.success(`已选择：${(file.size / 1024).toFixed(1)} KB`);
}

function fileToBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(reader.result as string);
    reader.onerror = reject;
    reader.readAsDataURL(file);
  });
}

async function handleThirdCreateSubmit() {
  if (!tcForm.value.title.trim()) {
    ElMessage.warning("请输入标题");
    return;
  }
  if (customerId.value <= 0) {
    ElMessage.warning("缺少 customer_id");
    return;
  }
  if (!tcPdfBase64.value) {
    ElMessage.warning("请上传已签署的 PDF");
    return;
  }
  tcSubmitting.value = true;
  try {
    // 1) 建骨架
    const createRes: any = await createThirdPartyContract({
      title: tcForm.value.title.trim(),
      type: tcForm.value.type,
      status: "draft",
      customer_id: customerId.value,
      amount: tcForm.value.amount,
      currency: tcForm.value.currency,
      sign_date: tsFromDate(tcForm.value.sign_date),
      start_date: tsFromDate(tcForm.value.start_date),
      end_date: tsFromDate(tcForm.value.end_date),
      file_path: "pending://upload-on-submit",
      file_size: tcPdfSize.value,
      file_sm3_hash: "",
      file_sha256_hash: "",
      file_combined_hash: "",
      remark: tcForm.value.remark
    });
    if (!createRes.success) {
      ElMessage.error(createRes.message || "创建失败");
      return;
    }
    const newId = createRes.data.id;
    // 2) 立即上传 PDF
    const upRes: any = await uploadThirdPartyContractPdf(newId, tcPdfBase64.value);
    if (!upRes.success) {
      ElMessage.warning("基本信息已创建，但 PDF 上传失败：" + (upRes.message || ""));
    } else {
      ElMessage.success("已创建并上传 PDF");
    }
    thirdCreateModalOpen.value = false;
    // 通知第三方列表刷新（用 key 强制重挂载 + emit edit 走 router 不可靠）
    // 直接刷新页面最稳，但 v5.2 改用 emit-based
  } catch (err: any) {
    ElMessage.error(err?.response?.data?.message || err?.message || err || "请求失败");
  } finally {
    tcSubmitting.value = false;
  }
}

// ============ 顶部"添加文档"按钮 ============
function handleAdd() {
  openThirdCreateModal();
}

// ============ 打包下载 ============
const handleBatchDownload = async () => {
  if (!customerId.value) return;
  try {
    const blob = (await bulkDownloadThirdPartyContracts({
      customer_id: customerId.value
    })) as Blob;
    const url = window.URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `third_party_customer_${customerId.value}_${Date.now()}.zip`;
    document.body.appendChild(a);
    a.click();
    window.URL.revokeObjectURL(url);
    document.body.removeChild(a);
    ElMessage.success("已下载 ZIP（该客户所有文档）");
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.message || e?.message || e || "打包下载失败");
  }
};

onMounted(() => {
  loadCustomer();
});
</script>

<template>
  <div class="contracts-container">
    <el-page-header @back="router.back()" content="客户文档管理">
      <template #extra>
        <el-button @click="handleBatchDownload">打包下载</el-button>
        <el-button v-if="showAddButton" type="primary" @click="handleAdd">
          {{ addButtonLabel }}
        </el-button>
      </template>
    </el-page-header>

    <!-- 客户信息卡 -->
    <el-card v-if="customer" style="margin-top: 20px">
      <template #header>
        <div class="customer-card-header">
          <span>客户信息</span>
          <el-tag
            :type="isEnterprise ? 'warning' : 'success'"
            size="small"
            effect="light"
          >
            {{ customerTypeLabel }}
          </el-tag>
        </div>
      </template>

      <el-descriptions v-if="isIndividual" :column="3" border>
        <el-descriptions-item label="姓名">{{ customer.real_name }}</el-descriptions-item>
        <el-descriptions-item label="电话">{{ customer.phone }}</el-descriptions-item>
        <el-descriptions-item label="身份证号">{{ customer.id_card || "-" }}</el-descriptions-item>
        <el-descriptions-item label="性别">{{ customer.gender || "-" }}</el-descriptions-item>
        <el-descriptions-item label="年龄">{{ calculateAge(customer.birth_date) }}</el-descriptions-item>
        <el-descriptions-item label="出生日期">{{ customer.birth_date || "-" }}</el-descriptions-item>
        <el-descriptions-item label="邮箱" :span="2">{{ customer.email || "-" }}</el-descriptions-item>
        <el-descriptions-item label="地址" :span="2">{{ customer.address || "-" }}</el-descriptions-item>
        <el-descriptions-item label="备注" :span="3">{{ customer.remarks || "-" }}</el-descriptions-item>
      </el-descriptions>

      <el-descriptions v-else :column="3" border>
        <el-descriptions-item label="企业名称" :span="2">
          <strong>{{ customer.company_name || customer.real_name || "-" }}</strong>
        </el-descriptions-item>
        <el-descriptions-item label="统一社会信用代码">
          <span style="font-family: monospace">{{ customer.uscc || "-" }}</span>
        </el-descriptions-item>
        <el-descriptions-item label="企业类型">{{ customer.company_type || "-" }}</el-descriptions-item>
        <el-descriptions-item label="法定代表人">{{ customer.legal_person || "-" }}</el-descriptions-item>
        <el-descriptions-item label="法人身份证号">{{ customer.legal_person_id_card || "-" }}</el-descriptions-item>
        <el-descriptions-item label="注册资本">{{ customer.registered_capital || "-" }}</el-descriptions-item>
        <el-descriptions-item label="所属行业">{{ customer.industry || "-" }}</el-descriptions-item>
        <el-descriptions-item label="成立日期">{{ customer.established_date || "-" }}</el-descriptions-item>
        <el-descriptions-item label="联系电话">{{ customer.phone || "-" }}</el-descriptions-item>
        <el-descriptions-item label="邮箱" :span="2">{{ customer.email || "-" }}</el-descriptions-item>
        <el-descriptions-item label="官网" :span="3">
          <a v-if="customer.website" :href="customer.website" target="_blank" rel="noopener">
            {{ customer.website }}
          </a>
          <span v-else>-</span>
        </el-descriptions-item>
        <el-descriptions-item label="经营范围" :span="3">
          <div style="white-space: pre-wrap">{{ customer.business_scope || "-" }}</div>
        </el-descriptions-item>
        <el-descriptions-item label="办公地址" :span="3">{{ customer.address || "-" }}</el-descriptions-item>
        <el-descriptions-item label="备注" :span="3">{{ customer.remarks || "-" }}</el-descriptions-item>
      </el-descriptions>
    </el-card>

    <!-- 关联文档列表 -->
    <el-card style="margin-top: 20px">
      <template #header>
        <div class="docs-header">
          <span>关联文档列表</span>
        </div>
      </template>

      <el-tabs v-model="activeTab" class="docs-tabs">
        <el-tab-pane label="关联文档" name="third">
          <ThirdPartyContractList :customer-id="customerId" />
        </el-tab-pane>
      </el-tabs>
    </el-card>

    <!-- 添加文档：页面内模态 -->
    <el-dialog
      v-model="thirdCreateModalOpen"
      title="添加文档"
      width="640px"
      :close-on-click-modal="false"
      @closed="initThirdCreateForm"
    >
      <el-form label-width="100px">
        <el-form-item label="对方">
          <el-tag>{{ displayName }} (id={{ customerId }})</el-tag>
        </el-form-item>
        <el-form-item label="标题" required>
          <el-input v-model="tcForm.title" maxlength="200" show-word-limit />
        </el-form-item>
        <el-form-item label="类型">
          <el-radio-group v-model="tcForm.type">
            <el-radio-button label="paper">纸质</el-radio-button>
            <el-radio-button label="electronic">电子</el-radio-button>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="金额">
          <el-input-number
            v-model="tcForm.amount"
            :min="0"
            :precision="2"
            :step="1000"
            style="width: 200px"
          />
          <el-select v-model="tcForm.currency" style="width: 100px; margin-left: 8px">
            <el-option label="CNY" value="CNY" />
            <el-option label="USD" value="USD" />
            <el-option label="EUR" value="EUR" />
          </el-select>
        </el-form-item>
        <el-form-item label="签署日期">
          <el-date-picker
            v-model="tcForm.sign_date"
            type="date"
            placeholder="选择签署日期"
            value-format="YYYY-MM-DD"
            style="width: 200px"
          />
        </el-form-item>
        <el-form-item label="生效日期">
          <el-date-picker
            v-model="tcForm.start_date"
            type="date"
            placeholder="选择生效日期"
            value-format="YYYY-MM-DD"
            style="width: 200px"
          />
        </el-form-item>
        <el-form-item label="到期日期">
          <el-date-picker
            v-model="tcForm.end_date"
            type="date"
            placeholder="选择到期日期"
            value-format="YYYY-MM-DD"
            style="width: 200px"
          />
        </el-form-item>
        <el-form-item label="已签署 PDF" required>
          <div v-if="tcPdfSize > 0" class="file-info">
            <el-tag type="success" size="small">
              已选择 {{ (tcPdfSize / 1024).toFixed(1) }} KB
            </el-tag>
            <el-button link type="primary" size="small" style="margin-left: 8px" @click="tcPickFile">
              重新选择
            </el-button>
          </div>
          <div
            v-else
            class="dropzone"
            :class="{ 'is-dragover': tcDragOver }"
            @click="tcPickFile"
            @dragover.prevent="tcDragOver = true"
            @dragleave.prevent="tcDragOver = false"
            @drop.prevent="tcOnDrop"
          >
            <el-icon class="dropzone-icon"><Upload /></el-icon>
            <div class="dropzone-text">
              点击或拖拽 PDF 到此处（≤20MB）
            </div>
          </div>
          <input
            ref="tcFileInput"
            type="file"
            accept="application/pdf"
            style="display: none"
            @change="tcOnFileSelected"
          />
        </el-form-item>
        <el-form-item label="备注">
          <el-input v-model="tcForm.remark" type="textarea" :rows="2" maxlength="500" />
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="thirdCreateModalOpen = false">取消</el-button>
        <el-button
          type="primary"
          :loading="tcSubmitting"
          @click="handleThirdCreateSubmit"
        >
          创建并上传
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.contracts-container {
  padding: 20px;
}
.customer-card-header {
  display: flex;
  align-items: center;
  gap: 10px;
}
.docs-header {
  display: flex;
  align-items: center;
}
.docs-tabs :deep(.el-tabs__header) {
  margin-bottom: 12px;
}
.file-info {
  display: flex;
  align-items: center;
}
.dropzone {
  width: 100%;
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
