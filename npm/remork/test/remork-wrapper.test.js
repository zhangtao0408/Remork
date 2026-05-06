const assert = require("node:assert/strict");
const path = require("node:path");
const test = require("node:test");

const wrapper = require("../bin/remork.js");

test("selects macOS arm64 client binary", () => {
  assert.equal(
    wrapper.clientBinaryName({ platform: "darwin", arch: "arm64" }),
    "remork-darwin-arm64",
  );
});

test("selects Windows x64 client binary", () => {
  assert.equal(
    wrapper.clientBinaryName({ platform: "win32", arch: "x64" }),
    "remork-windows-amd64.exe",
  );
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
