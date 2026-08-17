<template>
  <div class="role-container">
    <el-card>
      <template #header>
        <div class="card-header">
          <div class="card-title">
            <el-icon :size="20" color="#409EFF"><UserFilled /></el-icon>
            <span>角色管理</span>
            <el-tag size="small" type="info" effect="plain">
              {{ roleList.length }} 个角色
            </el-tag>
          </div>
          <div class="header-actions">
            <el-input
              v-model="searchKeyword"
              placeholder="搜索代码/名称/描述"
              clearable
              style="width: 220px"
              @keyup.enter="handleSearch"
            >
              <template #prefix>
                <el-icon><Search /></el-icon>
              </template>
            </el-input>
            <el-button @click="handleSearch">搜索</el-button>
            <el-button @click="loadRoles">
              <el-icon style="margin-right: 4px"><Refresh /></el-icon>
              刷新
            </el-button>
            <el-button v-perms="'rbac:roles:upsert'" type="primary" @click="handleCreate">
              <el-icon style="margin-right: 4px"><Plus /></el-icon>
              新建角色
            </el-button>
          </div>
        </div>
      </template>

      <!-- 统计卡 -->
      <div class="stats-row">
        <el-card class="stat-card" shadow="never">
          <div class="stat-label">角色总数</div>
          <div class="stat-value">{{ roleList.length }}</div>
        </el-card>
        <el-card class="stat-card" shadow="never">
          <div class="stat-label">系统预置</div>
          <div class="stat-value stat-warning">
            {{ roleList.filter(r => r.is_system).length }}
          </div>
        </el-card>
        <el-card class="stat-card" shadow="never">
          <div class="stat-label">自定义</div>
          <div class="stat-value stat-primary">
            {{ roleList.filter(r => !r.is_system).length }}
          </div>
        </el-card>
        <el-card class="stat-card" shadow="never">
          <div class="stat-label">已启用</div>
          <div class="stat-value stat-success">
            {{ roleList.filter(r => r.status === 'active').length }}
          </div>
        </el-card>
        <el-card class="stat-card" shadow="never">
          <div class="stat-label">权限总数</div>
          <div class="stat-value stat-info">
            {{ totalPermissions }}
          </div>
        </el-card>
      </div>

      <!-- 列表 -->
      <el-table
        v-loading="loading"
        :data="pagedRoleList"
        stripe
        style="margin-top: 12px"
        empty-text="暂无角色，点击右上角「新建角色」开始"
      >
        <el-table-column prop="code" label="代码" width="180" />
        <el-table-column prop="name" label="名称" width="160" />
        <el-table-column prop="description" label="描述" min-width="200" show-overflow-tooltip>
          <template #default="{ row }">
            <span v-if="row.description">{{ row.description }}</span>
            <span v-else style="color: #c0c4cc">-</span>
          </template>
        </el-table-column>
        <el-table-column label="权限数" width="100" align="center">
          <template #default="{ row }">
            <el-tag type="info" size="small">
              {{ (row.permissions || []).length }}
            </el-tag>
          </template>
        </el-table-column>
        <!-- 2026-06-28 RBAC v3 P5：数据范围列 -->
        <el-table-column label="数据范围" width="160" align="center">
          <template #default="{ row }">
            <el-tag :type="getDataScopeTagType(row.data_scope)" size="small">
              {{ getDataScopeLabel(row.data_scope) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="系统预置" width="100" align="center">
          <template #default="{ row }">
            <el-tag v-if="row.is_system" type="warning" size="small">是</el-tag>
            <el-tag v-else type="info" size="small">否</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="100" align="center">
          <template #default="{ row }">
            <el-tag :type="row.status === 'active' ? 'success' : 'danger'" size="small">
              {{ row.status === "active" ? "启用" : "停用" }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="320" fixed="right">
          <template #default="{ row }">
            <!-- 分配权限复用 rbac:roles:upsert 权限码（与后端 handler 一致）-->
            <el-button
              v-perms="'rbac:roles:upsert'"
              link
              type="primary"
              size="small"
              @click="handleAssignPerms(row)"
            >
              分配权限
            </el-button>
            <!-- 2026-06-25 A.3 修复：系统预置 role 不可编辑（与 is_system=1 在后端对应） -->
            <el-button
              v-perms="'rbac:roles:upsert'"
              link
              type="primary"
              size="small"
              :disabled="row.is_system"
              @click="handleEdit(row)"
            >
              编辑
            </el-button>
            <el-button link type="success" size="small" @click="handleTestRole(row)">
              权限测试
            </el-button>
            <el-button
              v-perms="'rbac:roles:delete'"
              link
              type="danger"
              size="small"
              :disabled="row.is_system"
              @click="handleDelete(row)"
            >
              删除
            </el-button>
          </template>
        </el-table-column>
      </el-table>

      <!-- 分页 -->
      <div class="pagination-container">
        <el-pagination
          v-model:current-page="currentPage"
          v-model:page-size="pageSize"
          :page-sizes="[10, 20, 50, 100]"
          :total="filteredRoleList.length"
          layout="total, sizes, prev, pager, next, jumper"
          background
          @size-change="handleSizeChange"
          @current-change="handleCurrentChange"
        />
      </div>
    </el-card>

    <!-- 编辑对话框 -->
    <el-dialog
      v-model="dialogVisible"
      :title="dialogTitle"
      width="600px"
      :close-on-click-modal="false"
      @close="handleDialogClose"
    >
      <el-form :model="formData" label-width="100px">
        <el-form-item label="代码" required>
          <el-input
            v-model="formData.code"
            :disabled="dialogMode === 'edit'"
            placeholder="英文，唯一，如 manager"
          />
        </el-form-item>
        <el-form-item label="名称" required>
          <el-input v-model="formData.name" placeholder="中文显示名" />
        </el-form-item>
        <el-form-item label="描述">
          <el-input v-model="formData.description" type="textarea" :rows="2" />
        </el-form-item>
        <el-form-item label="状态">
          <el-radio-group v-model="formData.status">
            <el-radio value="active">启用</el-radio>
            <el-radio value="disabled">停用</el-radio>
          </el-radio-group>
        </el-form-item>
        <!-- 2026-06-28 RBAC v3 P5：数据范围下拉 -->
        <el-form-item label="数据范围">
          <el-select v-model="formData.data_scope" placeholder="选择数据范围" style="width: 100%">
            <el-option
              v-for="opt in DATA_SCOPE_OPTIONS"
              :key="opt.value"
              :label="opt.label"
              :value="opt.value"
            >
              <div style="display: flex; justify-content: space-between; align-items: center">
                <span>{{ opt.label }}</span>
                <span style="color: #909399; font-size: 12px">{{ opt.desc }}</span>
              </div>
            </el-option>
          </el-select>
          <!-- 2026-06-28 RBAC v3.1：custom 模式部门多选（实现方案 B：JSON 字段）-->
          <!--
            2026-06-28 修复：check-strictly="true" 关闭父子节点关联。
            默认情况下 el-tree 的多选模式会级联（勾子节点自动勾父节点，反之亦然）。
            对 custom 白名单场景不合理：用户可能只想勾"销售部"或只想勾"上海办"，不应被自动扩散。
            参考 flow/template.vue 步骤部门选择的实现（DepartmentSelect 组件用 :check-strictly="multiple"）。
          -->
          <div v-if="formData.data_scope === 'custom'" style="margin-top: 12px">
            <div style="margin-bottom: 8px; color: #606266; font-size: 13px">
              选择允许查看的部门（白名单）：
              <span style="color: #909399; font-size: 12px">
                已选 {{ (formData.custom_dept_ids || []).length }} 个
              </span>
            </div>
            <el-tree
              v-if="deptTree.length > 0"
              ref="customDeptTreeRef"
              :data="deptTree"
              show-checkbox
              node-key="id"
              check-strictly
              :default-checked-keys="formData.custom_dept_ids || []"
              :props="{ label: 'name', children: 'children' }"
              @check="onCustomDeptCheck"
              style="max-height: 300px; overflow: auto; border: 1px solid #e4e7ed; border-radius: 4px; padding: 8px"
            />
            <el-empty v-else description="正在加载部门树..." :image-size="60" />
            <div style="margin-top: 8px; color: #909399; font-size: 12px">
              提示：
              <ol style="margin: 4px 0 0 20px; padding: 0; font-size: 12px; line-height: 1.6">
                <li>仅勾选您明确点击的部门；不会自动包含上级或下级部门</li>
                <li>删除部门后，custom 模式将自动过滤（应用层 FK 校验）</li>
              </ol>
            </div>
          </div>
        </el-form-item>
        <el-form-item v-if="dialogMode === 'edit'" label="权限数">
          <el-tag type="info">
            {{ (formData.permissions || []).length }} 项
          </el-tag>
          <span style="margin-left: 8px; color: #909399; font-size: 12px">
            请用「分配权限」按钮修改
          </span>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="handleSubmit">保存</el-button>
      </template>
    </el-dialog>

    <!-- 分配权限对话框 -->
    <el-dialog
      v-model="permDialogVisible"
      :title="`分配权限 - ${permDialogRole?.name || ''}`"
      width="700px"
      top="5vh"
      :close-on-click-modal="false"
    >
      <div style="margin-bottom: 12px">
        <el-input
          v-model="filterText"
          placeholder="搜索权限名/代码"
          clearable
          style="width: 250px"
        />
        <el-button style="margin-left: 8px" @click="clearAll">清空</el-button>
        <el-button style="margin-left: 8px" @click="selectAll">全选</el-button>
        <span style="margin-left: 16px; color: #909399">
          已选 {{ checkedKeys.length }} / {{ allPermissions.length }}
        </span>
      </div>
      <el-tree
        ref="treeRef"
        :data="filteredTreeData"
        show-checkbox
        node-key="id"
        :default-checked-keys="checkedKeys"
        :props="{ label: 'label', children: 'children' }"
        @check="onTreeCheck"
        style="max-height: 60vh; overflow: auto"
      />
      <template #footer>
        <el-button @click="permDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="handlePermSubmit">保存</el-button>
      </template>
    </el-dialog>

    <!-- 权限测试结果对话框（P2） -->
    <el-dialog
      v-model="testDialogVisible"
      :title="`权限测试 - ${permDialogRole?.name || ''}`"
      width="700px"
      top="5vh"
    >
      <div v-if="testResult">
        <el-alert
          v-if="testResult.hasWildcard"
          title="此角色拥有通配符 *:*:*，可访问所有权限"
          type="success"
          :closable="false"
          show-icon
        />
        <el-alert
          v-else
          title="此角色无通配符，仅可访问下方列出的权限码对应的 API"
          type="info"
          :closable="false"
          show-icon
        />

        <h4 style="margin-top: 16px">权限统计</h4>
        <el-tag
          v-for="(codes, mod) in (testResult as any).byModule"
          :key="mod"
          style="margin: 4px"
        >
          {{ mod }}: {{ codes.length }} 项
        </el-tag>

        <h4 style="margin-top: 16px">全部权限码（{{ testResult.matched.length }} 项）</h4>
        <div style="max-height: 40vh; overflow: auto">
          <el-tag
            v-for="code in testResult.matched"
            :key="code"
            style="margin: 2px"
            size="small"
          >
            {{ code }}
          </el-tag>
        </div>
      </div>
      <template #footer>
        <el-button @click="testDialogVisible = false">关闭</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, reactive, nextTick, watch } from "vue";
import { ElMessage, ElMessageBox } from "element-plus";
import { Search, Refresh, Plus, UserFilled } from "@element-plus/icons-vue";
import {
  listRoles,
  upsertRole,
  deleteRole,
  listPermissions,
  DATA_SCOPE_OPTIONS
} from "@/api/rbac";
import type { Role, Permission } from "@/api/rbac";
import { getDepartmentTree } from "@/api/department";
import type { DepartmentTree } from "@/api/department";

defineOptions({ name: "RoleManagement" });

// ==================== 列表 + 分页 ====================
const loading = ref(false);
const submitting = ref(false);
const roleList = ref<Role[]>([]);
const allPermissions = ref<Permission[]>([]);

// 分页（前端分页，与 system/user.vue、system/backup.vue 保持一致）
const currentPage = ref(1);
const pageSize = ref(20);

// 搜索
const searchKeyword = ref("");

// 编辑对话框
const dialogVisible = ref(false);
const dialogMode = ref<"create" | "edit">("create");
const dialogTitle = ref("");
const formData = reactive<Role>({
  id: 0,
  code: "",
  name: "",
  description: "",
  permissions: [],
  is_system: false,
  status: "active",
  data_scope: "self",
  custom_dept_ids: []
});

// 2026-06-28 RBAC v3.1：custom 模式部门树
const deptTree = ref<DepartmentTree[]>([]);

// 权限树对话框（分配权限）
const permDialogVisible = ref(false);
const permDialogRole = ref<Role | null>(null);
const checkedKeys = ref<string[]>([]);
const treeRef = ref();
const filterText = ref("");
const treeReady = ref(false);

// 权限测试对话框（P2）
const testDialogVisible = ref(false);
const testResult = ref<{ allowed: boolean; matched: string[]; required: string[]; byModule?: Record<string, string[]>; hasWildcard?: boolean } | null>(null);

// 树结构数据（按 module 分组）
const treeData = ref<any[]>([]);

const filteredTreeData = computed(() => {
  if (!filterText.value) return treeData.value;
  const txt = filterText.value.toLowerCase();
  const filtered = (nodes: any[]): any[] => {
    const out: any[] = [];
    for (const n of nodes) {
      const matchSelf =
        n.label?.toLowerCase().includes(txt) || n.code?.toLowerCase().includes(txt);
      const filteredChildren = n.children ? filtered(n.children) : [];
      if (matchSelf || filteredChildren.length) {
        out.push({ ...n, children: filteredChildren.length ? filteredChildren : n.children });
      }
    }
    return out;
  };
  return filtered(treeData.value);
});

// 过滤后的列表（用于搜索 + 分页）
const filteredRoleList = computed(() => {
  const kw = searchKeyword.value.trim().toLowerCase();
  if (!kw) return roleList.value;
  return roleList.value.filter(
    (r) =>
      r.code.toLowerCase().includes(kw) ||
      r.name.toLowerCase().includes(kw) ||
      (r.description || "").toLowerCase().includes(kw)
  );
});

// 搜索条件变化时自动重置到第一页（与 permission.vue 保持一致）
watch(searchKeyword, () => {
  currentPage.value = 1;
});

// 当前页数据
const pagedRoleList = computed(() => {
  const start = (currentPage.value - 1) * pageSize.value;
  return filteredRoleList.value.slice(start, start + pageSize.value);
});

// 权限总数（用于统计卡）
const totalPermissions = computed(() => {
  const set = new Set<string>();
  for (const r of roleList.value) {
    for (const p of r.permissions || []) {
      if (p !== "*:*:*") set.add(p);
    }
  }
  return set.size;
});

async function loadRoles() {
  loading.value = true;
  try {
    const res: any = await listRoles();
    roleList.value = res.data?.list || [];
    // 搜索条件变化时重置到第一页
    currentPage.value = 1;
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.message || e?.message || e || "加载角色失败");
  } finally {
    loading.value = false;
  }
}

async function loadPermissions() {
  try {
    const res: any = await listPermissions();
    allPermissions.value = res.data?.list || [];
    const grouped: Record<string, Permission[]> = {};
    for (const p of allPermissions.value) {
      const m = p.module || "其他";
      if (!grouped[m]) grouped[m] = [];
      grouped[m].push(p);
    }
    treeData.value = Object.keys(grouped)
      .sort()
      .map((m) => ({
        id: `module:${m}`,
        label: m,
        children: grouped[m].map((p) => ({
          id: p.code,
          label: `${p.name} (${p.code})`,
          code: p.code,
          api_path: p.api_path,
          http_method: p.http_method
        }))
      }));
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.message || e?.message || e || "加载权限失败");
  }
}

