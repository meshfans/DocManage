import { http } from "@/utils/http";

// Customer is the unified customer model. The customer_type field decides
// which subset of fields is meaningful:
//   - "individual" -> real_name / id_card / gender / birth_date
//   - "enterprise" -> company_name / uscc / legal_person / ...
export type Customer = {
  id: number;
  snowid: string;
  customer_type: "individual" | "enterprise";

  // 共用
  phone: string;
  email: string;
  address: string;
  remarks: string;
  created_at: string;
  updated_at: string;

  // 个人
  real_name: string;
  id_card: string;
  gender: string;
  birth_date: string;

  // 企业
  company_name: string;
  uscc: string;
  legal_person: string;
  legal_person_id_card: string;
  registered_capital: string;
  company_type: string;
  industry: string;
  established_date: string;
  business_scope: string;
  website: string;
};

export type CustomerResult = {
  success: boolean;
  data: {
    list: Array<Customer>;
    total: number;
    page: number;
    page_size: number;
  };
};

export type CustomerDetailResult = {
  success: boolean;
  data: Customer;
};

// ==================== Legacy APIs (kept for backward compatibility) ====================

export const getCustomerList = (page: number = 1, pageSize: number = 10, keyword: string = "") => {
  return http.request<CustomerResult>("get", "/api/customer/list", {
    params: { page, page_size: pageSize, keyword }
  });
};

export const getCustomerById = (id: number) => {
  return http.request<CustomerDetailResult>("get", "/api/customer/query", {
    params: { id }
  });
};

export const createCustomer = (data: {
  real_name: string;
  phone: string;
  id_card?: string;
  address?: string;
  email?: string;
  gender?: string;
  birth_date?: string;
  age?: number;
  remarks?: string;
  username?: string;
  password?: string;
}) => {
  return http.post("/api/customer/create", data);
};

export const updateCustomer = (
  id: number,
  data: {
    real_name?: string;
    phone?: string;
    id_card?: string;
    address?: string;
    email?: string;
    gender?: string;
    birth_date?: string;
    age?: number;
    remarks?: string;
  }
) => {
  return http.post("/api/customer/update", { id, ...data });
};

export const deleteCustomer = (id: number) => {
  return http.post("/api/customer/delete", { id });
};

// ==================== New APIs (individual / enterprise) ====================

// CustomerInput is the input payload for create-ext / update-ext.
export type CustomerInput = {
  customer_type?: "individual" | "enterprise";
  phone: string;
  email?: string;
  address?: string;
  remarks?: string;
  // 个人
  real_name?: string;
  id_card?: string;
  gender?: string;
  birth_date?: string;
  // 企业
  company_name?: string;
  uscc?: string;
  legal_person?: string;
  legal_person_id_card?: string;
  registered_capital?: string;
  company_type?: string;
  industry?: string;
  established_date?: string;
  business_scope?: string;
  website?: string;
};

export type CustomerListByTypeResult = {
  success: boolean;
  data: {
    list: Array<Customer>;
    total: number;
    page: number;
    page_size: number;
    type: string;
  };
};

export const getCustomersByType = (
  ctype: "individual" | "enterprise" | "" = "",
  page: number = 1,
  pageSize: number = 10
) => {
  return http.request<CustomerListByTypeResult>("get", "/api/customer/list-by-type", {
    params: { type: ctype, page, page_size: pageSize }
  });
};

export const searchCustomersByType = (
  ctype: "individual" | "enterprise" | "" = "",
  keyword: string = "",
  page: number = 1,
  pageSize: number = 10
) => {
  return http.request<CustomerListByTypeResult>("get", "/api/customer/search-by-type", {
    params: { type: ctype, keyword, page, page_size: pageSize }
  });
};

export const createCustomerExt = (data: CustomerInput) => {
  return http.post("/api/customer/create-ext", data);
};

export const updateCustomerExt = (id: number, data: CustomerInput) => {
  return http.post("/api/customer/update-ext", { id, ...data });
};
