# npm support request — release the name `wavie`

Submit while logged in as the publishing account at
<https://www.npmjs.com/support> (category: package name / publishing issue).
Status: **not yet submitted** as of 2026-07-18.

---

**Subject:** Request to allow publishing the unregistered package name `wavie`

Hello,

I'd like to publish the package name `wavie`, which is currently
unregistered but blocked by the registry's name-similarity check:

    npm error 403 Forbidden - PUT https://registry.npmjs.org/wavie -
    Package name too similar to existing package save

`wavie` is not a typosquat of `save` — it is the brand name of our
product: Wavie, the plain-text accounting CLI by Bitwave
(https://bitwave.io). The package is a real, functional CLI, already
published under our org scope while we resolve this:

- Published package: https://www.npmjs.com/package/@bitwave-io/wavie
- Source repository: https://github.com/bitwave-io/bitwave-cli
- License: AGPL-3.0-only

We're launching publicly next week and `npm install wavie` is our
intended install path. The unscoped package would ship identical
content to `@bitwave-io/wavie` (a launcher that selects a platform
binary via optionalDependencies — no install scripts).

Requesting account: `sassame` (or transfer to the `bitwave-io` org,
whichever is easier on your side).

Thanks!

---

## After npm grants the name

1. In `npm/package.json`, change `name` back to `wavie` (keep everything
   else; platform packages stay under `@bitwave-io/`).
2. Publish immediately (any placeholder version counts — the grant can
   otherwise lapse back into the block).
3. Keep `@bitwave-io/wavie` published as an alias, or deprecate it with
   `npm deprecate @bitwave-io/wavie "use 'wavie' instead"` after a few
   releases.
