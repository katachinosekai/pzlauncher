const MAX_TEXT = 4000;
const MAX_LOGS = 20 * 1024 * 1024;
const MAX_BODY = MAX_LOGS + 256 * 1024;
const PER_IP_PER_HOUR = 3;
const PER_DAY = 50;

const json = (status, obj) =>
  new Response(JSON.stringify(obj), { status, headers: { "content-type": "application/json" } });

async function sha256hex(s) {
  const d = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(s));
  return [...new Uint8Array(d)].map((b) => b.toString(16).padStart(2, "0")).join("");
}

function base64(bytes) {
  let s = "";
  for (let i = 0; i < bytes.length; i += 0x8000) {
    s += String.fromCharCode.apply(null, bytes.subarray(i, i + 0x8000));
  }
  return btoa(s);
}

async function bump(kv, key, limit, ttl) {
  const n = parseInt((await kv.get(key)) || "0", 10);
  if (n >= limit) return false;
  await kv.put(key, String(n + 1), { expirationTtl: ttl });
  return true;
}

async function github(env, method, path, body) {
  const r = await fetch("https://api.github.com/repos/" + env.REPO + path, {
    method,
    headers: {
      authorization: "Bearer " + env.GITHUB_TOKEN,
      accept: "application/vnd.github+json",
      "user-agent": "pzlauncher-report-relay",
      "content-type": "application/json",
    },
    body: JSON.stringify(body),
  });
  if (!r.ok) throw new Error("GitHub " + path + ": " + r.status);
  return r.json();
}

const field = (form, name, max) => {
  const v = form.get(name);
  return typeof v === "string" ? v.slice(0, max) : "";
};

export default {
  async fetch(req, env) {
    if (req.method !== "POST") return json(405, { error: "POST only" });
    const len = parseInt(req.headers.get("content-length") || "0", 10);
    if (!len || len > MAX_BODY) return json(413, { error: "report too large" });

    const now = new Date();
    const day = now.toISOString().slice(0, 10);
    const hour = now.toISOString().slice(0, 13);
    const ip = await sha256hex((req.headers.get("cf-connecting-ip") || "?") + env.SALT);
    if (!(await bump(env.RL, "ip:" + ip.slice(0, 32) + ":" + hour, PER_IP_PER_HOUR, 7200))) {
      return json(429, { error: "too many reports from you this hour" });
    }
    if (!(await bump(env.RL, "day:" + day, PER_DAY, 172800))) {
      return json(429, { error: "daily report limit reached" });
    }

    let form;
    try {
      form = await req.formData();
    } catch {
      return json(400, { error: "bad form" });
    }
    const doing = field(form, "doing", MAX_TEXT).trim();
    const happened = field(form, "happened", MAX_TEXT).trim();
    const version = field(form, "version", 40);
    const os = field(form, "os", 40);
    const crash = form.get("crash") === "1";
    if (!doing && !happened) return json(400, { error: "empty report" });

    let logsPath = "";
    const logs = form.get("logs");
    if (logs && typeof logs !== "string") {
      if (logs.size > MAX_LOGS) return json(413, { error: "logs too large" });
      const bytes = new Uint8Array(await logs.arrayBuffer());
      if (bytes.length < 4 || bytes[0] !== 0x50 || bytes[1] !== 0x4b || bytes[2] !== 3 || bytes[3] !== 4) {
        return json(400, { error: "logs must be a zip" });
      }
      const id = now.toISOString().replace(/[-:.TZ]/g, "") + "-" + crypto.randomUUID().slice(0, 8);
      logsPath = "reports/" + day + "/" + id + ".zip";
      await github(env, "PUT", "/contents/" + logsPath, { message: "report " + id, content: base64(bytes) });
    }

    const title = (crash ? "[crash] " : "") + (happened || doing).split("\n")[0].slice(0, 80);
    const body =
      "Launcher: `" + version + "`  OS: `" + os + "`" + (crash ? "  (offered after a game crash)" : "") +
      "\n\n### What were you doing?\n" + (doing || "_(empty)_") +
      "\n\n### What happened?\n" + (happened || "_(empty)_") +
      "\n\n" + (logsPath ? "Logs: [`" + logsPath + "`](../blob/HEAD/" + logsPath + ")" : "No logs sent.");
    await github(env, "POST", "/issues", { title, body });
    return json(200, { ok: true });
  },
};
