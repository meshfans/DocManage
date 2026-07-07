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

  const loadDepartmentTree = async (fields?: string, forceRefresh: boolean = false) => {
    const now = Date.now();

    if (!forceRefresh && !fields && cachedTree && (now - cacheTimestamp) < CACHE_DURATION) {
      departmentTree.value = cachedTree;
      return;
    }

    loading.value = true;
    try {
      const res = await getDepartmentTree(fields);
      if (res.success) {
        departmentTree.value = res.data as DepartmentTree[];
        if (!fields) {
          cachedTree = res.data as DepartmentTree[];
          cacheTimestamp = now;
        }
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
    getDepartmentLabel,
    loadDepartmentTree,
    invalidateCache
  };
}
