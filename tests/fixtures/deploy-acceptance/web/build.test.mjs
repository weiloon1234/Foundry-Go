import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { cp, mkdtemp, readFile, readdir, rm } from "node:fs/promises";
import { SourceMap } from "node:module";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import vm from "node:vm";

test("web build emits usable assets and a private release-bound source map", async (t) => {
  const root = await mkdtemp(join(tmpdir(), "foundry-acceptance-web-"));
  t.after(() => rm(root, { recursive: true }));
  await cp(new URL("./", import.meta.url), join(root, "web"), { recursive: true });
  const release = "20261110T000000Z-acceptance";
  execFileSync(process.execPath, [join(root, "web/build.mjs")], {
    env: { ...process.env, FOUNDRY_BUILD_RELEASE: release }, timeout: 10000,
  });
  const source = await readFile(join(root, "web/src/portal.js"), "utf8");
  assert.equal(await readFile(join(root, "public/assets/portal.js"), "utf8"), source);
  assert.match(await readFile(join(root, "public/index.html"), "utf8"), /src="\/assets\/portal\.js"/);
  assert.deepEqual(await readdir(join(root, "public/assets")), ["portal.js"]);
  assert.doesNotMatch(source, /sourceMappingURL/);
  const payload = JSON.parse(await readFile(join(root, `storage/private-source-maps/${release}/assets/portal.js.map`), "utf8"));
  assert.deepEqual(payload.sourcesContent, [source]);
  assert.equal(payload.file, "assets/portal.js");
  const map = new SourceMap(payload);
  for (let line = 0; line < source.trimEnd().split("\n").length; line++) {
    const entry = map.findEntry(line, 0);
    assert.equal(entry.originalSource, "src/portal.js");
    assert.equal(entry.originalLine, line);
    assert.equal(entry.originalColumn, 0);
  }
  assert.throws(() => execFileSync(process.execPath, [join(root, "web/build.mjs")], {
    env: { ...process.env, FOUNDRY_BUILD_RELEASE: "../outside" }, timeout: 10000, stdio: "pipe",
  }));
});

test("portal reports actual readiness and request failure", async () => {
  const source = await readFile(new URL("src/portal.js", import.meta.url), "utf8");
  for (const [reply, expected] of [
    [{ ok: true, json: async () => ({ ready: true }) }, "Ready"],
    [{ ok: false, json: async () => ({ ready: false }) }, "Not ready"],
    [null, "Readiness unavailable"],
  ]) {
    const status = {};
    let refresh;
    await vm.runInNewContext(source, {
      document: { querySelector: (selector) => selector === "#readiness" ? status : {
        addEventListener: (event, handler) => { assert.equal(event, "click"); refresh = handler; },
      } },
      fetch: async (path) => { assert.equal(path, "/health/ready"); if (!reply) throw new Error("offline"); return reply; },
    }, { timeout: 1000 });
    assert.equal(status.textContent, expected);
    assert.equal(typeof refresh, "function");
  }
});
