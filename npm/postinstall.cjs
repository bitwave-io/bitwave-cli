"use strict";

// Best-effort registration of Wavie's loopback-only local CLI bridge. Package
// managers that disable lifecycle scripts still install a fully working CLI;
// support can recover with `bitwave wavie service install`.
const { spawnSync } = require("child_process");

const packages = {
  "darwin-arm64": "@bitwave-io/bitwave-darwin-arm64",
  "darwin-x64": "@bitwave-io/bitwave-darwin-x64",
  "linux-arm64": "@bitwave-io/bitwave-linux-arm64",
  "linux-x64": "@bitwave-io/bitwave-linux-x64",
  "win32-x64": "@bitwave-io/bitwave-win32-x64",
};

const packageName = packages[`${process.platform}-${process.arch}`];
if (!packageName) process.exit(0);

try {
  const binary = require.resolve(packageName);
  const result = spawnSync(
    binary,
    ["--quiet", "wavie", "service", "install"],
    { stdio: "ignore", windowsHide: true }
  );
  if (result.status !== 0) {
    console.warn("bitwave: Wavie automatic connection was not started; the CLI is still installed.");
  }
} catch (_) {
  // Optional platform packages can be unavailable during lifecycle execution.
  // The launcher will give the normal actionable error if the binary is absent.
}
