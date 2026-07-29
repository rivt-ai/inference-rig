export function basename(path: string | undefined | null) {
  if (!path) return '';
  const parts = String(path).split(/[\\/]/).filter(Boolean);
  return parts[parts.length - 1] || path;
}

export function formatDate(value: string | undefined | null) {
  if (!value) return '-';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return String(value);
  return date.toLocaleString();
}

export function formatBytes(value: number | undefined | null) {
  if (!value) return '-';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let size = Number(value);
  let unit = 0;
  while (size >= 1024 && unit < units.length - 1) {
    size /= 1024;
    unit += 1;
  }
  return `${size.toFixed(unit === 0 ? 0 : 1)} ${units[unit]}`;
}

export function formatParamCount(value: number | undefined | null) {
  if (!value) return '';
  const units = [
    { suffix: 'T', value: 1_000_000_000_000 },
    { suffix: 'B', value: 1_000_000_000 },
    { suffix: 'M', value: 1_000_000 }
  ];
  const unit = units.find((item) => value >= item.value);
  if (!unit) return value.toLocaleString();
  return `${(value / unit.value).toFixed(1).replace(/\.0$/, '')}${unit.suffix}`;
}

export function formatContextLength(value: number | undefined | null) {
  if (!value) return '';
  if (value >= 1024) return `${Math.round(value / 1024)}K`;
  return value.toLocaleString();
}
