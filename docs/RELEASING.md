# Releasing bitwave

A release is one command:

```sh
git tag v0.3.0
git push origin v0.3.0
```

The `Release` workflow (`.github/workflows/release.yml`) then:

1. runs the test suite;
2. cross-compiles `bitwave` for linux/darwin (amd64+arm64) and windows/amd64
   via goreleaser, stamping the version from the tag;
3. signs + notarizes the darwin binaries **iff** the `MACOS_*` secrets exist
   (skips cleanly otherwise);
4. publishes a GitHub Release with `.tar.gz`/`.zip` archives and
   `checksums.txt` — which is what `install.sh` consumes;
5. pushes an updated cask to `bitwave-io/homebrew-tap`
   (`brew install bitwave-io/tap/bitwave`);
6. publishes to npm: `@bitwave-io/bitwave-<os>-<arch>` platform packages plus
   the `bitwave` launcher with pinned optionalDependencies
   (`npm install -g bitwave`).

Local dry-runs: `goreleaser check`, `goreleaser build --snapshot --clean`,
`node scripts/publish-npm.mjs --dry-run`.

## Version sources

- Tagged releases use the tag injected by GoReleaser, such as `0.3.0`.
- `make bitwave` defaults to `0.3.0-dev`; override with `VERSION=...`.
- Versioned `go install github.com/bitwave-io/bitwave-cli/cmd/bitwave@v0.3.0`
  builds derive `0.3.0` from Go build information when no ldflags are present.
- Plain source-checkout builds use Go's embedded module version when available,
  including a traceable pseudo-version; otherwise they report `0.3.0-dev`.
  Both forms keep telemetry and update checks disabled as development builds.

## Required repository secrets

| Secret | What / how to get it |
|---|---|
| `HOMEBREW_TAP_GITHUB_TOKEN` | Fine-grained GitHub PAT with *Contents: read & write* on `bitwave-io/homebrew-tap` only. The default `GITHUB_TOKEN` can't push to other repos. |

Without `HOMEBREW_TAP_GITHUB_TOKEN` the cask push fails (release still
completes on GitHub).

npm needs **no secret**: the workflow uses [trusted publishing
(OIDC)](https://docs.npmjs.com/trusted-publishers) — the job exchanges a
GitHub OIDC token for publish rights (`permissions: id-token: write` in
`release.yml`). Each published package (`bitwave` plus every
`@bitwave-io/bitwave-<os>-<arch>`) must list this repo's `release.yml` as a
**GitHub Actions** trusted publisher on npmjs.com. Two gotchas: passing
`registry-url` to setup-node plants a placeholder token that pre-empts OIDC
(leave it off), and configuring the trusted publisher as "GitLab CI/CD"
instead of "GitHub Actions" makes the token exchange 404 (surfaces as
`ENEEDAUTH`).

## Optional secrets — macOS signing + notarization

Until these exist, releases ship unsigned darwin binaries; the Homebrew cask
clears the quarantine attribute post-install, and `curl | sh` / npm installs
don't quarantine at all. Browser-downloaded binaries will hit Gatekeeper,
so set this up soon after launch:

1. Enroll in the [Apple Developer Program](https://developer.apple.com/programs/)
   ($99/yr, needs a D-U-N-S number for the org enrollment).
2. In Xcode or developer.apple.com → Certificates: create a
   **Developer ID Application** certificate. Export it (with private key) from
   Keychain as `.p12` with a password.
3. In [App Store Connect → Users → Integrations → Keys](https://appstoreconnect.apple.com/access/integrations/api):
   create an API key with **Developer** access; note the Issuer ID and Key ID
   and download the `.p8` once.
4. Set the secrets:

| Secret | Value |
|---|---|
| `MACOS_SIGN_P12` | `base64 < DeveloperID.p12` |
| `MACOS_SIGN_PASSWORD` | the p12 export password |
| `MACOS_NOTARY_ISSUER_ID` | App Store Connect issuer UUID |
| `MACOS_NOTARY_KEY_ID` | API key ID |
| `MACOS_NOTARY_KEY` | contents of the `.p8` file |

goreleaser signs and notarizes the mach-o binaries directly on the Linux
runner (no macOS runner needed). Presence of `MACOS_SIGN_P12` is the switch.

## One-time setup (before the first tagged release)

- [ ] Create the tap repo: `gh repo create bitwave-io/homebrew-tap --public`
      (empty is fine; goreleaser writes `Casks/bitwave.rb`).
- [ ] Set the `HOMEBREW_TAP_GITHUB_TOKEN` secret.
- [ ] Configure the npm trusted publishers (see above) for all six packages.
- [ ] `npm install -g bitwave` currently serves the v0.0.2 name-reservation
      launcher; the first release replaces it with the real thing.

## Post-release verification

```sh
brew install bitwave-io/tap/bitwave && bitwave version
npm install -g bitwave && bitwave version
curl -fsSL https://cli.bitwave.io/install.sh | sh
# darwin, once signing is live:
codesign -dv --verbose=2 "$(command -v bitwave)"
spctl -a -vv -t install "$(command -v bitwave)"
```
