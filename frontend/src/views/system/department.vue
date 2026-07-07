<template>
  <div class="department-container">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>部门管理</span>
          <el-button type="primary" @click="handleCreate">添加部门</el-button>
        </div>
      </template>

      <!-- 树形表格：自带展开/折叠、列对齐、易扩展 -->
      <el-table
        ref="tableRef"
        v-loading="loading"
        :data="departmentTree"
        row-key="id"
        :tree-props="{ children: 'children', hasChildren: 'hasChildren' }"
        :default-expand-all="isExpandAll"
        border
        stripe
        style="width: 100%"
      >
        <el-table-column prop="name" label="部门名称" min-width="220">
          <template #default="{ row }">
            <span class="dept-name">{{ row.name }}</span>
          </template>
        </el-table-column>

        <el-table-column prop="id" label="ID" width="80" />

        <el-table-column prop="level" label="级别" width="90" align="center">
          <template #default="{ row }">
            <el-tag size="small" type="info">L{{ row.level }}</el-tag>
          </template>
        </el-table-column>

        <el-table-column prop="sort_order" label="排序" width="90" align="center" />

        <el-table-column prop="status" label="状态" width="100" align="center">
          <template #default="{ row }">
            <el-tag size="small" :type="row.status === 'active' ? 'success' : 'danger'">
              {{ row.status === 'active' ? '启用' : '停用' }}
            </el-tag>
          </template>
        </el-table-column>

        <el-table-column prop="created_at" label="创建时间" width="180">
          <template #default="{ row }">
            <span class="text-muted">{{ formatTime(row.created_at) }}</span>
          </template>
        </el-table-column>

        <el-table-column label="操作" width="200" fixed="right" align="center">
          <template #default="{ row }">
            <el-button link type="primary" size="small" @click="handleEdit(row)">
              编辑
            </el-button>
            <el-button link type="danger" size="small" @click="handleDelete(row)">
              删除
            </el-button>
          </template>
        </el-table-column>

        <template #empty>
          <el-empty description="暂无部门数据，点击右上角「添加部门」开始" />
        </template>
      </el-table>
    </el-card>

    <el-dialog
      v-model="dialogVisible"
      :title="dialogTitle"
      width="500px"
      @close="handleDialogClose"
    >
      <el-form :model="formData" label-width="100px">
        <el-form-item label="部门名称" required>
          <el-input v-model="formData.name" placeholder="请输入部门名称" />
        </el-form-item>

        <!-- 树形下拉：注入"顶级部门"虚拟根节点，使 parent_id=0 直观显示 -->
        <el-form-item label="上级部门">
          <el-tree-select
            v-model="formData.parent_id"
            :data="parentTreeOptions"
            :props="treeSelectProps"
            node-key="id"
            check-strictly
            default-expand-all
            filterable
            placeholder="请选择上级部门"
            style="width: 100%"
            :render-after-expand="false"
            class="parent-tree-select"
          />
        </el-form-item>

        <el-form-item label="排序">
          <el-input-number v-model="formData.sort_order" :min="1" :max="999" />
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
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from "vue";
import { ElMessage, ElMessageBox } from "element-plus";
import {
  createDepartment,
  updateDepartment,
  deleteDepartment,
  type DepartmentTree,
} from "@/api/department";
import { useDepartmentTree } from "@/hooks/useDepartment";
import { formatTimestampLang } from "@/utils/date";

defineOptions({ name: "DepartmentManagement" });

const {
  departmentTree,
  loading,
  loadDepartmentTree,
  invalidateCache,
} = useDepartmentTree();

const isExpandAll = ref(true);
const dialogVisible = ref(false);
const dialogTitle = ref("");
const isEdit = ref(false);
const currentId = ref<number | null>(null);

const formData = ref<{
  name: string;
  parent_id: number | null | undefined;
  sort_order: number;
  status: string;
}>({
  name: "",
  parent_id: 0,
  sort_order: 1,
  status: "active",
});

// 在树中按 id 查找节点
const findNodeById = (
  nodes: DepartmentTree[],
  id: number
): DepartmentTree | null => {
  for (const n of nodes) {
    if (n.id === id) return n;
    if (n.children?.length) {
      const found = findNodeById(n.children, id);
      if (found) return found;
    }
  }
  return null;
};

// el-tree-select 配置：label 显示 name，禁用当前编辑节点及其子孙
const treeSelectProps = {
  value: "id",
  label: "name",
  children: "children",
  disabled: (data: DepartmentTree) => isEditDisabled(data),
};

