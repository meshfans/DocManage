import { http } from "@/utils/http";

export type RouteNode = {
  path: string;
  name?: string;
  redirect?: string;
  component?: string;
  meta?: {
    title?: string;
    icon?: string;
    rank?: number;
    roles?: string[];
    auths?: string[];
    showParent?: boolean;
    keepAlive?: boolean;
  };
  children?: RouteNode[];
};

export type GetAsyncRoutesResponse = {
  success: boolean;
  data: {
    list: RouteNode[];
    permission_version: string;
  };
};

export const getAsyncRoutes = () => {
  return http.request<GetAsyncRoutesResponse>("get", "/api/get-async-routes");
};

export const getPermissionVersion = () => {
  return http.request<{ success: boolean; data: { version: string } }>(
    "get",
    "/api/rbac/permission-version"
  );
};