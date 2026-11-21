# Report relay

Cloudflare Worker that receives "Report a problem" submissions from the launcher and files them as issues in a private GitHub repo. The GitHub token lives only in the Worker, never in the launcher.

Limits: 3 reports per IP per hour (IPs are stored only as salted hashes, for 2 hours), 50 per day in total, text fields capped at 4000 characters, logs zip at most 20 MiB and must be a zip. Logs are committed to `reports/<date>/<id>.zip` in the reports repo and linked from the issue.

## Setup (once)

1. Create a private repo for reports.
2. Create a fine-grained GitHub token limited to that repo with Contents: read/write and Issues: read/write.
3. `npx wrangler kv namespace create RL` and put the id into `wrangler.toml`; set `REPO` there.
4. `npx wrangler secret put GITHUB_TOKEN` and `npx wrangler secret put SALT` (any long random string).
5. `npx wrangler deploy`, note the `https://...workers.dev` URL.
6. Build releases with the relay URL: add `-ldflags "-X github.com/katachinosekai/pzlauncher/internal/report.RelayURL=<url>"` to the fyne-cross calls in `scripts/release.sh`.

Until step 6 the launcher's Report a problem dialog only saves the report and logs to a local zip.
