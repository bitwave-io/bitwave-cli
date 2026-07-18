#!/usr/bin/env node
// Publish wavie to npm from a finished goreleaser run.
//
// Reads dist/artifacts.json, wraps each built binary in a platform package
// (@bitwave-io/wavie-<os>-<arch>), publishes those, then publishes the main
// @bitwave-io/wavie launcher (from npm/) with optionalDependencies pinned to
// the same version. Run from the repo root after `goreleaser release`.
//
//   node scripts/publish-npm.mjs [--dry-run]
//
// Auth comes from the ambient npm config (in CI: actions/setup-node writes
// .npmrc and reads NODE_AUTH_TOKEN).

import { execSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";

const DRY_RUN = process.argv.includes("--dry-run");
const SCOPE = "@bitwave-io";
const NPM_OS = { darwin: "darwin", linux: "linux", windows: "win32" };
const NPM_CPU = { amd64: "x64", arm64: "arm64" };

const artifacts = JSON.parse(fs.readFileSync("dist/artifacts.json", "utf8"));
const metadata = JSON.parse(fs.readFileSync("dist/metadata.json", "utf8"));
const version = process.env.WAVIE_NPM_VERSION || metadata.version;
if (!version) throw new Error("no version in dist/metadata.json");

const binaries = artifacts.filter(
  (a) => a.type === "Binary" && a.name.startsWith("wavie")
);
if (binaries.length === 0) throw new Error("no Binary artifacts in dist/");

const outRoot = path.join("dist", "npm");
fs.rmSync(outRoot, { recursive: true, force: true });

const optionalDependencies = {};

for (const b of binaries) {
  const os = NPM_OS[b.goos];
  const cpu = NPM_CPU[b.goarch];
  if (!os || !cpu) {
    console.log(`skipping unmapped platform ${b.goos}/${b.goarch}`);
    continue;
  }
  const name = `${SCOPE}/wavie-${os}-${cpu}`;
  const dir = path.join(outRoot, `wavie-${os}-${cpu}`);
  const binName = os === "win32" ? "wavie.exe" : "wavie";
  fs.mkdirSync(dir, { recursive: true });
  fs.copyFileSync(b.path, path.join(dir, binName));
  fs.chmodSync(path.join(dir, binName), 0o755);
  fs.writeFileSync(
    path.join(dir, "package.json"),
    JSON.stringify(
      {
        name,
        version,
        description: `wavie binary for ${os}-${cpu}. Install @bitwave-io/wavie instead of this package.`,
        license: "AGPL-3.0-only",
        repository: {
          type: "git",
          url: "git+https://github.com/bitwave-io/bitwave-cli.git",
        },
        os: [os],
        cpu: [cpu],
        // The launcher locates the binary via require.resolve(<this package>),
        // which resolves to "main".
        main: binName,
        files: [binName],
      },
      null,
      2
    ) + "\n"
  );
  optionalDependencies[name] = version;
  publish(dir);
}

// Main launcher package: npm/ contents + version + pinned optional deps.
const mainDir = path.join(outRoot, "wavie");
fs.cpSync("npm", mainDir, { recursive: true });
const pkg = JSON.parse(fs.readFileSync(path.join(mainDir, "package.json"), "utf8"));
pkg.version = version;
pkg.optionalDependencies = optionalDependencies;
fs.writeFileSync(
  path.join(mainDir, "package.json"),
  JSON.stringify(pkg, null, 2) + "\n"
);
publish(mainDir);

console.log(
  `${DRY_RUN ? "[dry-run] " : ""}published ${pkg.name}@${version} + ${
    Object.keys(optionalDependencies).length
  } platform packages`
);

function publish(dir) {
  execSync(`npm publish --access public${DRY_RUN ? " --dry-run" : ""}`, {
    cwd: dir,
    stdio: "inherit",
  });
}
