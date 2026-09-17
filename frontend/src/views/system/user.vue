<template>
  <div class="user-container">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>员工管理</span>
          <div class="header-actions">
            <el-input
              v-model="searchKeyword"
              placeholder="搜索用户名/昵称/真实姓名/工号/邮箱"
              style="width: 250px; margin-right: 10px"
              clearable
              @keyup.enter="handleSearch"
            />
            <el-button type="primary" @click="handleSearch">
              <i class="ri-search-line" style="margin-right: 4px"></i>
              搜索
            </el-button>
            <el-button @click="loadUsers">
              <i class="ri-refresh-line" style="margin-right: 4px"></i>
              刷新
            </el-button>
            <el-button type="primary" @click="handleCreate">
              添加用户
            </el-button>
          </div>
        </div>
      </template>

      <el-table :data="userList" stripe style="width: 100%">
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="username" label="用户名" width="120" />
        <el-table-column prop="nickname" label="昵称" width="120" />
        <el-table-column prop="real_name" label="真实姓名" width="120" />
        <el-table-column prop="department_name" label="部门" width="150">
          <template #default="{ row }">
            <el-tag v-if="row.department_name" type="info">
              {{ row.department_name }}
            </el-tag>
            <span v-else type="info">-</span>
          </template>
        </el-table-column>
        <el-table-column prop="position" label="职位" width="120" />
        <el-table-column prop="employee_no" label="工号" width="120" />
        <el-table-column prop="email" label="邮箱" min-width="180" />
        <el-table-column prop="phone" label="电话" width="130" />
        <el-table-column label="状态" width="100">
          <template #default="{ row }">
            <el-tag :type="row.status === 'active' ? 'success' : 'danger'" size="small">
              {{ row.status === "active" ? "启用" : "停用" }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="角色" width="180">
          <template #default="{ row }">
            <el-tag
              v-for="r in (row.roles || [])"
              :key="r"
              size="small"
              style="margin-right: 4px"
            >
              {{ r }}
            </el-tag>
            <span v-if="!row.roles || row.roles.length === 0" style="color: #c0c4cc">-</span>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="180" fixed="right">
          <template #default="{ row }">
            <span class="op-cell">
              <el-button link type="primary" size="small" @click="handleEdit(row)">
                编辑
              </el-button>
              <el-button link type="primary" size="small" @click="handleChangeDepartment(row)">
                调整部门
              </el-button>
              <el-dropdown trigger="click" @command="(cmd: string) => {
                if (cmd === 'roles') handleAssignRoles(row);
                else if (cmd === 'disable') handleToggleStatus(row);
                else if (cmd === 'enable') handleToggleStatus(row);
                else if (cmd === 'delete') handleDelete(row);
              }">
                <el-button type="primary" link size="small" class="op-more">
                  更多<el-icon class="el-icon--right"><ArrowDown /></el-icon>
                </el-button>
                <template #dropdown>
                  <el-dropdown-menu>
                    <el-dropdown-item :disabled="row.username === 'admin'" command="roles">
                      分配角色
                    </el-dropdown-item>
                    <el-dropdown-item v-if="row.username !== 'admin'" command="disable" divided>
                      {{ row.status === 'active' ? '禁用' : '启用' }}
                    </el-dropdown-item>
                    <el-dropdown-item v-if="row.username !== 'admin'" command="delete" divided style="color: var(--el-color-danger)">
                      删除
                    </el-dropdown-item>
                  </el-dropdown-menu>
                </template>
              </el-dropdown>
            </span>
          </template>
        </el-table-column>
      </el-table>

      <div class="pagination-container">
        <el-pagination
          v-model:current-page="currentPage"
          v-model:page-size="pageSize"
          :page-sizes="[10, 20, 50, 100]"
          :total="total"
          layout="total, sizes, prev, pager, next, jumper"
          @size-change="handleSizeChange"
          @current-change="handleCurrentChange"
        />
      </div>
    </el-card>

    <el-dialog
      v-model="dialogVisible"
      :title="dialogTitle"
      width="600px"
      @close="handleDialogClose"
    >
      <el-form :model="formData" label-width="100px">
        <el-form-item label="用户名" required>
          <el-input
            v-model="formData.username"
            placeholder="请输入用户名"
            :disabled="formData.username === 'admin'"
            @blur="handleUsernameBlur"
          />
        </el-form-item>

        <el-form-item v-if="!currentId" label="密码" required>
          <el-input v-model="formData.password" type="password" placeholder="请输入密码（至少6位）" show-password />
        </el-form-item>
        <el-form-item v-else label="新密码">
          <el-input v-model="formData.password" type="password" placeholder="留空则不更改密码" show-password />
        </el-form-item>

        <el-form-item label="昵称">
          <el-input v-model="formData.nickname" placeholder="请输入昵称" />
        </el-form-item>

        <el-form-item label="真实姓名">
          <el-input v-model="formData.real_name" placeholder="请输入真实姓名" />
        </el-form-item>

        <el-form-item label="部门">
          <el-select
            v-model="formData.department_id"
            placeholder="请选择部门"
            clearable
            style="width: 100%"
          >
            <el-option
              v-for="dept in flatDepartments"
              :key="dept.id"
              :label="getDepartmentLabel(dept, dept.isLast)"
              :value="dept.id"
            />
          </el-select>
        </el-form-item>

        <el-form-item label="职位">
          <el-input v-model="formData.position" placeholder="请输入职位" />
        </el-form-item>

        <el-form-item label="工号">
          <el-input v-model="formData.employee_no" placeholder="请输入工号" />
        </el-form-item>

        <el-form-item label="邮箱">
          <el-input v-model="formData.email" placeholder="请输入邮箱" />
        </el-form-item>

        <el-form-item label="电话">
          <el-input v-model="formData.phone" placeholder="请输入电话" />
        </el-form-item>

        <el-form-item label="状态">
          <el-radio-group v-model="formData.status">
            <el-radio label="active">启用</el-radio>
            <el-radio label="inactive">停用</el-radio>
          </el-radio-group>
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleSubmit">确定</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="deptDialogVisible" title="调整部门" width="400px">
      <el-form label-width="80px">
        <el-form-item label="当前用户">
          <span>{{ selectedUser?.nickname }}</span>
        </el-form-item>
        <el-form-item label="选择部门">
          <el-select
            v-model="newDepartmentId"
            placeholder="请选择部门"
            clearable
            style="width: 100%"
          >
            <el-option
              v-for="dept in flatDepartments"
              :key="dept.id"
              :label="getDepartmentLabel(dept, dept.isLast)"
              :value="dept.id"
            />
          </el-select>
        </el-form-item>
      </el-form>

      <template #footer>
        <el-button @click="deptDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleSubmitDepartment">确定</el-button>
      </template>
    </el-dialog>

    <!-- 分配角色对话框 -->
    <el-dialog
      v-model="roleDialogVisible"
      :title="`分配角色 - ${selectedUser?.nickname || ''}`"
      width="500px"
    >
      <el-form label-width="80px">
        <el-form-item label="当前用户">
          <span>{{ selectedUser?.username }}</span>
        </el-form-item>
        <el-form-item label="角色">
          <el-select
            v-model="selectedRoleCodes"
            multiple
            placeholder="请选择角色（可多选）"
            style="width: 100%"
          >
            <el-option
              v-for="r in allRoles"
              :key="r.code"
              :label="`${r.name} (${r.code})`"
              :value="r.code"
            />
          </el-select>
          <div style="margin-top: 8px; color: #909399; font-size: 12px">
            提示：分配的角色会与用户原有角色合并覆盖。修改后用户需重新登录才能看到新权限（30s 缓存）。
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="roleDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleSubmitRoles">确定</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from "vue";
import { ElMessage, ElMessageBox } from "element-plus";
import { ArrowDown } from "@element-plus/icons-vue";
import {
  getUsers,
  createUser,
  updateUser,
  deleteUser,
  updateUserDepartment,
  assignUserRoles,
  checkUsername,
  type User,
} from "@/api/user_extended";
import { listRoles, type Role } from "@/api/rbac";
import { useDepartmentTree } from "@/hooks/useDepartment";

