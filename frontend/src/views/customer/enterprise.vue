<script setup lang="ts">
import { ref, onMounted } from "vue";
import { useRouter } from "vue-router";
import { ElMessage, ElMessageBox } from "element-plus";
import {
  getCustomersByType,
  searchCustomersByType,
  createCustomerExt,
  updateCustomerExt,
  deleteCustomer,
  type Customer,
  type CustomerInput
} from "@/api/customer";
import { formatTimestampLang } from "@/utils/date";
// 第十三阶段 v4：提醒订阅入口（列表层）
import SubscriptionDialog from "@/views/system/reminder/components/SubscriptionDialog.vue";

defineOptions({
  name: "CustomerEnterprise"
});

const router = useRouter();

const loading = ref(false);
const customerList = ref<Customer[]>([]);
const searchKeyword = ref("");
const total = ref(0);
const page = ref(1);
const pageSize = ref(10);

const dialogVisible = ref(false);
const dialogTitle = ref("");
const isEdit = ref(false);
const currentCustomerId = ref<number | null>(null);

const form = ref({
  customer_type: "enterprise" as const,
  company_name: "",
  phone: "",
  uscc: "",
  legal_person: "",
  legal_person_id_card: "",
  registered_capital: "",
  company_type: "",
  industry: "",
  established_date: "",
  business_scope: "",
  website: "",
  email: "",
  address: "",
  remarks: ""
});

// ==================== Validators ====================
const validatePhone = (phone: string): boolean => /^1[3-9]\d{9}$/.test(phone);
const validateIdCard = (idCard: string): boolean => {
  if (!idCard) return true;
  return /^[1-9]\d{5}(18|19|20)\d{2}(0[1-9]|1[0-2])(0[1-9]|[12]\d|3[01])\d{3}[0-9Xx]$/.test(idCard);
};
const validateEmail = (email: string): boolean => {
  if (!email) return true;
  return /^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$/.test(email);
};

// USCC per GB 32100-2015
const USCC_ALPHABET = "0123456789ABCDEFGHJKLMNPQRTUWXY";
const USCC_WEIGHTS = [1, 3, 9, 27, 19, 26, 16, 17, 20, 29, 25, 13, 8, 24, 10, 30, 28];
const validateUSCC = (uscc: string): boolean => {
  if (!uscc) return true;
  if (uscc.length !== 18) return false;
  const code = uscc.toUpperCase();
  if (USCC_ALPHABET.indexOf(code[17]) < 0) return false;
  let sum = 0;
  for (let i = 0; i < 17; i++) {
    const idx = USCC_ALPHABET.indexOf(code[i]);
    if (idx < 0) return false;
    sum += idx * USCC_WEIGHTS[i];
  }
  const checkVal = (31 - (sum % 31)) % 31;
  return checkVal === USCC_ALPHABET.indexOf(code[17]);
};

const columns = [
  { prop: "id", label: "ID", width: 70 },
  { prop: "company_name", label: "企业名称", width: 200 },
  { prop: "uscc", label: "统一社会信用代码", width: 200 },
  { prop: "legal_person", label: "法定代表人", width: 110 },
  { prop: "company_type", label: "企业类型", width: 130 },
  { prop: "industry", label: "所属行业", width: 120 },
  { prop: "phone", label: "电话", width: 130 },
  { prop: "registered_capital", label: "注册资本", width: 130 },
  { prop: "established_date", label: "成立日期", width: 110 },
  { prop: "created_at", label: "创建时间", width: 170 }
];

// ==================== Data loading ====================
const loadCustomers = async () => {
  loading.value = true;
  try {
    const keyword = searchKeyword.value.trim();
    const res = keyword
      ? await searchCustomersByType("enterprise", keyword, page.value, pageSize.value)
      : await getCustomersByType("enterprise", page.value, pageSize.value);
    if (res.success) {
      customerList.value = res.data.list;
      total.value = res.data.total;
    }
  } catch (error) {
    const msg = (error as any)?.response?.data?.message || (error as any)?.message || "加载企业客户列表失败";
    ElMessage.error(msg);
  } finally {
    loading.value = false;
  }
};

const handleSearch = () => {
  page.value = 1;
  loadCustomers();
};

const handlePageChange = (newPage: number) => {
  page.value = newPage;
  loadCustomers();
};

const handleSizeChange = (newSize: number) => {
  pageSize.value = newSize;
  page.value = 1;
  loadCustomers();
};

// ==================== Dialogs ====================
const openCreateDialog = () => {
  isEdit.value = false;
  dialogTitle.value = "新建企业客户";
  form.value = {
    customer_type: "enterprise",
    company_name: "",
    phone: "",
    uscc: "",
    legal_person: "",
    legal_person_id_card: "",
    registered_capital: "",
    company_type: "",
    industry: "",
    established_date: "",
    business_scope: "",
    website: "",
    email: "",
    address: "",
    remarks: ""
  };
  dialogVisible.value = true;
};

