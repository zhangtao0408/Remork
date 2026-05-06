#!/usr/bin/env node
"use strict";

const fs = require("node:fs");
const path = require("node:path");
const childProcess = require("node:child_process");

function clientBinaryName(runtime = process) {
  const platform = runtime.platform;
  const arch = runtime.arch;
  if (platform === "darwin" && arch === "arm64") return "remork-darwin-arm64";
  if (platform === "darwin" && arch === "x64") return "remork-darwin-amd64";
  if (platform === "win32" && arch === "arm64") return "remork-windows-arm64.exe";
  if (platform === "win32" && arch === "x64") return "remork-windows-amd64.exe";
  throw new Error(`unsupported Remork client platform: ${platform}-${arch}`);
}

function packageRootFromFilename(filename = __filename) {
  return path.resolve(path.dirname(filename), "..");
}

function childEnv(baseEnv = process.env, packageRoot = packageRootFromFilename()) {
  return {
    ...baseEnv,
    REMORK_DAEMON_VENDOR_DIR: path.join(packageRoot, "vendor"),
  };
}

function spawnPlan({
  packageRoot = packageRootFromFilename(),
  argv = process.argv.slice(2),
  platform = process.platform,
  arch = process.arch,
  env = process.env,
} = {}) {
  const command = path.join(packageRoot, "vendor", clientBinaryName({ platform, arch }));
  return {
    command,
    args: argv,
    options: {
      stdio: "inherit",
      env: childEnv(env, packageRoot),
    },
  };
}

function main() {
  let plan;
  try {
    plan = spawnPlan();
    if (!fs.existsSync(plan.command)) {
      throw new Error(`Remork client binary is missing: ${plan.command}`);
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

module.exports = {
  clientBinaryName,
  childEnv,
  packageRootFromFilename,
  spawnPlan,
  main,
};

if (require.main === module) {
  main();
}
