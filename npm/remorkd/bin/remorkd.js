#!/usr/bin/env node
"use strict";

const fs = require("node:fs");
const path = require("node:path");
const childProcess = require("node:child_process");

function daemonBinaryName(runtime = process) {
  const platform = runtime.platform;
  const arch = runtime.arch;
  if (platform === "linux" && arch === "arm64") return "remorkd-linux-arm64";
  if (platform === "linux" && arch === "x64") return "remorkd-linux-amd64";
  throw new Error(`unsupported Remork daemon platform: ${platform}-${arch}`);
}

function packageRootFromFilename(filename = __filename) {
  return path.resolve(path.dirname(filename), "..");
}

function spawnPlan({
  packageRoot = packageRootFromFilename(),
  argv = process.argv.slice(2),
  platform = process.platform,
  arch = process.arch,
  env = process.env,
} = {}) {
  return {
    command: path.join(packageRoot, "vendor", daemonBinaryName({ platform, arch })),
    args: argv,
    options: {
      stdio: "inherit",
      env: { ...env },
    },
  };
}

function main() {
  let plan;
  try {
    plan = spawnPlan();
    if (!fs.existsSync(plan.command)) {
      throw new Error(`Remork daemon binary is missing: ${plan.command}`);
    }
  } catch (err) {
    console.error(err.message);
    process.exit(1);
  }
  const child = childProcess.spawn(plan.command, plan.args, plan.options);
  child.on("error", (err) => {
    console.error(err.message);
    process.exit(1);
  });
  child.on("exit", (code, signal) => {
    if (signal) {
      process.kill(process.pid, signal);
      return;
    }
    process.exit(code ?? 1);
  });
}

module.exports = { daemonBinaryName, packageRootFromFilename, spawnPlan, main };

if (require.main === module) {
  main();
}
