import { describe, expect, it } from "vitest";
import {
  PASSWORD_POLICY,
  REGEXP_PWD,
  validatePasswordStrength
} from "@/utils/password";

describe("密码强度校验", () => {
  it("接受由两类字符组成的有效密码", () => {
    expect(validatePasswordStrength("abc12345")).toBeNull();
    expect(validatePasswordStrength("Abcdef!@")).toBeNull();
    expect(REGEXP_PWD.test("1234567!")).toBe(true);
  });

  it("拒绝空值和长度越界", () => {
    expect(validatePasswordStrength("")).toBe("请输入密码");
    expect(validatePasswordStrength("a1!")).toBe("密码长度至少 8 位");
    expect(validatePasswordStrength("a1!2345678901234567")).toBe(
      "密码长度不能超过 18 位"
    );
  });

  it("拒绝单一字符类型和中文字符", () => {
    expect(validatePasswordStrength("abcdefgh")).toBe(PASSWORD_POLICY.message);
    expect(validatePasswordStrength("12345678")).toBe(PASSWORD_POLICY.message);
    expect(validatePasswordStrength("密码abc123")).toBe(PASSWORD_POLICY.message);
  });
});
