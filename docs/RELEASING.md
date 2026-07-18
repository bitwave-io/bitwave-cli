# Releasing bitwave

A release is one command:

```sh
git tag v0.2.0
git push origin v0.2.0
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

## Required repository secrets

| Secret | What / how to get it |
|---|---|
| `NPM_TOKEN` | npm **granular access token** with *Read and write* on the `bitwave` package **and** on *all packages* in the `@bitwave-io` org — the first release *creates* the platform packages, and a token scoped to existing packages only can't create new ones. npmjs.com → Access Tokens → Generate → Granular. Set a long expiry; rotate on a calendar. |
| `HOMEBREW_TAP_GITHUB_TOKEN` | Fine-grained GitHub PAT with *Contents: read & write* on `bitwave-io/homebrew-tap` only. The default `GITHUB_TOKEN` can't push to other repos. |

Without `NPM_TOKEN` the npm step fails (release still completes on GitHub);
without `HOMEBREW_TAP_GITHUB_TOKEN` the cask push fails. Set both before the
first tag.

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
- [ ] Set `NPM_TOKEN` and `HOMEBREW_TAP_GITHUB_TOKEN` secrets.
- [ ] `npm install -g bitwave` currently serves the v0.0.2 name-reservation
      launcher; the first release replaces it with the real thing.

## Post-release verification

```sh
brew install bitwave-io/tap/bitwave && bitwave version
npm install -g bitwave && bitwave version
curl -fsSL https://raw.githubusercontent.com/bitwave-io/bitwave-cli/main/install.sh | sh
# darwin, once signing is live:
codesign -dv --verbose=2 "$(command -v bitwave)"
spctl -a -vv -t install "$(command -v bitwave)"
```
