<template>
  <div class="user-select">
    <div class="selected-tags" v-if="selectedIds.length > 0">
      <el-tag
        v-for="userId in selectedIds"
        :key="userId"
        type="success"
        closable
        @close="handleRemove(userId)"
        size="small"
        class="user-tag"
      >
        {{ getUserName(userId) }}
      </el-tag>
    </div>

    <el-button
      type="primary"
      plain
      circle
      @click="openDialog"
      :title="placeholder"
    >
      <el-icon><Plus /></el-icon>
    </el-button>

    <el-dialog
      v-model="dialogVisible"
      :title="title"
      width="600px"
      @close="handleClose"
    >
      <div class="search-container">
        <el-input
          v-model="searchKeyword"
          :placeholder="searchPlaceholder"
          clearable
          @input="handleSearch"
          class="search-input"
        >
          <template #prefix>
            <el-icon><Search /></el-icon>
          </template>
        </el-input>
      </div>

      <div v-loading="loading">
        <el-table
          ref="tableRef"
          :data="displayList"
          height="350"
          @selection-change="handleSelectionChange"
          :row-key="(row: any) => row.id"
          highlight-current-row
        >
          <el-table-column
            v-if="multiple"
            type="selection"
            width="55"
          />
          <el-table-column
            v-else
            type="index"
            width="55"
          />
          <el-table-column prop="username" label="用户名" min-width="120" />
          <el-table-column prop="nickname" label="昵称" min-width="120" />
          <el-table-column prop="real_name" label="真实姓名" min-width="120" />
          <el-table-column prop="employee_no" label="工号" width="100" />
          <el-table-column
            v-if="!multiple"
            label="操作"
            width="80"
            fixed="right"
          >
            <template #default="{ row }">
              <el-button type="primary" link @click="handleSelect(row)">
                选择
              </el-button>
            </template>
          </el-table-column>
        </el-table>
      </div>

      <template #footer>
        <div class="dialog-footer">
          <span v-if="multiple && selectedUsers.length > 0" class="selected-count">
            已选择 {{ selectedUsers.length }} 项
          </span>
          <el-button @click="dialogVisible = false">取消</el-button>
          <el-button
            v-if="multiple"
            type="primary"
            @click="handleConfirm"
            :disabled="selectedUsers.length === 0"
          >
            确认选择
          </el-button>
        </div>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted } from "vue";
import { Plus, Search } from "@element-plus/icons-vue";
import { getUsers } from "@/api/user_extended";

interface Props {
  modelValue?: number | number[];
  multiple?: boolean;
  title?: string;
  placeholder?: string;
  searchPlaceholder?: string;
  disabledIds?: number[];
  /**
   * 是否在用户列表顶部显示「公开（组织级）」虚拟员工选项（value=0）。
   * 适用场景：印章分配等需要"公开 vs 具体员工"二选一的场景。
   */
  publicOption?: boolean;
  /** publicOption=true 时显示的标签文本 */
  publicLabel?: string;
}

const props = withDefaults(defineProps<Props>(), {
  multiple: false,
  title: "选择用户",
  placeholder: "添加用户",
  searchPlaceholder: "搜索用户名/昵称/真实姓名/工号",
  disabledIds: () => [],
  publicOption: false,
  publicLabel: "全员公开",
});

const emit = defineEmits<{
  "update:modelValue": [value: number | number[]];
  change: [value: number | number[]];
}>();

const dialogVisible = ref(false);
const searchKeyword = ref("");
const userList = ref<any[]>([]);
const selectedUsers = ref<any[]>([]);
const tableRef = ref<any>(null);
const loading = ref(false);

/**
 * 把 modelValue 归一化为 number[]：
 *   - number（单选）→ [n] 或 []
 *   - number[]（多选）→ 原样
 * 这样 v-for / v-if / getUserName 都不用再分单/多选写两套。
 */
const selectedIds = computed<number[]>(() => {
  const v = props.modelValue;
  if (Array.isArray(v)) return v.filter(id => typeof id === "number");
  // 包含 0（公开虚拟员工）：单选时显示已选标签
  if (typeof v === "number" && v >= 0) return [v];
  return [];
});

