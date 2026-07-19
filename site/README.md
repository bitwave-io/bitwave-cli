# bitwave landing page

A single self-contained `index.html` (no build step) modeled on
cli.github.com. The install box's version number self-fills from the
GitHub releases API at view time.

## Preview locally

```sh
python3 -m http.server -d site 8000
```

## Deploy (Firebase Hosting)

Lives on the `bitwave-cli` Hosting site in the `bitwave-prod` project
(target name `cli`, see `.firebaserc` + `firebase.json` at the repo root):

```sh
firebase deploy --only hosting:cli --project bitwave-prod
```

Live at <https://bitwave-cli.web.app>; the custom domain `cli.bitwave.io`
is attached via Firebase console → Hosting → bitwave-cli → Add custom
domain (requires the DNS records the console prints).