// 编辑时排除当前节点及其所有子节点（防止循环引用）
const isEditDisabled = (data: DepartmentTree): boolean => {
  // 虚拟根节点（顶级部门）始终可选
  if (data.id === 0) return false;
  if (!isEdit.value || currentId.value === null) return false;
  if (data.id === currentId.value) return true;
  const current = findNodeById(departmentTree.value, currentId.value);
  if (!current) return false;
  const contains = (nodes: DepartmentTree[]): boolean => {
    for (const n of nodes) {
      if (n.id === data.id) return true;
      if (n.children?.length && contains(n.children)) return true;
    }
    return false;
  };
  return contains(current.children || []);
};

// 注入"顶级部门"虚拟根节点，使 parent_id=0 直观显示
const parentTreeOptions = computed<DepartmentTree[]>(() => {
  return [
    {
      id: 0,
      name: "顶级部门",
      parent_id: -1,
      level: -1,
      sort_order: 0,
      status: "active",
      created_at: "",
      updated_at: "",
      children: departmentTree.value as DepartmentTree[],
    },
  ];
});

const formatTime = (v: number | string | null | undefined): string => {
  if (v === null || v === undefined || v === "" || v === 0) return "-";
  return formatTimestampLang(v);
};

const handleCreate = () => {
  isEdit.value = false;
  currentId.value = null;
  dialogTitle.value = "添加部门";
  formData.value = {
    name: "",
    parent_id: 0,
    sort_order: 1,
    status: "active",
  };
  dialogVisible.value = true;
};

const handleEdit = (data: DepartmentTree) => {
  isEdit.value = true;
  currentId.value = data.id;
  dialogTitle.value = "编辑部门";
  formData.value = {
    name: data.name,
    parent_id: data.parent_id || 0,
    sort_order: data.sort_order,
    status: data.status,
  };
  dialogVisible.value = true;
};

const handleDelete = async (data: DepartmentTree) => {
  try {
    await ElMessageBox.confirm(
      `确定要删除部门"${data.name}"吗？${
        data.children?.length ? "该部门下存在子部门，将无法删除。" : ""
      }`,
      "提示",
      {
        confirmButtonText: "确定",
        cancelButtonText: "取消",
        type: "warning",
      }
    );

    const res = await deleteDepartment(data.id);
    if (res.success) {
      ElMessage.success("删除成功");
      invalidateCache();
      await loadDepartmentTree(undefined, true);
    } else {
      ElMessage.error(res.message || "删除失败");
    }
  } catch (error: any) {
    if (error !== "cancel" && error !== "close") {
      if (error?.response?.data?.message) {
        ElMessage.error(error.response.data.message);
      } else if (error?.message) {
        ElMessage.error(error.message);
      } else {
        ElMessage.error("删除失败");
      }
    }
  }
};

const handleSubmit = async () => {
  if (!formData.value.name.trim()) {
    ElMessage.warning("请输入部门名称");
    return;
  }

  // 提交时将 null/undefined 归一为 0（顶级部门）
  const submitData = {
    ...formData.value,
    parent_id:
      formData.value.parent_id === null || formData.value.parent_id === undefined
        ? 0
        : formData.value.parent_id,
  };

  try {
    if (isEdit.value && currentId.value) {
      const res = await updateDepartment(currentId.value, submitData);
      if (res.success) {
        ElMessage.success("更新成功");
        invalidateCache();
        await loadDepartmentTree(undefined, true);
        dialogVisible.value = false;
      } else {
        ElMessage.error(res.message || "更新失败");
      }
    } else {
      const res = await createDepartment(submitData);
      if (res.success) {
        ElMessage.success("创建成功");
        invalidateCache();
        await loadDepartmentTree(undefined, true);
        dialogVisible.value = false;
      } else {
        ElMessage.error(res.message || "创建失败");
      }
    }
  } catch (error: any) {
    if (error?.response?.data?.message) {
      ElMessage.error(error.response.data.message);
    } else if (error?.message) {
      ElMessage.error(error.message);
    } else {
      ElMessage.error("操作失败");
    }
  }
};

const handleDialogClose = () => {
  currentId.value = null;
  isEdit.value = false;
};

onMounted(() => {
  loadDepartmentTree();
});
</script>

<style scoped>
.department-container {
  padding: 20px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.dept-name {
  font-weight: 500;
}

.text-muted {
  color: #909399;
  font-size: 13px;
}

/* el-tree-select 内部下拉面板树形样式微调 */
:deep(.parent-tree-select .el-tree-node__content) {
  height: 30px;
}

:deep(.parent-tree-select .el-tree-node__label) {
  font-size: 13px;
}
</style>
