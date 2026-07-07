/**
 * 共享密码策略工具
 *
 * 规则：8-18 位，至少包含 数字 / 字母 / 符号 中的 2 种
 * 禁止中文字符
 *
 * 与后端 `utils.ValidatePasswordStrength` 保持一致：
 *   - MinPasswordLength = 8
 *   - MaxPasswordLength = 18
 *
 * 用法：
 *   import { REGEXP_PWD, validatePasswordStrength, PASSWORD_POLICY } from "@/utils/password";
 *
 *   // 1. 直接用正则（适合 Element Plus 表单 rules）
 *   if (!REGEXP_PWD.test(value)) callback(new Error(PASSWORD_POLICY.message));
 *
 *   // 2. 用函数（带具体错误信息）
 *   const err = validatePasswordStrength(value);
 *   if (err) return err;
 */

export const PASSWORD_MIN = 8;
export const PASSWORD_MAX = 18;

/** 密码正则：8-18 位，至少 2 种字符（数字/小写/大写/符号），排除中文 */
export const REGEXP_PWD =
  /^(?![0-9]+$)(?![a-z]+$)(?![A-Z]+$)(?!([^(0-9a-zA-Z)]|[()])+$)(?!^.*[\u4E00-\u9FA5].*$)([^(0-9a-zA-Z)]|[()]|[a-z]|[A-Z]|[0-9]){8,18}$/;

/** 完整规则描述（用户友好） */
export const PASSWORD_POLICY = {
  min: PASSWORD_MIN,
  max: PASSWORD_MAX,
  message: "密码格式应为8-18位数字、字母、符号的任意两种组合"
};

/**
 * 校验密码强度。
 * @param password 待校验密码
 * @returns null = 通过；string = 具体错误信息
 */
export const validatePasswordStrength = (password: string): string | null => {
  if (password === "" || password === undefined || password === null) {
    return "请输入密码";
  }
  if (password.length < PASSWORD_MIN) {
    return `密码长度至少 ${PASSWORD_MIN} 位`;
  }
  if (password.length > PASSWORD_MAX) {
    return `密码长度不能超过 ${PASSWORD_MAX} 位`;
  }
  if (!REGEXP_PWD.test(password)) {
    return PASSWORD_POLICY.message;
  }
  return null;
};
