import { spawn } from 'node:child_process';
import { once } from 'node:events';
import { writeFile } from 'node:fs/promises';
import { join } from 'node:path';

const [prompt, workspace] = process.argv.slice(2);
const child = spawn(process.execPath, ['-e', 'setInterval(() => {}, 1000)'], { stdio: 'ignore' });
await writeFile(join(workspace, 'fixture-pids.json'), JSON.stringify([process.pid, child.pid]));
process.stdout.write('fixture started\n');
await new Promise((resolve) => setTimeout(resolve, prompt.includes('hold') ? 30_000 : 200));
const stopped = once(child, 'exit');
child.kill('SIGTERM');
await stopped;
process.stdout.write('fixture completed\n');