const { departmentTree, flatDepartments, getDepartmentLabel, loadDepartmentTree, invalidateCache } = useDepartmentTree();

const userList = ref<User[]>([]);
const currentPage = ref(1);
const pageSize = ref(20);
const total = ref(0);
const searchKeyword = ref("");

const dialogVisible = ref(false);
const dialogTitle = ref("");
const currentId = ref<number | null>(null);

const deptDialogVisible = ref(false);
const selectedUser = ref<User | null>(null);
const newDepartmentId = ref<number | null>(null);

// 分配角色相关
const roleDialogVisible = ref(false);
const allRoles = ref<Role[]>([]);
const selectedRoleCodes = ref<string[]>([]);

const formData = ref({
  username: "",
  password: "",
  nickname: "",
  real_name: "",
  department_id: null as number | null,
  position: "",
  employee_no: "",
  email: "",
  phone: "",
  status: "active",
});

const usernameExists = ref(false);
const checkingUsername = ref(false);

const handleUsernameBlur = async () => {
  if (!formData.value.username || formData.value.username.length < 3) {
    return;
  }

  try {
    checkingUsername.value = true;
    const excludeId = currentId.value || undefined;
    const res = await checkUsername(formData.value.username, excludeId);
    if (res.success && res.data.exists) {
      usernameExists.value = true;
      ElMessage.warning("用户名已存在，请更换");
    } else {
      usernameExists.value = false;
    }
  } catch (error) {
    usernameExists.value = false;
  } finally {
    checkingUsername.value = false;
  }
};

