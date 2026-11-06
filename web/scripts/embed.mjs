// Copies the built interface (web/dist) into the Go core, replacing what the embedded folder held.
import { cp, mkdir, readdir, readFile, rm } from 'node:fs/promises';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = dirname(dirname(fileURLToPath(import.meta.url)));
const source = join(root, 'dist');
const target = join(root, '..', 'core', 'internal', 'ui', 'dist');

const index = await readFile(join(source, 'index.html'), 'utf8');
if (!index.includes('<!--POUPLE_BOOT-->')) {
  throw new Error('dist/index.html lost the <!--POUPLE_BOOT--> placeholder: the core could not inject the token');
}

await mkdir(target, { recursive: true });
for (const entry of await readdir(target)) await rm(join(target, entry), { recursive: true, force: true });
await cp(source, target, { recursive: true });
console.log(`Embedded ${source} into ${target}`);
