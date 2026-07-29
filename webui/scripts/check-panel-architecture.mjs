import { existsSync, readFileSync, readdirSync, statSync } from 'node:fs';
import { join, relative } from 'node:path';

const root = new URL('../web/src', import.meta.url).pathname;
const generatedUI = join(root, 'lib', 'components', 'ui');
const forbiddenTokens = [
  ['<style', 'panel-local style block'],
  ["../../components/ui/", 'legacy LlamaRig UI import'],
  ["../../components/data/", 'legacy LlamaRig data import'],
  ['<button', 'raw button; use shadcn Button'],
  ['<input', 'raw input; use shadcn Input, Checkbox, or RadioGroup'],
  ['<select', 'raw select; use shadcn Select'],
  ['<textarea', 'raw textarea; use shadcn Textarea'],
  ['<dialog', 'raw dialog; use shadcn Dialog or AlertDialog'],
  ['<details', 'raw disclosure; use shadcn Collapsible']
];
const violations = [];

function walk(dir) {
  for (const entry of readdirSync(dir)) {
    const path = join(dir, entry);
    const stat = statSync(path);
    if (stat.isDirectory()) {
      if (path !== generatedUI) walk(path);
      continue;
    }
    if (!path.endsWith('.svelte')) continue;
    const source = readFileSync(path, 'utf8');
    for (const [token, message] of forbiddenTokens) {
      if (source.includes(token)) violations.push(`${relative(root, path)}: ${message}`);
    }
  }
}

if (existsSync(root)) walk(root);

if (violations.length) {
  console.error('Frontend architecture violations:\n');
  console.error(violations.join('\n'));
  process.exit(1);
}
