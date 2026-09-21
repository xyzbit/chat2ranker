import { strToU8, zipSync } from "fflate";

const sources = import.meta.glob([
  "../../../rank/examples/responses-client/demo-client.mjs",
  "../../../rank/examples/responses-client/demo-server.mjs",
  "../../../rank/examples/responses-client/daily-news.mjs",
  "../../../rank/examples/responses-client/demo.test.mjs",
  "../../../rank/examples/responses-client/verify-demos.mjs",
  "../../../rank/examples/responses-client/package.json",
  "../../../rank/examples/responses-client/package-lock.json",
  "../../../rank/examples/responses-client/web/*",
  "../../../rank/examples/responses-client/snapshots/demo-chat.json",
  "../../../rank/examples/responses-client/download/README*.md",
], { query: "?raw", import: "default", eager: true });

/** Builds a standalone ZIP from an explicit source allowlist; no credentials are accepted. */
export function exampleBundle(kind, endpoint, origin) {
  if (!["news", "chat"].includes(kind)) throw new Error("Unknown example");
  if (/[\r\n'"`#]/.test(endpoint)) throw new Error("Unsupported endpoint name for .env");
  const platform = new URL(origin).origin;
  const root = `rank-${kind}`;
  const files = {};
  for (const [path, content] of Object.entries(sources)) {
    const name = path.split("responses-client/")[1].replace(/^download\//, "");
    files[name] = content;
  }
  const pkg = JSON.parse(files["package.json"]);
  pkg.name = root;
  pkg.scripts = { start: "node --env-file-if-exists=.env demo-server.mjs", demo: "node --env-file-if-exists=.env demo-server.mjs", news: "node --env-file-if-exists=.env daily-news.mjs", test: "node --test demo.test.mjs", "verify:demos": "node --env-file-if-exists=.env verify-demos.mjs" };
  files["package.json"] = JSON.stringify(pkg, null, 2) + "\n";
  const lock = JSON.parse(files["package-lock.json"]);
  lock.name = root;
  lock.packages[""].name = root;
  files["package-lock.json"] = JSON.stringify(lock, null, 2) + "\n";
  files[".env.example"] = `RANK_BASE_URL=${platform}/v1\nRANK_MODEL='${endpoint}'\nRANK_API_KEY=\nRANK_PLATFORM_URL=${platform}\nDEMO_KIND=${kind}\nDEMO_PORT=${kind === "news" ? 4181 : 4182}\n`;
  files[".gitignore"] = ".env\nnode_modules/\noutput/\n";
  return { name: root + ".zip", bytes: zipSync(Object.fromEntries(Object.entries(files).map(([name, content]) => [`${root}/${name}`, strToU8(content)]))) };
}
