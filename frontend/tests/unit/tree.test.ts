import { describe, it, expect } from "vitest";
import {
  handleTree,
  extractPathList,
  buildHierarchyTree,
  getNodeByUniqueId,
  appendFieldByUniqueId,
  deleteChildren
} from "@/utils/tree";

describe("handleTree", () => {
  it("returns [] for non-array input", () => {
    expect(handleTree(null as unknown as any[])).toEqual([]);
    expect(handleTree("not-array" as unknown as any[])).toEqual([]);
  });

  it("builds a 3-level tree with default field names", () => {
    const flat = [
      { id: 1, parentId: 0, name: "root1" },
      { id: 2, parentId: 0, name: "root2" },
      { id: 3, parentId: 1, name: "child1.1" },
      { id: 4, parentId: 2, name: "child2.1" },
      { id: 5, parentId: 4, name: "grandchild2.1.1" }
    ];
    const tree = handleTree(flat);
    expect(tree).toHaveLength(2);
    expect(tree[0].name).toBe("root1");
    expect(tree[0].children).toHaveLength(1);
    expect(tree[0].children[0].name).toBe("child1.1");
    expect(tree[1].name).toBe("root2");
    expect(tree[1].children[0].name).toBe("child2.1");
    expect(tree[1].children[0].children[0].name).toBe("grandchild2.1.1");
  });

  it("honors custom id / parentId / children field names", () => {
    const flat = [
      { uid: "a", upid: null, label: "A" },
      { uid: "b", upid: "a", label: "B" }
    ];
    const tree = handleTree(flat, "uid", "upid", "kids");
    expect(tree).toHaveLength(1);
    expect(tree[0].label).toBe("A");
    expect(tree[0].kids).toHaveLength(1);
    expect(tree[0].kids[0].label).toBe("B");
  });
});

describe("extractPathList", () => {
  it("returns [] for non-array input", () => {
    expect(extractPathList(null as unknown as any[])).toEqual([]);
  });

  it("returns [] for empty tree", () => {
    expect(extractPathList([])).toEqual([]);
  });

  it("returns uniqueIds of the input tree (current behavior: only top-level ids are returned because recursion discards its result)", () => {
    // 实现中递归返回值未被合并，仅 push 当前 node.uniqueId；
    // 因此该工具实际只收集传入"顶层数组"中每个 node 的 uniqueId（children 子节点的 id 不会被收集）。
    // 本测试记录该现有契约，防止回归。
    const tree = [
      { uniqueId: "0", children: [{ uniqueId: "0-0" }, { uniqueId: "0-1" }] },
      { uniqueId: "1" }
    ];
    expect(extractPathList(tree as any[])).toEqual(["0", "1"]);
  });
});

describe("buildHierarchyTree", () => {
  it("returns [] for empty input", () => {
    expect(buildHierarchyTree([])).toEqual([]);
  });

  it("returns [] for non-array input", () => {
    expect(buildHierarchyTree(null as unknown as any[])).toEqual([]);
  });

  it("overwrites id with array index, sets parentId and pathList", () => {
    // buildHierarchyTree 不会设置 uniqueId（只有 deleteChildren 会），这里只断言它设的字段。
    const flat = [
      { id: 1, parentId: 0, name: "root" },
      { id: 2, parentId: 1, name: "child" },
      { id: 3, parentId: 2, name: "grand" }
    ];
    const nested = handleTree(flat);
    const tree = buildHierarchyTree(nested);
    expect(tree).toHaveLength(1);
    const root = tree[0];
    expect(root.id).toBe(0);
    expect(root.parentId).toBeNull();
    expect(root.pathList).toEqual([0]);
    expect(root.children).toHaveLength(1);
    const child = root.children[0];
    expect(child.id).toBe(0); // 索引覆盖
    expect(child.parentId).toBe(0); // pathList 最后一个 = root.id = 0
    expect(child.pathList).toEqual([0, 0]);
    expect(child.children[0].pathList).toEqual([0, 0, 0]);
  });
});

