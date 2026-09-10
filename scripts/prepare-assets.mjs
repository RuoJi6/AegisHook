import { copyFile, cp, mkdir, rm, writeFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { join } from "node:path";
const root = fileURLToPath(new URL("../", import.meta.url));
await copyFile(join(root, "scripts/hook.sh"), join(root, "internal/assets/hooks.sh"));
await copyFile(join(root, "scripts/hook.ps1"), join(root, "internal/assets/hooks.ps1"));
const ui = join(root, "internal/assets/ui");
await rm(ui, { recursive: true, force: true });
await mkdir(ui, { recursive: true });
await cp(join(root, "web/dist"), ui, { recursive: true });
await writeFile(
  join(ui, "placeholder.txt"),
  "Generated frontend assets: run npm run build before starting the server.\n",
);
await copyFile(
  join(root, "adapter/index.ts"),
  join(root, "internal/assets/adapter.ts"),
);
await copyFile(
  join(root, "adapter/opencode.mjs"),
  join(root, "internal/assets/opencode.mjs"),
);