function handleSearch() {
  currentPage.value = 1;
}

function handleSizeChange(val: number) {
  pageSize.value = val;
  currentPage.value = 1;
}

function handleCurrentChange(val: number) {
  currentPage.value = val;
}

function handleCreate() {
  dialogMode.value = "create";
  dialogTitle.value = "新建角色";
  Object.assign(formData, {
    id: 0,
    code: "",
    name: "",
    description: "",
    permissions: [],
    is_system: false,
    status: "active",
    data_scope: "self",
    custom_dept_ids: []
  });
  dialogVisible.value = true;
}

function handleEdit(row: Role) {
  dialogMode.value = "edit";
  dialogTitle.value = "编辑角色";
  Object.assign(formData, JSON.parse(JSON.stringify(row)));
  dialogVisible.value = true;
}

async function handleDelete(row: Role) {
  try {
    await ElMessageBox.confirm(
      `确定删除角色 "${row.name}" 吗？此操作不可恢复。`,
      "确认删除",
      {
        type: "warning",
        confirmButtonText: "确认删除",
        cancelButtonText: "取消"
      }
    );
    await deleteRole(row.code);
    ElMessage.success("删除成功");
    loadRoles();
  } catch (e: any) {
    if (e !== "cancel" && e !== "close") {
      ElMessage.error(e?.response?.data?.message || e?.message || e || "删除失败");
    }
  }
}

