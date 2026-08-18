import { describe, expect, it } from "vitest";
import {
  calculateAge,
  formatDateByFormat,
  formatTimestamp,
  formatTimestampLang,
  formatTimestampShort
} from "@/utils/date";

describe("日期工具", () => {
  it("按模板格式化日期并保留非法输入", () => {
    expect(formatDateByFormat("2026-08-18")).toBe("2026-08-18");
    expect(formatDateByFormat("2026-08-18", "YYYY/MM/DD")).toBe("2026/08/18");
    expect(formatDateByFormat("invalid", "YYYY-MM-DD")).toBe("invalid");
    expect(formatDateByFormat("")).toBe("");
  });

  it("格式化 Unix 时间戳", () => {
    const timestamp = Math.floor(new Date(2026, 7, 18, 9, 5, 7).getTime() / 1000);
    expect(formatTimestamp(timestamp)).toBe("2026-08-18 09:05:07");
    expect(formatTimestampLang(timestamp)).toBe("2026-08-18 09:05:07");
    expect(formatTimestampShort(timestamp)).toBe("2026-8-18");
    expect(formatTimestamp(0)).toBe("");
  });

  it("正确计算生日边界年龄", () => {
    const today = new Date();
    const bornThisDay = new Date(today.getFullYear() - 20, today.getMonth(), today.getDate());
    expect(calculateAge(bornThisDay.toISOString().slice(0, 10))).toBe(20);
    expect(calculateAge("")).toBe(0);
  });
});
