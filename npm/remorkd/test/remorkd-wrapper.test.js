const assert = require("node:assert/strict");
const path = require("node:path");
const test = require("node:test");

const wrapper = require("../bin/remorkd.js");

test("selects supported Linux daemon binaries", () => {
  assert.equal(wrapper.daemonBinaryName({ platform: "linux", arch: "arm64" }), "remorkd-linux-arm64");
  assert.equal(wrapper.daemonBinaryName({ platform: "linux", arch: "x64" }), "remorkd-linux-amd64");
});

test("rejects unsupported server platform", () => {
  assert.throws(
    () => wrapper.daemonBinaryName({ platform: "darwin", arch: "arm64" }),
    /unsupported Remork daemon platform/,
  );
});

test("builds spawn plan with args", () => {
  const plan = wrapper.spawnPlan({
    packageRoot: "/pkg",
    argv: ["setup"],
    platform: "linux",
    arch: "x64",
    env: { PATH: "/bin" },
  });

  assert.equal(plan.command, path.join("/pkg", "vendor", "remorkd-linux-amd64"));
  assert.deepEqual(plan.args, ["setup"]);
  assert.equal(plan.options.stdio, "inherit");
});