async function handleSubmit() {
  if (!formData.code || !formData.name) {
    ElMessage.error("代码和名称不能为空");
    return;
  }
  submitting.value = true;
  try {
    await upsertRole(formData);
    ElMessage.success("保存成功");
    dialogVisible.value = false;
    loadRoles();
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.message || e?.message || e || "保存失败");
  } finally {
    submitting.value = false;
  }
}

function handleAssignPerms(row: Role) {
  permDialogRole.value = row;
  checkedKeys.value = [...(row.permissions || [])];
  treeReady.value = false;
  permDialogVisible.value = true;
  // 弹窗打开 + tree 挂载后，用 setCheckedKeys 同步（default-checked-keys 只在初始化生效）
  nextTick(() => {
    nextTick(() => {
      treeRef.value?.setCheckedKeys(checkedKeys.value, false);
      treeReady.value = true;
    });
  });
}

// P2：测试 role 的权限配置（直接展示角色拥有的权限码分布）
function handleTestRole(row: Role) {
  if (!row.permissions || row.permissions.length === 0) {
    ElMessage.warning("此角色没有任何权限码");
    return;
  }

  // 按 module 分组统计
  const byModule: Record<string, string[]> = {};
  for (const code of row.permissions) {
    if (code === "*:*:*") continue;
    const mod = code.split(":")[0] || "其他";
    if (!byModule[mod]) byModule[mod] = [];
    byModule[mod].push(code);
  }
  const hasWildcard = row.permissions.includes("*:*:*");
  testResult.value = {
    allowed: hasWildcard || row.permissions.length > 0,
    matched: row.permissions,
    required: row.permissions,
    byModule,
    hasWildcard
  } as any;
  testDialogVisible.value = true;
}

