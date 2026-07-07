import { http } from "@/utils/http";
import { getToken } from "@/utils/auth";

// 第十阶段：第三方合同（v5 精简版）
// 对方信息完全靠 customer 联表查；6 个对方文本字段已砍

/**
 * 联表返回的 customer 精简字段（v5 第十阶段列表 join 用）
 * - 列表接口额外返回 customer_map（id → lite），前端按行 customer_id 索引
 * - 不传全表字段，省序列化 + 省前端内存
 */
export type CustomerLite = {
  id: number;
  customer_type: "individual" | "enterprise" | string;
  real_name: string;
  company_name: string;
  phone: string;
  email: string;
};

export type ThirdPartyContract = {
  id: number;
  contract_no: string;
  title: string;
  type: "paper" | "electronic";
  status: "draft" | "pending" | "signed" | "archived" | "cancelled";
  customer_id: number;

  // 联表字段：v5 第十阶段列表接口额外返回 customer_map，
  // 前端按 row.customer_id 索引（不嵌在每行内，省一次拷贝）
  customer?: CustomerLite;

  amount: number;
  currency: string;
  sign_date: number;
  start_date: number;
  end_date: number;

  // 文件（v5 必填）
  file_path: string;
  file_size: number;
  file_sm3_hash: string;        // SM3
  file_sha256_hash: string;     // SHA-256
  file_combined_hash: string;   // combined = SHA256(SM3 || SHA256)

  remark: string;
  created_by: number;
  created_at: number;
  updated_at: number;
};

export type ThirdPartyContractListResult = {
  success: boolean;
  data: {
    list: ThirdPartyContract[];
    total: number;
    page: number;
    page_size: number;
    // 客户信息 map：v5 新增（key = customer_id）
    customer_map?: Record<string, CustomerLite>;
  };
};

export type ThirdPartyContractDetailResult = {
  success: boolean;
  data: ThirdPartyContract;
};

// ==================== 9 个 API ====================

export const listThirdPartyContracts = (params: {
  customer_id?: number;
  customer_type?: "individual" | "enterprise" | "";
  status?: string;
  search?: string;
  page?: number;
  page_size?: number;
}) => {
  return http.request<ThirdPartyContractListResult>(
    "get",
    "/api/third-party/contracts",
    { params }
  );
};

export const createThirdPartyContract = (data: {
  title: string;
  type?: "paper" | "electronic";
  status?: "draft" | "pending" | "signed" | "archived" | "cancelled";
  customer_id: number;
  amount?: number;
  currency?: string;
  sign_date?: number;
  start_date?: number;
  end_date?: number;
  // 文件字段（v5 必填）
  file_path: string;
  file_size: number;
  file_sm3_hash: string;
  file_sha256_hash: string;
  file_combined_hash: string;
  remark?: string;
}) => {
  return http.request<{
    success: boolean;
    data: { id: number; data: ThirdPartyContract };
  }>("post", "/api/third-party/contracts", { data });
};

export const getThirdPartyContract = (id: number) => {
  return http.request<ThirdPartyContractDetailResult>(
    "get",
    `/api/third-party/contracts/${id}`
  );
};

export const updateThirdPartyContract = (
  id: number,
  data: {
    title: string;
    type: "paper" | "electronic";
    customer_id: number;
    amount?: number;
    currency?: string;
    sign_date?: number;
    start_date?: number;
    end_date?: number;
    remark?: string;
  }
) => {
  return http.request("post", `/api/third-party/contracts/${id}`, { data });
};

export const changeThirdPartyContractStatus = (id: number, status: string) => {
  return http.request("post", `/api/third-party/contracts/${id}/status`, {
    data: { status }
  });
};

export const deleteThirdPartyContract = (id: number) => {
  return http.request("post", `/api/third-party/contracts/${id}/delete`);
};

export const uploadThirdPartyContractPdf = (
  id: number,
  pdfBase64: string
) => {
  return http.request<{
    success: boolean;
    data: {
      file_size: number;
      file_sm3_hash: string;
      file_sha256_hash: string;
      file_combined_hash: string;
      file_path: string;
    };
  }>("post", `/api/third-party/contracts/${id}/upload`, {
    data: { pdf_base64: pdfBase64 }
  });
};

export const downloadThirdPartyContractPdf = (id: number) => {
  const token = getToken();
  return http.request("get", `/api/third-party/contracts/${id}/download`, {
    responseType: "blob",
    headers: token
      ? { Authorization: `Bearer ${token.accessToken}` }
      : {}
  });
};

export const bulkDownloadThirdPartyContracts = (payload: {
  ids?: number[];
  search?: string;
  status?: string;
  customer_id?: number;
}) => {
  const token = getToken();
  return http.request("post", `/api/third-party/contracts/bulk-download`, {
    data: payload,
    responseType: "blob",
    headers: token
      ? { Authorization: `Bearer ${token.accessToken}` }
      : {}
  });
};

// ==================== 状态机辅助 ====================

export const TP_STATUS_OPTIONS = [
  { value: "draft", label: "草稿", type: "info" },
  { value: "pending", label: "待签署", type: "warning" },
  { value: "signed", label: "已签署", type: "success" },
  { value: "archived", label: "归档", type: "" },
  { value: "cancelled", label: "取消", type: "danger" }
] as const;

/** 合法状态转换表 */
const TP_TRANSITIONS: Record<string, string[]> = {
  draft: ["pending", "cancelled"],
  pending: ["signed", "cancelled"],
  signed: ["archived"],
  archived: [],
  cancelled: []
};

export function canTPTransition(from: string, to: string): boolean {
  return (TP_TRANSITIONS[from] ?? []).includes(to);
}

/** 哪些状态可删除（仅 draft / cancelled） */
export function canTPDelete(status: string): boolean {
  return status === "draft" || status === "cancelled";
}