const loadUsers = async () => {
  try {
    const res = await getUsers({
      page: currentPage.value,
      page_size: pageSize.value,
      search: searchKeyword.value
    });
    if (res.success) {
      userList.value = res.data.list;
      total.value = res.data.total;
    }
  } catch (error) {
    userList.value = [];
    total.value = 0;
    const msg = (error as any)?.response?.data?.message || (error as any)?.message || "获取用户列表失败";
    ElMessage.error(msg);
  }
};

const handleSearch = () => {
  currentPage.value = 1;
  loadUsers();
};

const handleSizeChange = (val: number) => {
  pageSize.value = val;
  currentPage.value = 1;
  loadUsers();
};

const handleCurrentChange = (val: number) => {
  currentPage.value = val;
  loadUsers();
};

const loadDepartments = async () => {
  // 已迁移到 useDepartmentTree().loadDepartmentTree
  await loadDepartmentTree();
};

const handleCreate = () => {
  dialogTitle.value = "添加用户";
  currentId.value = null;
  formData.value = {
    username: "",
    password: "",
    nickname: "",
    real_name: "",
    department_id: null,
    position: "",
    employee_no: "",
    email: "",
    phone: "",
    status: "active",
  };
  usernameExists.value = false;
  dialogVisible.value = true;
};

const handleEdit = (user: User) => {
  dialogTitle.value = "编辑用户";
  currentId.value = user.id;
  formData.value = {
    username: user.username,
    password: "",
    nickname: user.nickname,
    real_name: user.real_name,
    department_id: user.department_id,
    position: user.position,
    employee_no: user.employee_no,
    email: user.email,
    phone: user.phone,
    status: user.status,
  };
  usernameExists.value = false;
  dialogVisible.value = true;
};

const handleChangeDepartment = (user: User) => {
  selectedUser.value = user;
  newDepartmentId.value = user.department_id;
  deptDialogVisible.value = true;
};

const handleSubmitDepartment = async () => {
  if (!selectedUser.value) return;

  try {
    const res = await updateUserDepartment(selectedUser.value.id, {
      department_id: newDepartmentId.value,
    });
    if (res.success) {
      ElMessage.success("部门调整成功");
      invalidateCache();
      loadDepartmentTree();
      deptDialogVisible.value = false;
      loadUsers();
    } else {
      ElMessage.error(res.message || "部门调整失败");
    }
  } catch (error) {
    const msg = (error as any)?.response?.data?.message || (error as any)?.message || "部门调整失败";
    ElMessage.error(msg);
  }
};

// 加载所有角色（用于分配角色下拉）
const loadAllRoles = async () => {
  try {
    const res: any = await listRoles();
    allRoles.value = res.data?.list || [];
  } catch (e: any) {
    console.error("加载角色列表失败", e);
  }
};