async function handlePermSubmit() {
  if (!permDialogRole.value) return;
  submitting.value = true;
  try {
    const updated = { ...permDialogRole.value, permissions: checkedKeys.value };
    await upsertRole(updated);
    ElMessage.success("权限分配成功");
    permDialogVisible.value = false;
    loadRoles();
  } catch (e: any) {
    ElMessage.error(e?.response?.data?.message || e?.message || e || "权限分配失败");
  } finally {
    submitting.value = false;
  }
}

function onTreeCheck() {
  const t = treeRef.value;
  if (!t) return;
  checkedKeys.value = (t.getCheckedKeys() as string[]).filter(
    (k) => !k.startsWith("module:")
  );
}

// 2026-06-28 RBAC v3 P5：data_scope 展示辅助函数
function getDataScopeLabel(scope?: string): string {
  const opt = DATA_SCOPE_OPTIONS.find((o) => o.value === scope);
  return opt?.label || "仅自己";
}

// 2026-06-28 RBAC v3.1：custom 模式 el-tree @check 事件 → 同步到 formData.custom_dept_ids
const customDeptTreeRef = ref();
function onCustomDeptCheck() {
  const t = customDeptTreeRef.value;
  if (!t) return;
  const checked = t.getCheckedNodes(false, false) as { id: number }[];
  formData.custom_dept_ids = checked.map((n) => n.id).filter((id) => typeof id === "number");
}

