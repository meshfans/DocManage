/**
 * 后端统一响应包装：`utils.Success(c, gin.H{...})` / `utils.Error(...)` 的 JSON 形态。
 *
 * - success：业务成功标记（true / false）
 * - data：业务数据载荷，可空（错误响应通常没有 data）
 * - message：人类可读的状态消息（成功 / 失败都可携带），可空
 *
 * 所有 list / detail / write 端点统一用此 envelope 返回。
 * 调用方按需取 `res.data` / `res.message`，axios 拦截器已 unwrap `response.data`。
 */
export interface ApiEnvelope<T> {
  success: boolean;
  data?: T;
  message?: string;
}