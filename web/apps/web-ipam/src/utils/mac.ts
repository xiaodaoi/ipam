/**
 * 归一化常见 MAC 书写为小写冒号格式（与后端 coherence.NormalizeMAC 语义对齐）。
 * 兼容：C4-3D-1A-07-EB-2B / C43D-1A07-EB2B / C4:3D:1A:07:EB:2B /
 *       C43D1A07EB2B / c4.3d.1a.07.eb.2b / 任意大小写混用。
 * 非法（hex 位 ≠ 12 或含非法字符）返回 null。
 */
export function normalizeMacInput(raw: string): string | null {
  const s = String(raw ?? '').trim();
  if (!s || /[^0-9a-fA-F:\-.]/.test(s)) return null;
  const hex = s.replace(/[:\-.]/g, '');
  if (hex.length !== 12) return null;
  const p = hex.toLowerCase();
  return [
    p.slice(0, 2),
    p.slice(2, 4),
    p.slice(4, 6),
    p.slice(6, 8),
    p.slice(8, 10),
    p.slice(10, 12),
  ].join(':');
}

export const MAC_PLACEHOLDER = '如 C4-3D-1A-07-EB-2B / C43D1A07EB2B / 冒号分隔均可';