// 懒加载部门树：data_scope 切到 custom 时才拉部门树，避免每次打开 dialog 都打接口。
// 缓存命中：deptTree 已非空则直接复用（同一 dialog 内多次切回 custom 不会重复请求）。
async function ensureDeptTreeLoaded() {
  if (deptTree.value.length > 0) return;
  try {
    const res = await getDepartmentTree();
    // 后端返回 { list: DepartmentTree[], total }；axios 拦截器已 unwrap。
    // res.list 在错误路径上可能为 undefined（如 401 时 403 envelope 没 list 字段），
    // 用 Array.isArray 兜底，避免把 undefined 灌进 ref 导致 el-tree 渲染崩溃。
    deptTree.value = Array.isArray(res.list) ? res.list : [];
  } catch (e: any) {
    // 错误格式兼容：
    //   1) axios 错误对象：e.response.data.message 是后端 envelope.message
    //   2) 非 axios 异常（如代码抛出的 Error）：取 e.message
    //   3) 都为空时降级到 fallback 字符串
    ElMessage.error(e?.response?.data?.message || e?.message || e || "加载部门树失败");
  }
}

watch(
  () => formData.data_scope,
  (v) => {
    if (v === "custom" && dialogVisible.value) {
      ensureDeptTreeLoaded();
    }
  }
);