const filteredUserList = computed(() => {
  if (!searchKeyword.value) {
    return userList.value.slice(0, 50);
  }

  const keyword = searchKeyword.value.toLowerCase();
  return userList.value.filter((user) => {
    return (
      user.username.toLowerCase().includes(keyword) ||
      (user.nickname && user.nickname.toLowerCase().includes(keyword)) ||
      (user.real_name && user.real_name.toLowerCase().includes(keyword)) ||
      (user.employee_no && user.employee_no.toLowerCase().includes(keyword))
    );
  }).slice(0, 50);
});

/**
 * 显示给 el-table 的最终列表：当 publicOption=true 时，
 * 在真实员工列表前面插入一条"公开"虚拟行。
 * 该行不受搜索影响（始终置顶），便于 admin 快速设为公开。
 */
const displayList = computed<any[]>(() => {
  if (!props.publicOption) return filteredUserList.value;
  return [
    {
      id: 0,
      isPublic: true,
      username: props.publicLabel,
      nickname: "组织级共享",
      real_name: "—",
      employee_no: "—"
    },
    ...filteredUserList.value
  ];
});

const loadUsers = async () => {
  loading.value = true;
  try {
    const res = await getUsers({
      fields: "id,username,nickname,real_name,employee_no,avatar",
    });
    if (res.data?.list) {
      userList.value = res.data.list;
    }
  } catch (error) {
    console.error("加载用户列表失败:", error);
  } finally {
    loading.value = false;
  }
};

const openDialog = async () => {
  dialogVisible.value = true;
  searchKeyword.value = "";
  selectedUsers.value = [];

  if (userList.value.length === 0) {
    await loadUsers();
  }

  const currentIds = Array.isArray(props.modelValue)
    ? props.modelValue
    : props.modelValue ? [props.modelValue]
    : [];

  selectedUsers.value = userList.value.filter((user) =>
    currentIds.includes(user.id)
  );

  setTimeout(() => {
    if (tableRef.value && props.multiple) {
      selectedUsers.value.forEach((user) => {
        tableRef.value.toggleRowSelection(user, true);
      });
    }
  }, 100);
};

const handleClose = () => {
  searchKeyword.value = "";
  selectedUsers.value = [];
};

const handleSearch = () => {
  setTimeout(() => {
    if (tableRef.value && props.multiple) {
      tableRef.value.clearSelection();
      selectedUsers.value.forEach((user) => {
        if (filteredUserList.value.find((u) => u.id === user.id)) {
          tableRef.value.toggleRowSelection(user, true);
        }
      });
    }
  }, 50);
};

const handleSelect = (user: any) => {
  // 虚拟"公开"行：直接发送 0，关闭弹窗
  if (user.isPublic) {
    emit("update:modelValue", 0);
    emit("change", 0);
    dialogVisible.value = false;
    return;
  }
  if (props.multiple) {
    const index = selectedUsers.value.findIndex((u) => u.id === user.id);
    if (index === -1) {
      selectedUsers.value.push(user);
    }
  } else {
    emit("update:modelValue", user.id);
    emit("change", user.id);
    dialogVisible.value = false;
  }
};

const handleSelectionChange = (selection: any[]) => {
  selectedUsers.value = selection;
};

const handleConfirm = () => {
  const ids = selectedUsers.value.map((user) => user.id);
  emit("update:modelValue", props.multiple ? ids : ids[0] || null);
  emit("change", props.multiple ? ids : ids[0] || null);
  dialogVisible.value = false;
};

const handleRemove = (userId: number) => {
  const currentValue = Array.isArray(props.modelValue)
    ? [...props.modelValue]
    : props.modelValue
    ? [props.modelValue]
    : [];

  const newValue = currentValue.filter((id) => id !== userId);
  emit(
    "update:modelValue",
    props.multiple ? newValue : newValue[0] || null
  );
  emit("change", props.multiple ? newValue : newValue[0] || null);
};

const getUserName = (userId: number): string => {
  // 虚拟"公开"员工 (user_id = 0)
  if (userId === 0) return props.publicLabel;
  const user = userList.value.find((u) => u.id === userId);
  if (!user) return `User ${userId}`;
  const name = user.nickname || user.username;
  return user.real_name ? `${name} (${user.real_name})` : name;
};

onMounted(() => {
  loadUsers();
});
</script>

<style scoped>
.user-select {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
}

.selected-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.user-tag {
  margin-right: 0;
}

.search-container {
  margin-bottom: 16px;
}

.search-input {
  width: 100%;
}

.dialog-footer {
  display: flex;
  justify-content: space-between;
  align-items: center;
  width: 100%;
}

.selected-count {
  color: #409eff;
  font-size: 14px;
}
</style>