// 打开"分配角色"对话框
const handleAssignRoles = (user: User) => {
  selectedUser.value = user;
  selectedRoleCodes.value = [...(user.roles || [])];
  roleDialogVisible.value = true;
};

// 提交角色分配
const handleSubmitRoles = async () => {
  if (!selectedUser.value) return;
  try {
    const res: any = await assignUserRoles(selectedUser.value.id, {
      roles: selectedRoleCodes.value
    });
    if (res.success !== false) {
      ElMessage.success(`已分配 ${selectedRoleCodes.value.length} 个角色`);
      roleDialogVisible.value = false;
      loadUsers();
    } else {
      ElMessage.error(res.message || "分配失败");
    }
  } catch (e: any) {
    const msg = e?.response?.data?.message || e?.message || e || "分配角色失败";
    ElMessage.error(msg);
  }
};

const handleDelete = async (user: User) => {
  try {
    await ElMessageBox.confirm(`确定要删除用户"${user.nickname}"吗？`, "提示", {
      confirmButtonText: "确定",
      cancelButtonText: "取消",
      type: "warning",
    });

    const res = await deleteUser(user.id);
    if (res.data.success) {
      ElMessage.success("删除成功");
      loadUsers();
    }
  } catch (error: any) {
    if (error !== "cancel") {
      ElMessage.error(error.message || "删除失败");
    }
  }
};

const handleToggleStatus = async (user: User) => {
  const newStatus = user.status === "active" ? "inactive" : "active";
  const action = user.status === "active" ? "禁用" : "启用";

  try {
    await ElMessageBox.confirm(`确定要${action}用户"${user.nickname}"吗？`, "提示", {
      confirmButtonText: "确定",
      cancelButtonText: "取消",
      type: "warning",
    });

    const res = await updateUser(user.id, { status: newStatus });
    if (res.success) {
      ElMessage.success(`${action}成功`);
      loadUsers();
    } else {
      ElMessage.error(res.message || `${action}失败`);
    }
  } catch (error: any) {
    if (error !== "cancel") {
      if (error.response?.data?.message) {
        ElMessage.error(error.response.data.message);
      } else {
        ElMessage.error(`${action}失败`);
      }
    }
  }
};

const handleSubmit = async () => {
  if (!formData.value.username || formData.value.username.length < 3) {
    ElMessage.warning("用户名至少需要3个字符");
    return;
  }

  if (usernameExists.value) {
    ElMessage.warning("用户名已存在，请更换");
    return;
  }

  if (!currentId.value) {
    if (!formData.value.password || formData.value.password.length < 6) {
      ElMessage.warning("密码至少需要6个字符");
      return;
    }

    try {
      const res = await createUser(formData.value);
      if (res.success) {
        ElMessage.success("用户创建成功");
        dialogVisible.value = false;
        loadUsers();
      } else {
        ElMessage.error(res.message || "用户创建失败");
      }
    } catch (error) {
      const msg = (error as any)?.response?.data?.message || (error as any)?.message || "用户创建失败";
      ElMessage.error(msg);
    }
    return;
  }

  try {
    const res = await updateUser(currentId.value, formData.value);
    if (res.success) {
      ElMessage.success("更新成功");
      dialogVisible.value = false;
      loadUsers();
    } else {
      ElMessage.error(res.message || "更新失败");
    }
  } catch (error) {
    const msg = (error as any)?.response?.data?.message || (error as any)?.message || "更新失败";
    ElMessage.error(msg);
  }
};

const handleDialogClose = () => {
  currentId.value = null;
  usernameExists.value = false;
  checkingUsername.value = false;
};

onMounted(() => {
  loadUsers();
  loadDepartmentTree();
  loadAllRoles();
});
</script>

<style scoped>
.user-container {
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

.pagination-container {
  margin-top: 20px;
  display: flex;
  justify-content: flex-end;
}

/* 操作列按钮紧凑对齐 */
.op-cell {
  display: inline-flex;
  align-items: center;
  gap: 0;
  white-space: nowrap;
}
.op-cell .el-button {
  margin-right: 0 !important;
  padding: 0 !important;
}
.op-cell .op-more {
  display: inline-flex;
  align-items: center;
  line-height: 1;
  margin-left: 12px !important;
}
.op-cell .op-more .el-icon {
  margin-left: 0 !important;
}
</style>