// ==================== 第十三阶段 v4：提醒订阅入口（列表层） ====================
const reminderDialogVisible = ref(false);
const reminderLinkID = ref<number | null>(null);
function openReminderDialog(row: Customer) {
  if (!row.id || row.id <= 0) {
    ElMessage.warning("客户 ID 无效");
    return;
  }
  reminderLinkID.value = row.id;
  reminderDialogVisible.value = true;
}
function closeReminderDialog() {
  reminderDialogVisible.value = false;
  reminderLinkID.value = null;
}

const openEditDialog = (row: Customer) => {
  isEdit.value = true;
  dialogTitle.value = "编辑企业客户";
  currentCustomerId.value = row.id;
  form.value = {
    customer_type: "enterprise",
    company_name: row.company_name,
    phone: row.phone,
    uscc: row.uscc,
    legal_person: row.legal_person,
    legal_person_id_card: row.legal_person_id_card,
    registered_capital: row.registered_capital,
    company_type: row.company_type,
    industry: row.industry,
    established_date: row.established_date,
    business_scope: row.business_scope,
    website: row.website,
    email: row.email,
    address: row.address,
    remarks: row.remarks
  };
  dialogVisible.value = true;
};

const handleSubmit = async () => {
  if (!form.value.company_name) {
    ElMessage.warning("请填写企业名称");
    return;
  }
  if (!form.value.phone) {
    ElMessage.warning("请填写电话");
    return;
  }
  if (!validatePhone(form.value.phone)) {
    ElMessage.warning("电话格式不正确，请输入 11 位手机号");
    return;
  }
  if (form.value.uscc && !validateUSCC(form.value.uscc)) {
    ElMessage.warning("统一社会信用代码无效（长度 18 位，且校验位必须正确）");
    return;
  }
  if (form.value.legal_person_id_card && !validateIdCard(form.value.legal_person_id_card)) {
    ElMessage.warning("法人身份证号格式不正确");
    return;
  }
  if (form.value.email && !validateEmail(form.value.email)) {
    ElMessage.warning("邮箱格式不正确");
    return;
  }

  const payload: CustomerInput = { ...form.value } as CustomerInput;
  for (const k of Object.keys(payload)) {
    if ((payload as any)[k] === undefined || (payload as any)[k] === null) {
      (payload as any)[k] = "";
    }
  }

  try {
    if (isEdit.value && currentCustomerId.value) {
      await updateCustomerExt(currentCustomerId.value, payload);
      ElMessage.success("更新成功");
    } else {
      await createCustomerExt(payload);
      ElMessage.success("创建成功");
    }
    dialogVisible.value = false;
    loadCustomers();
  } catch (error: any) {
    const msg = error?.response?.data?.message || (isEdit.value ? "更新失败" : "创建失败");
    ElMessage.error(msg);
  }
};

const handleDelete = async (row: Customer) => {
  const displayName = row.company_name || row.real_name || `客户#${row.id}`;
  try {
    await ElMessageBox.confirm(`确定要删除客户"${displayName}"吗？`, "警告", {
      confirmButtonText: "确定",
      cancelButtonText: "取消",
      type: "warning"
    });
    await deleteCustomer(row.id);
    ElMessage.success("删除成功");
    loadCustomers();
  } catch (error: unknown) {
    if (error !== "cancel" && error !== "close") {
      // 优先展示后端返回的具体业务错误（如"存在进行中合同"）
      const errMsg = (error as { response?: { data?: { message?: string } } })
        ?.response?.data?.message;
      ElMessage.error(errMsg || "删除失败");
    }
  }
};

const goToContracts = (row: Customer) => {
  router.push(`/customer/contract?id=${row.id}`);
};

const goToMedia = (row: Customer) => {
  router.push({
    path: "/media/library",
    query: {
      customer_id: String(row.id),
      customer_name: row.company_name || ""
    }
  });
};

onMounted(() => {
  loadCustomers();
});
</script>

