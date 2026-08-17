<template>
  <div class="department-select">
    <div class="selected-tags" v-if="Array.isArray(modelValue) && modelValue.length > 0">
      <el-tag
        v-for="deptId in modelValue"
        :key="deptId"
        type="success"
        closable
        @close="handleRemove(deptId)"
        size="small"
        class="dept-tag"
      >
        {{ getDepartmentName(deptId) }}
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
      width="400px"
      @close="handleClose"
    >
      <div v-loading="loading" style="min-height: 300px;">
        <el-tree
          ref="treeRef"
          :data="treeData"
          :props="treeProps"
          node-key="id"
          :highlight-current="!multiple"
          :show-checkbox="multiple"
          :check-strictly="multiple"
          :expand-on-click-node="false"
          @node-click="handleNodeClick"
          @check="handleCheck"
          default-expand-all
          class="department-tree"
        />
      </div>

      <template #footer>
        <div class="dialog-footer">
          <span v-if="multiple && leafCount > 0" class="selected-count">
            已选择 {{ leafCount }} 个部门
          </span>
          <el-button @click="dialogVisible = false">取消</el-button>
          <el-button
            v-if="multiple"
            type="primary"
            @click="handleConfirm"
            :disabled="checkedDepts.length === 0"
          >
            确认选择
          </el-button>
        </div>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, watch, onMounted, computed } from "vue";
import { Plus } from "@element-plus/icons-vue";
import { getDepartmentTree, type DepartmentTree } from "@/api/department";

interface Props {
  modelValue?: number | number[];
  multiple?: boolean;
  title?: string;
  placeholder?: string;
  disabledIds?: number[];
}

const props = withDefaults(defineProps<Props>(), {
  multiple: false,
  title: "选择部门",
  placeholder: "添加部门",
  disabledIds: () => [],
});

const emit = defineEmits<{
  "update:modelValue": [value: number | number[]];
  change: [value: number | number[]];
}>();

const dialogVisible = ref(false);
const treeData = ref<any[]>([]);
const treeRef = ref<any>(null);
const checkedDepts = ref<any[]>([]);
const currentNode = ref<any>(null);
const loading = ref(false);

const treeProps = {
  label: "name",
  children: "children",
};

const loadDepartmentTree = async () => {
  loading.value = true;
  try {
    const res = await getDepartmentTree("id,name,parent_id,level");
    // 后端返回 { list: DepartmentTree[], total }；axios 拦截器已 unwrap。
    if (Array.isArray(res.list)) {
      treeData.value = res.list as DepartmentTree[];
    }
  } catch (error) {
    console.error("加载部门树失败:", error);
  } finally {
    loading.value = false;
  }
};

const openDialog = async () => {
  dialogVisible.value = true;
  checkedDepts.value = [];
  currentNode.value = null;

  if (treeData.value.length === 0) {
    await loadDepartmentTree();
  }

  setTimeout(() => {
    if (treeRef.value && props.multiple) {
      treeRef.value.setCheckedKeys(
        Array.isArray(props.modelValue) ? props.modelValue : props.modelValue ? [props.modelValue] : []
      );
    } else if (treeRef.value && !props.multiple && props.modelValue) {
      treeRef.value.setCurrentNode(
        findNodeById(treeData.value, props.modelValue as number)
      );
    }
  }, 100);
};

const handleClose = () => {
  checkedDepts.value = [];
  currentNode.value = null;
};

const findNodeById = (nodes: any[], id: number): any => {
  for (const node of nodes) {
    if (node.id === id) {
      return node;
    }
    if (node.children && node.children.length > 0) {
      const found = findNodeById(node.children, id);
      if (found) return found;
    }
  }
  return null;
};

const handleNodeClick = (data: any) => {
  if (props.multiple) {
    return;
  }

  if (props.disabledIds.includes(data.id)) {
    return;
  }

  emit("update:modelValue", data.id);
  emit("change", data.id);
  dialogVisible.value = false;
};

const handleCheck = (data: any, checked: any) => {
  // 收集所有被勾选的节点
  checkedDepts.value = checked.checkedNodes.map((node: any) => ({
    id: node.id,
    name: node.name,
  }));
};

// 计算已选择的部门数
const leafCount = computed(() => checkedDepts.value.length);

const handleConfirm = () => {
  if (!props.multiple) {
    // 单选模式：直接用当前节点
    const ids = checkedDepts.value.map((dept) => dept.id);
    emit("update:modelValue", ids[0] || null);
    emit("change", ids[0] || null);
    dialogVisible.value = false;
    return;
  }

  // 多选模式：用户选择哪个就选中哪个，不展开
  const ids = Array.from(new Set(checkedDepts.value.map((dept) => dept.id)));

  emit("update:modelValue", ids);
  emit("change", ids);
  dialogVisible.value = false;
};

const handleRemove = (deptId: number) => {
  const currentValue = Array.isArray(props.modelValue)
    ? [...props.modelValue]
    : props.modelValue
    ? [props.modelValue]
    : [];

  const newValue = currentValue.filter((id) => id !== deptId);
  emit(
    "update:modelValue",
    props.multiple ? newValue : newValue[0] || null
  );
  emit("change", props.multiple ? newValue : newValue[0] || null);
};

const getDepartmentName = (deptId: number): string => {
  const dept = findNodeById(treeData.value, deptId);
  return dept ? dept.name : `Dept ${deptId}`;
};

watch(
  () => props.modelValue,
  async (newValue) => {
    if (
      treeData.value.length === 0 &&
      newValue &&
      (Array.isArray(newValue) ? newValue.length > 0 : true)
    ) {
      await loadDepartmentTree();
    }
  },
  { immediate: true }
);

onMounted(() => {
  loadDepartmentTree();
});
</script>

<style scoped>
.department-select {
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

.dept-tag {
  margin-right: 0;
}

.department-tree {
  max-height: 400px;
  overflow-y: auto;
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
