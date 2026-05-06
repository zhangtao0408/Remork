const assert = require("node:assert/strict");
const path = require("node:path");
const test = require("node:test");

const wrapper = require("../bin/remork.js");

test("selects supported client binaries", () => {
  const cases = [
    [{ platform: "darwin", arch: "arm64" }, "remork-darwin-arm64"],
    [{ platform: "darwin", arch: "x64" }, "remork-darwin-amd64"],
    [{ platform: "win32", arch: "arm64" }, "remork-windows-arm64.exe"],
    [{ platform: "win32", arch: "x64" }, "remork-windows-amd64.exe"],
  ];

  for (const [runtime, expected] of cases) {
    assert.equal(wrapper.clientBinaryName(runtime), expected);
  }
});

test("declares supported package platforms", () => {
  const pkg = require("../package.json");

  assert.equal(pkg.name, "@zhangtao0408/remork");
  assert.deepEqual(pkg.os, ["darwin", "win32"]);
  assert.deepEqual(pkg.cpu, ["arm64", "x64"]);
});

test("injects daemon vendor directory", () => {
  const env = wrapper.childEnv({ FOO: "bar" }, "/tmp/remork-package");
  assert.equal(env.FOO, "bar");
  assert.equal(env.REMORK_DAEMON_VENDOR_DIR, path.join("/tmp/remork-package", "vendor"));
});

test("builds spawn plan with args and inherited stdio", () => {
  const plan = wrapper.spawnPlan({
    packageRoot: "/pkg",
    argv: ["setup", "--help"],
    platform: "darwin",
    arch: "arm64",
    env: { PATH: "/bin" },
  });

  assert.equal(plan.command, path.join("/pkg", "vendor", "remork-darwin-arm64"));
  assert.deepEqual(plan.args, ["setup", "--help"]);
  assert.equal(plan.options.stdio, "inherit");
  assert.equal(plan.options.env.REMORK_DAEMON_VENDOR_DIR, path.join("/pkg", "vendor"));
});

test("rejects unsupported client platform", () => {
  assert.throws(
    () => wrapper.clientBinaryName({ platform: "linux", arch: "x64" }),
    /unsupported Remork client platform/,
  );
});
