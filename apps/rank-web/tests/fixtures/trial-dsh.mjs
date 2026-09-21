// Deterministic CLI transcript through the real DSH adapter and isolated worker.
import { readFile } from 'node:fs/promises';
const argv = process.argv.slice(2);
const patchAt = argv.indexOf('--patch');
const patches = patchAt < 0 ? [] : JSON.parse(await readFile(argv[patchAt + 1], 'utf8'));
const driver = patches.flatMap(patch => patch.insert || []).find(patch => patch.id === 'rank-history-runner');
const history = driver ? JSON.parse(await readFile(driver.config.inputPath, 'utf8')).messages
  : [{ role: 'user', content: process.argv.at(-1).split('Task:\n').slice(1).join('Task:\n') }];
const message = history.at(-1).content;
if (message === "fail") {
  process.stderr.write("Fixture execution failed\n");
  process.exitCode = 1;
} else {
  process.stdout.write("Reply: ");
  await new Promise(resolve => setTimeout(resolve, message === "slow" ? 4000 : 200));
  process.stdout.write(message);
  if (history.length > 1) process.stdout.write(` | First: ${history[0].content}`);
}
