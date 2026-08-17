import { ref, computed } from 'vue';
import { getDepartmentTree, type DepartmentTree } from '@/api/department';

export interface FlatDepartment extends DepartmentTree {
  isLast: boolean;
}

let cachedTree: DepartmentTree[] | null = null;
let cacheTimestamp: number = 0;
const CACHE_DURATION = 5 * 60 * 1000; // 5 minutes

export function useDepartmentTree() {
  const departmentTree = ref<DepartmentTree[]>([]);
  const loading = ref(false);

  const flatDepartments = computed<FlatDepartment[]>(() => {
    const result: FlatDepartment[] = [];
    
    const flatten = (nodes: DepartmentTree[], parentIsLast: boolean = false, parentLevel: number = 0) => {
      for (let i = 0; i < nodes.length; i++) {
        const node = nodes[i];
        const isLast = i === nodes.length - 1;
        result.push({
          ...node,
          isLast
        });
        
        if (node.children && node.children.length > 0) {
          flatten(node.children, isLast, node.level);
        }
      }
    };
    
    flatten(departmentTree.value);
    return result;
  });

  const getDepartmentLabel = (dept: FlatDepartment): string => {
    if (dept.level === 0) {
      return dept.name;
    }
    const prefix = '├'.repeat(dept.level - 1);
    const connector = dept.isLast ? '└' : '├';
    return prefix + connector + ' ' + dept.name;
  };

  /** 兼容旧调用：`getDepartmentLabel(dept, isLast)` —— 忽略 isLast */
  function getDepartmentLabelCompat(
    dept: FlatDepartment,
    _isLast?: boolean
  ): string {
    return getDepartmentLabel(dept);
  }

  const loadDepartmentTree = async (fields?: string, forceRefresh: boolean = false) => {
    const now = Date.now();

    if (!forceRefresh && !fields && cachedTree && (now - cacheTimestamp) < CACHE_DURATION) {
      departmentTree.value = cachedTree;
      return;
    }

    loading.value = true;
    try {
      const res = await getDepartmentTree(fields);
      // 后端返回 { list: DepartmentTree[], total }；axios 拦截器已 unwrap。
      const tree = Array.isArray(res.list) ? res.list : [];
      departmentTree.value = tree;
      if (!fields) {
        cachedTree = tree;
        cacheTimestamp = now;
      }
    } catch (error) {
      console.error('获取部门列表失败', error);
    } finally {
      loading.value = false;
    }
  };

  const invalidateCache = () => {
    cachedTree = null;
    cacheTimestamp = 0;
  };

  return {
    departmentTree,
    flatDepartments,
    loading,
    getDepartmentLabel: getDepartmentLabelCompat,
    loadDepartmentTree,
    invalidateCache
  };
}