describe("getNodeByUniqueId", () => {
  it("returns [] for empty / non-array input", () => {
    expect(getNodeByUniqueId([], "x")).toEqual([]);
    expect(getNodeByUniqueId(null as unknown as any[], "x")).toEqual([]);
  });

  it("finds the node by uniqueId via BFS", () => {
    // uniqueId 仅在 deleteChildren 后才会被设置；这里手动构造已带 uniqueId 的树。
    const tree = [
      { uniqueId: 0, children: [{ uniqueId: "0-0", name: "child" }] }
    ];
    const node = getNodeByUniqueId(tree, "0-0");
    expect(node).toBeDefined();
    expect(node.name).toBe("child");
  });

  it("returns [] when uniqueId is not found", () => {
    const tree = [{ uniqueId: 0 }];
    expect(getNodeByUniqueId(tree, "missing")).toEqual([]);
  });
});

describe("appendFieldByUniqueId", () => {
  it("returns [] for empty input", () => {
    expect(appendFieldByUniqueId([], "x", { a: 1 })).toEqual([]);
  });

  it("adds fields to the targeted node only", () => {
    const tree = [
      { uniqueId: 0, children: [{ uniqueId: "0-0", name: "child" }] }
    ];
    appendFieldByUniqueId(tree, "0-0", { tag: "marked" });
    const target = getNodeByUniqueId(tree, "0-0");
    expect(target.tag).toBe("marked");
    // 兄弟节点（根）不应被改
    expect(tree[0].tag).toBeUndefined();
  });

  it("ignores non-object fields param", () => {
    const tree = [{ uniqueId: 0, name: "root" }];
    appendFieldByUniqueId(tree, 0, null as unknown as object);
    expect(tree[0].extra).toBeUndefined();
  });
});

describe("deleteChildren", () => {
  it("returns [] for empty / non-array input", () => {
    expect(deleteChildren([])).toEqual([]);
    expect(deleteChildren(null as unknown as any[])).toEqual([]);
  });

  it("removes children arrays of length 1 and assigns uniqueId", () => {
    // 顶层只有 1 个 root -> root 不会被移除
    // 但 root 只有一个 child -> children 数组会被 delete，uniqueId 设为 pathList[0]
    const tree = buildHierarchyTree(
      handleTree([
        { id: 1, parentId: 0, name: "root" },
        { id: 2, parentId: 1, name: "child" }
      ])
    );
    // buildHierarchyTree 已把 root.id 改为 0
    expect(tree[0].id).toBe(0);
    expect(tree[0].children).toHaveLength(1);
    // deleteChildren 会删除只有一个 child 的节点下的 children，并把 uniqueId 降级为 pathList[0]
    deleteChildren(tree);
    // 顶层节点本身未在循环中处理（被检视时它自己不在 children 里），所以它的 children 在递归中被删。
    // 由于只有一个 root node，它被检视：child 是单个 -> 删除 children，uniqueId = pathList[0] = 0
    expect(tree[0].children).toBeUndefined();
    expect(tree[0].uniqueId).toBe(0);
  });

  it("preserves nodes with multiple children", () => {
    // 顶层有 2 个 root -> 它们的 children 都不会被删除（length > 1）
    const tree = buildHierarchyTree(
      handleTree([
        { id: 1, parentId: 0, name: "r1" },
        { id: 2, parentId: 1, name: "r1-child1" },
        { id: 3, parentId: 1, name: "r1-child2" }
      ])
    );
    // 顶层只有 1 个 root (id=1 -> index 0)
    expect(tree).toHaveLength(1);
    expect(tree[0].children).toHaveLength(2);
    deleteChildren(tree);
    // 仍有 2 个 child，children 数组保留；uniqueId 在子层用 pathList join
    expect(tree[0].children).toHaveLength(2);
    // 子节点的 uniqueId 来自 pathList.length > 1
    expect(tree[0].children[0].uniqueId).toBe("0-0");
  });
});