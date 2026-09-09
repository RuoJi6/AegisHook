import { execFileSync } from "node:child_process";
import { readFile, readdir, writeFile } from "node:fs/promises";
import { join, resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";
const root = fileURLToPath(new URL("../", import.meta.url));
const output = process.argv[2];
if (!output) throw new Error("Usage: node scripts/write-notices.mjs OUTPUT");
let text =
  "# Third-party notices\n\nDependency notices for the Go executable and frontend packages. Some listed frontend dependencies are build-time helpers rather than shipped runtime code.\n";
async function include(name, dir) {
  const files = (await readdir(dir))
    .filter((f) => /^(licen[sc]e|copying|notice)([._-].*)?$/i.test(f))
    .sort();
  if (!files.length) {
    // This published npm version omits its license; retain the exact upstream notice.
    if (name === "@vue/devtools-api 6.6.4") {
      text += `\n## ${name}\n\n${await readFile(join(root, "scripts/licenses/vue-devtools-api-6.6.4.txt"), "utf8")}\n`;
      return;
    }
    throw new Error(`Missing license file for ${name}`);
  }
  text += `\n## ${name}\n`;
  for (const file of files)
    text += `\n### ${file}\n\n${await readFile(join(dir, file), "utf8")}\n`;
}
const modules = execFileSync(
  "go",
  ["list", "-deps", "-json", "./cmd/aegishook"],
  {
    cwd: root,
    encoding: "utf8",
  },
)
  .trim()
  .split(/\n(?=\{)/)
  .map((v) => JSON.parse(v));
const dependencies = new Map();
for (const pkg of modules)
  if (pkg.Module && !pkg.Module.Main)
    dependencies.set(pkg.Module.Path, pkg.Module);
for (const mod of dependencies.values())
  await include(`${mod.Path} ${mod.Version}`, mod.Dir);
let goRoot = execFileSync("go", ["env", "GOROOT"], { encoding: "utf8" }).trim();
if (!(await readdir(goRoot)).includes("LICENSE")) goRoot = dirname(goRoot);
await include("Go standard library", goRoot);
const npm = process.platform === "win32" ? "npm.cmd" : "npm";
const paths = execFileSync(npm, ["ls", "--omit=dev", "--all", "--parseable"], {
  cwd: join(root, "web"),
  encoding: "utf8",
})
  .trim()
  .split("\n");
for (const path of [...new Set(paths)].sort()) {
  if (resolve(path) === resolve(root, "web")) continue;
  const pkg = JSON.parse(await readFile(join(path, "package.json"), "utf8"));
  await include(`${pkg.name} ${pkg.version}`, path);
}
await writeFile(output, text);