watch(
  () => dialogVisible.value,
  (visible) => {
    if (visible && formData.data_scope === "custom") {
      ensureDeptTreeLoaded();
    }
  }
);

function getDataScopeTagType(scope?: string): "primary" | "success" | "warning" | "info" | "danger" {
  switch (scope) {
    case "all":
      return "danger";
    case "dept_and_sub":
      return "warning";
    case "dept":
      return "success";
    case "self_and_sub_dept":
      return "warning";
    case "custom":
      return "primary";
    case "self":
    default:
      return "info";
  }
}

function selectAll() {
  checkedKeys.value = allPermissions.value.map((p) => p.code);
  nextTick(() => {
    treeRef.value?.setCheckedKeys(checkedKeys.value, false);
  });
}

function clearAll() {
  checkedKeys.value = [];
  nextTick(() => {
    treeRef.value?.setCheckedKeys([], false);
  });
}

function handleDialogClose() {
  // 重置表单状态，避免下次打开时残留
  Object.assign(formData, {
    id: 0,
    code: "",
    name: "",
    description: "",
    permissions: [],
    is_system: false,
    status: "active",
    data_scope: "self",
    custom_dept_ids: []
  });
}

onMounted(async () => {
  await Promise.all([loadRoles(), loadPermissions()]);
});
</script>

<style scoped>
/* 与 system/user.vue、system/department.vue、system/backup.vue 保持一致的容器样式 */
.role-container {
  padding: 20px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.card-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-weight: 600;
}

.header-actions {
  display: flex;
  gap: 8px;
  align-items: center;
  flex-wrap: wrap;
}

/* 统计卡：与 system/audit.vue、system/backup.vue 保持一致 */
.stats-row {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
  gap: 12px;
  margin-bottom: 12px;
}

.stat-card {
  text-align: center;
}

.stat-card :deep(.el-card__body) {
  padding: 14px 10px;
}

.stat-label {
  color: #909399;
  font-size: 12px;
  margin-bottom: 6px;
}

.stat-value {
  font-size: 22px;
  font-weight: 600;
  color: #303133;
}

.stat-primary {
  color: #409eff;
}

.stat-success {
  color: #67c23a;
}

.stat-warning {
  color: #e6a23c;
}

.stat-info {
  color: #909399;
}

.stat-danger {
  color: #f56c6c;
}

/* 分页：与 system/user.vue 保持一致 */
.pagination-container {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}

/* 响应式 */
@media (max-width: 1200px) {
  .stats-row {
    grid-template-columns: repeat(3, 1fr);
  }
}

@media (max-width: 768px) {
  .stats-row {
    grid-template-columns: repeat(2, 1fr);
  }

  .header-actions {
    width: 100%;
    justify-content: flex-end;
  }
}
</style>