<template>
  <div class="customer-container">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>企业客户管理</span>
          <div class="header-actions">
            <el-input
              v-model="searchKeyword"
              placeholder="搜索企业名/统一社会信用代码/法人/电话"
              style="width: 320px; margin-right: 10px"
              clearable
              @keyup.enter="handleSearch"
            />
            <el-button type="primary" @click="handleSearch">
              <i class="ri-search-line" style="margin-right: 4px"></i>
              搜索
            </el-button>
            <el-button @click="loadCustomers">
              <i class="ri-refresh-line" style="margin-right: 4px"></i>
              刷新
            </el-button>
            <el-button type="primary" @click="openCreateDialog">
              <i class="ri-add-line" style="margin-right: 4px"></i>
              新建企业客户
            </el-button>
          </div>
        </div>
      </template>

      <el-table
        :data="customerList"
        v-loading="loading"
        stripe
        :empty-text="loading ? '加载中...' : '暂无企业客户'"
      >
        <el-table-column
          v-for="col in columns"
          :key="col.prop"
          :prop="col.prop"
          :label="col.label"
          :width="col.width"
          :show-overflow-tooltip="true"
        >
          <template #default="{ row }" v-if="col.prop === 'created_at'">
            {{ formatTimestampLang(row.created_at) }}
          </template>
          <template #default="{ row }" v-else-if="col.prop === 'company_type'">
            {{ row.company_type || "-" }}
          </template>
        </el-table-column>
        <el-table-column label="操作" fixed="right" minWidth="260">
          <template #default="{ row }">
            <el-button link type="primary" @click="goToMedia(row)">
              媒体
            </el-button>
            <el-button link type="primary" @click="goToContracts(row)">
              文档
            </el-button>
            <el-button link type="warning" @click="openReminderDialog(row)">
              提醒
            </el-button>
            <el-button link type="primary" @click="openEditDialog(row)">
              编辑
            </el-button>
            <el-button link type="danger" @click="handleDelete(row)">
              删除
            </el-button>
          </template>
        </el-table-column>
      </el-table>

      <div class="pagination">
        <el-pagination
          v-model:current-page="page"
          v-model:page-size="pageSize"
          :page-sizes="[10, 20, 50, 100]"
          :total="total"
          layout="total, sizes, prev, pager, next, jumper"
          @current-change="handlePageChange"
          @size-change="handleSizeChange"
        />
      </div>
    </el-card>

    <el-dialog v-model="dialogVisible" :title="dialogTitle" width="640px">
      <el-form :model="form" label-width="130px">
        <el-form-item label="企业名称" required>
          <el-input v-model="form.company_name" placeholder="请输入企业/商户全称" />
        </el-form-item>
        <el-form-item label="统一社会信用代码">
          <el-input
            v-model="form.uscc"
            placeholder="18 位统一社会信用代码（选填，将自动校验）"
            maxlength="18"
          />
        </el-form-item>
        <el-form-item label="法定代表人">
          <el-input v-model="form.legal_person" placeholder="请输入法人姓名" />
        </el-form-item>
        <el-form-item label="法人身份证号">
          <el-input
            v-model="form.legal_person_id_card"
            placeholder="请输入 18 位法人身份证号"
            maxlength="18"
          />
        </el-form-item>
        <el-form-item label="注册资本">
          <el-input v-model="form.registered_capital" placeholder="如：100 万人民币" />
        </el-form-item>
        <el-form-item label="企业类型">
          <el-select
            v-model="form.company_type"
            placeholder="请选择企业类型"
            style="width: 100%"
            clearable
          >
            <el-option label="有限责任公司" value="有限责任公司" />
            <el-option label="股份有限公司" value="股份有限公司" />
            <el-option label="个体工商户" value="个体工商户" />
            <el-option label="合伙企业" value="合伙企业" />
            <el-option label="个人独资企业" value="个人独资企业" />
            <el-option label="其他" value="其他" />
          </el-select>
        </el-form-item>
        <el-form-item label="所属行业">
          <el-input v-model="form.industry" placeholder="如：信息技术、房地产" />
        </el-form-item>
        <el-form-item label="成立日期">
          <el-date-picker
            v-model="form.established_date"
            type="date"
            placeholder="选择日期"
            format="YYYY-MM-DD"
            value-format="YYYY-MM-DD"
            style="width: 100%"
          />
        </el-form-item>
        <el-form-item label="经营范围">
          <el-input
            v-model="form.business_scope"
            type="textarea"
            :rows="2"
            placeholder="请输入经营范围"
          />
        </el-form-item>
        <el-form-item label="官网">
          <el-input v-model="form.website" placeholder="https://" />
        </el-form-item>
        <el-form-item label="联系电话" required>
          <el-input v-model="form.phone" placeholder="请输入 11 位手机号" />
        </el-form-item>
        <el-form-item label="邮箱">
          <el-input v-model="form.email" placeholder="请输入邮箱" />
        </el-form-item>
        <el-form-item label="办公地址">
          <el-input
            v-model="form.address"
            type="textarea"
            :rows="2"
            placeholder="请输入办公地址"
          />
        </el-form-item>
        <el-form-item label="备注">
          <el-input
            v-model="form.remarks"
            type="textarea"
            :rows="2"
            placeholder="请输入备注"
          />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleSubmit">确定</el-button>
      </template>
    </el-dialog>

    <!-- 第十三阶段 v4：提醒订阅 Dialog（列表层入口） -->
    <SubscriptionDialog
      v-model:visible="reminderDialogVisible"
      link-type="customer"
      :link-id="reminderLinkID || 0"
      @created="closeReminderDialog"
    />
  </div>
</template>

<style scoped>
.customer-container {
  padding: 20px;
}
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.header-actions {
  display: flex;
  align-items: center;
}
.pagination {
  display: flex;
  justify-content: flex-end;
  margin-top: 20px;
}
</style>
