import { copyFile, mkdir, readFile, writeFile } from "node:fs/promises";

const release = process.env.FOUNDRY_BUILD_RELEASE;
if (!release || !/^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/.test(release)) {
  throw new Error("FOUNDRY_BUILD_RELEASE must name the deployment release");
}
const output = new URL("../public/", import.meta.url);
const maps = new URL(`../storage/private-source-maps/${release}/assets/`, import.meta.url);
const source = await readFile(new URL("src/portal.js", import.meta.url), "utf8");
await mkdir(new URL("assets/", output), { recursive: true });
await mkdir(maps, { recursive: true });
await copyFile(new URL("src/index.html", import.meta.url), new URL("index.html", output));
await writeFile(new URL("assets/portal.js", output), source);
// The browser file is unchanged source: map each line's column zero to itself.
// AAAA selects source 0, line 0; AACA advances the original line by one.
const lines = source.trimEnd().split("\n").length;
await writeFile(new URL("portal.js.map", maps), JSON.stringify({
  version: 3,
  file: "assets/portal.js",
  sources: ["src/portal.js"],
  sourcesContent: [source],
  names: [],
  mappings: ["AAAA", ...Array(lines - 1).fill("AACA")].join(";"),
}) + "\n");
