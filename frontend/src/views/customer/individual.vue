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
  name: "CustomerIndividual"
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
  customer_type: "individual" as const,
  real_name: "",
  phone: "",
  id_card: "",
  gender: "男",
  birth_date: "",
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

const columns = [
  { prop: "id", label: "ID", width: 70 },
  { prop: "real_name", label: "姓名", width: 130 },
  { prop: "phone", label: "电话", width: 130 },
  { prop: "id_card", label: "身份证号", width: 200 },
  { prop: "gender", label: "性别", width: 70 },
  { prop: "birth_date", label: "出生日期", width: 110 },
  { prop: "email", label: "邮箱", width: 180 },
  { prop: "address", label: "地址", width: 220 },
  { prop: "remarks", label: "备注", width: 180 },
  { prop: "created_at", label: "创建时间", width: 170 }
];

// ==================== Data loading ====================
const loadCustomers = async () => {
  loading.value = true;
  try {
    const keyword = searchKeyword.value.trim();
    const res = keyword
      ? await searchCustomersByType("individual", keyword, page.value, pageSize.value)
      : await getCustomersByType("individual", page.value, pageSize.value);
    if (res.success) {
      customerList.value = res.data.list;
      total.value = res.data.total;
    }
  } catch (error) {
    const msg = (error as any)?.response?.data?.message || (error as any)?.message || "加载个人客户列表失败";
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
  dialogTitle.value = "新建个人客户";
  form.value = {
    customer_type: "individual",
    real_name: "",
    phone: "",
    id_card: "",
    gender: "男",
    birth_date: "",
    email: "",
    address: "",
    remarks: ""
  };
  dialogVisible.value = true;
};

const openEditDialog = (row: Customer) => {
  isEdit.value = true;
  dialogTitle.value = "编辑个人客户";
  currentCustomerId.value = row.id;
  form.value = {
    customer_type: "individual",
    real_name: row.real_name,
    phone: row.phone,
    id_card: row.id_card,
    gender: row.gender || "男",
    birth_date: row.birth_date,
    email: row.email,
    address: row.address,
    remarks: row.remarks
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

const handleSubmit = async () => {
  if (!form.value.real_name) {
    ElMessage.warning("请填写姓名");
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
  if (form.value.id_card && !validateIdCard(form.value.id_card)) {
    ElMessage.warning("身份证号格式不正确");
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
  try {
    await ElMessageBox.confirm(`确定要删除客户"${row.real_name}"吗？`, "警告", {
      confirmButtonText: "确定",
      cancelButtonText: "取消",
      type: "warning"
    });
    await deleteCustomer(row.id);
    ElMessage.success("删除成功");
    loadCustomers();
  } catch (error: unknown) {
    if (error !== "cancel" && error !== "close") {
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
      customer_name: row.real_name || ""
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
          <span>个人客户管理</span>
          <div class="header-actions">
            <el-input
              v-model="searchKeyword"
              placeholder="搜索姓名/电话/身份证/地址"
              style="width: 280px; margin-right: 10px"
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
              新建个人客户
            </el-button>
          </div>
        </div>
      </template>

      <el-table
        :data="customerList"
        v-loading="loading"
        stripe
        :empty-text="loading ? '加载中...' : '暂无个人客户'"
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
      <el-form :model="form" label-width="110px">
        <el-form-item label="姓名" required>
          <el-input v-model="form.real_name" placeholder="请输入姓名" />
        </el-form-item>
        <el-form-item label="电话" required>
          <el-input v-model="form.phone" placeholder="请输入 11 位手机号" />
        </el-form-item>
        <el-form-item label="身份证号">
          <el-input
            v-model="form.id_card"
            placeholder="请输入 18 位身份证号"
            maxlength="18"
          />
        </el-form-item>
        <el-form-item label="性别">
          <el-radio-group v-model="form.gender">
            <el-radio value="男">男</el-radio>
            <el-radio value="女">女</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="出生日期">
          <el-date-picker
            v-model="form.birth_date"
            type="date"
            placeholder="选择日期"
            format="YYYY-MM-DD"
            value-format="YYYY-MM-DD"
            style="width: 100%"
          />
        </el-form-item>
        <el-form-item label="邮箱">
          <el-input v-model="form.email" placeholder="请输入邮箱" />
        </el-form-item>
        <el-form-item label="地址">
          <el-input
            v-model="form.address"
            type="textarea"
            :rows="2"
            placeholder="请输入地址"
          />
        </el-form-item>
        <el-form-item label="备注">
          <el-input
            v-model="form.remarks"
            type="textarea"
            :rows="3"
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
