export function buildDiffPreview(before: string, after: string, limit = 100) {
  if (before === after) return 'No changes.';
  const oldLines = before.split('\n');
  const newLines = after.split('\n');
  const rows: string[] = [];
  for (let i = 0; i < Math.max(oldLines.length, newLines.length); i += 1) {
    if (oldLines[i] === newLines[i]) continue;
    if (oldLines[i] !== undefined) rows.push(`-${i + 1}: ${oldLines[i]}`);
    if (newLines[i] !== undefined) rows.push(`+${i + 1}: ${newLines[i]}`);
    if (rows.length >= limit) {
      rows.push('...');
      break;
    }
  }
  return rows.join('\n');
}
