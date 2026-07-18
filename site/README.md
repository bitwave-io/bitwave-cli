# bitwave landing page

A single, self-contained static page (`index.html`, no build step, no
dependencies) modeled on [cli.github.com](https://cli.github.com): hero with
tabbed install commands, a cycling terminal demo, feature sections, and a
footer. The "Current version" line fills itself from the GitHub releases API
at view time.

Preview locally:

```sh
python3 -m http.server -d site 8000   # http://localhost:8000
```

Deploy options (pick one):

- **GitHub Pages**: repo Settings → Pages → deploy from a branch is
  root/docs-only, so either move this to `docs/`, or add a
  `actions/deploy-pages` workflow that uploads `site/` as the Pages artifact.
- **Custom domain** (e.g. `cli.bitwave.io`): point the CNAME at Pages or any
  static host and drop this file in.
