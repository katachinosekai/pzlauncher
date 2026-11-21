# Report relay

Cloudflare Worker that receives "Report a problem" submissions and files them as issues in the private repo katachinosekai/bug-reports. One relay and one repo for all apps: each report carries an `app` field that must be in `APPS` in worker.js (now `pzlauncher`); issues get the label `app:<app>` and a `[<app>]` title prefix, logs go to `reports/<app>/<date>/<id>.zip`. The GitHub token lives only in the Worker, never in the apps.

To add an app (e.g. Quill): add its name to `APPS`, `npx wrangler deploy`, and have the app POST the same multipart form (doing, happened, version, os, app, optional crash=1, optional logs.zip) to the relay URL.

Limits: 3 reports per IP per hour (IPs are stored only as salted hashes, for 2 hours), 50 per day in total, text fields capped at 4000 characters, logs zip at most 20 MiB and must be a zip. Logs are committed to `reports/<app>/<date>/<id>.zip` in the reports repo and linked from the issue.

## Setup (once)

1. Create a private repo for reports (done 2026-11-20: katachinosekai/bug-reports, first named pzlauncher-reports).
2. Create a fine-grained GitHub token limited to that repo with Contents: read/write and Issues: read/write.
3. `npx wrangler kv namespace create RL` and put the id into `wrangler.toml`; set `REPO` there.
4. `npx wrangler secret put GITHUB_TOKEN` and `npx wrangler secret put SALT` (any long random string).
5. `npx wrangler deploy`, note the `https://...workers.dev` URL.
6. Build releases with the relay URL: `scripts/release.sh` passes `-X .../internal/report.RelayURL=$RELAY` (default https://pzlauncher-report-relay.katachinosekai.workers.dev, workers.dev subdomain `katachinosekai` registered 2026-11-21) and refuses binaries without it.

Done 2026-11-21: Cloudflare account of the user, KV `RL` (id in wrangler.toml), secrets GITHUB_TOKEN (fine-grained, only bug-reports, Contents + Issues read/write; not stored anywhere else) and SALT. Dev builds (`go build`) have no relay URL, so their dialog only saves a local zip.
