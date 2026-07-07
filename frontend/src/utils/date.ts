// 2026-07-04 新增：按 FieldContent.format 格式化日期字符串（与后端 backend/services/pdf.go 的 applyDateFormat 对齐）。
// 输入：ISO 日期字符串（"2026-07-04" / "2026/07/04"）或 Unix 秒数字符串。
// 输出：按 format 模板渲染的字符串。
// 失败：原样返回（与后端行为一致）。
export const formatDateByFormat = (
  value: string,
  format: string = "YYYY-MM-DD"
): string => {
  if (!value) return "";
  // 容错：Unix 秒数（10 位数字）→ 转 ISO
  let dateStr = value;
  if (/^\d{10}$/.test(value)) {
    dateStr = new Date(Number(value) * 1000).toISOString().slice(0, 10);
  }
  const date = new Date(dateStr);
  if (isNaN(date.getTime())) {
    return value; // 解析失败，原样返回
  }
  const year = date.getFullYear();
  const month = date.getMonth() + 1;
  const day = date.getDate();
  return format
    .replace("YYYY", String(year).padStart(4, "0"))
    .replace("yyyy", String(year))
    .replace("YY", String(year).slice(-2))
    .replace("yy", String(year).slice(-2))
    .replace("MM", String(month).padStart(2, "0"))
    .replace("mm", String(month))
    .replace("DD", String(day).padStart(2, "0"))
    .replace("dd", String(day));
};

export const calculateAge = (birthDate: string): number => {
  if (!birthDate) return 0;
  const today = new Date();
  const birth = new Date(birthDate);
  let age = today.getFullYear() - birth.getFullYear();
  const monthDiff = today.getMonth() - birth.getMonth();
  if (monthDiff < 0 || (monthDiff === 0 && today.getDate() < birth.getDate())) {
    age--;
  }
  return age;
};

export const formatTimestamp = (
  timestamp: number | string,
  format: string = "YYYY-MM-DD HH:II:SS"
): string => {
  if (!timestamp) return "";
  const date = new Date(Number(timestamp) * 1000);

  const year = date.getFullYear();
  const month = date.getMonth() + 1;
  const day = date.getDate();
  const hours = date.getHours();
  const minutes = date.getMinutes();
  const seconds = date.getSeconds();

  return format
    .replace("YYYY", String(year).padStart(4, "0"))
    .replace("yyyy", String(year))
    .replace("YY", String(year).slice(-2))
    .replace("yy", String(year).slice(-2))
    .replace("MM", String(month).padStart(2, "0"))
    .replace("mm", String(month))
    .replace("DD", String(day).padStart(2, "0"))
    .replace("dd", String(day))
    .replace("HH", String(hours).padStart(2, "0"))
    .replace("hh", String(hours))
    .replace("II", String(minutes).padStart(2, "0"))
    .replace("ii", String(minutes))
    .replace("SS", String(seconds).padStart(2, "0"))
    .replace("ss", String(seconds));
};

export const formatTimestampLang = (timestamp: number | string): string => {
  return formatTimestamp(timestamp, "YYYY-MM-DD HH:II:SS");
};

export const formatTimestampShort = (timestamp: number | string): string => {
  return formatTimestamp(timestamp, "yyyy-mm-dd");
};
