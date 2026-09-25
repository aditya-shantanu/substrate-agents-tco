# Research: personal-agent workload profiles (OpenClaw, Hermes)

Compiled 2026-09-25 from web research (subagent). Facts carry source URLs;
estimates are marked. This calibrates the *workload* inputs of the cost model.

## OpenClaw (ex-Clawdbot/Moltbot)

- Architecture: single long-lived Node.js **Gateway** process (WebSocket control
  plane, ws://127.0.0.1:18789, singleton because IM protocols disallow
  concurrent sessions) + separate agent runtime; local-first state in
  `~/.openclaw`. Node 22+. ([github.com/openclaw/openclaw](https://github.com/openclaw/openclaw))
- **Idle behavior:** not purely event-driven — default **heartbeat every 30 min**
  is a full agent turn (~38–48 wakes/day, ~99% no-op)
  ([docs.openclaw.ai/gateway/heartbeat](https://docs.openclaw.ai/gateway/heartbeat),
  [issue #23254](https://github.com/openclaw/openclaw/issues/23254) — heartbeat
  token cost ~$6.84/day on Opus at defaults; mitigations: `isolatedSession`,
  `lightContext`, active-hours).
- Resources: min 2 vCPU / 4 GB (1 GB dies OOM on install); recommended 4 vCPU /
  8 GB; browser automation adds 2–4 GB. Community default host: Hetzner ~$7/mo
  (2 vCPU/4 GB) ([clawtrust.ai](https://clawtrust.ai/blog/openclaw-server-requirements)).
- **Measured RSS:** ~350 MB clean start; recent builds settle ~1.3–1.5 GiB idle
  (issues #152961, #157815); ~25 MB/h idle growth from allocator retention
  (issue #131492).

## Hermes (Nous Research "Hermes Agent")

- MIT-licensed OpenClaw-alike (gateway + runtime, 20+ chat platforms, cron
  scheduler). "Runs on a $5 VPS"; min 1 core/1 GB, recommended 2 core/2–4 GB.
  ([github.com/nousresearch/hermes-agent](https://github.com/nousresearch/hermes-agent))
- **Fleet-measured RSS** (myclaw.ai hosting, 31 containers): median 282 MB,
  min 139 MB, max 858 MB; 1.1 GB peak mid-generation; browser sidecar +8–95 MB.

## Duty-cycle evidence

- ChatGPT: ~2.5B msgs/day ÷ ~800M WAU → **~3 messages/user/day** average;
  session length ~6–12 min, 2–3 sessions/day, ~23–37 min/day total for active
  users.
- Personal-agent scheduled work: ~48 heartbeat wakes/day (OpenClaw default),
  each seconds-long; user-defined crons on top.
- **Modeling estimate:** active compute 20–60 min/day → **CPU duty cycle 2–5%**,
  but 300 MB–1.5 GB RAM held warm 24/7 unless the platform suspends. This is
  exactly the gap Substrate monetizes.

### Default workload profile for the calculator ("personal agent")

| Input | Default | Basis |
|---|---|---|
| Interactive sessions/day | 3 | ChatGPT msgs/user/day |
| Avg session (live burst) | 8 min | SimilarWeb/Semrush session lengths |
| Scheduled wakes/day | 40 | OpenClaw heartbeat minus sleep hours |
| Avg scheduled burst | 15 s | short LLM turn |
| → duty cycle | ~3.5% | (3×480 + 40×15)/86400 ≈ 2.36% live + margin |
| Sandbox RAM working set | 1 GB | Hermes median 282 MB … OpenClaw 1.5 GB |
| Snapshot size | 1–2 GB | ≈ RAM + FS delta |

## Snapshot / suspend-resume comparables

- **E2B:** pause ≈ 4 s per 1 GiB RAM, resume ~1 s; $0 while paused
  ([e2b.dev/docs/sandbox/persistence](https://e2b.dev/docs/sandbox/persistence)).
- **Fly.io suspend:** Firecracker full-VM snapshot, requires ≤2 GB RAM machine,
  resume "few hundred ms" vs ~2 s cold start
  ([docs.fly.io/reference/suspend-resume](https://docs.fly.io/reference/suspend-resume/)).
- DeltaBox paper (arxiv 2605.22781): confirms ~4 s/GiB checkpoints; GC cuts
  checkpoint storage 46–63%. Crab paper (arxiv 2604.28138): 75–87% of agent
  turns need no checkpoint.

## Pricing comparables (for the tool's benchmark row)

| Provider | Active price | Idle price | Notes |
|---|---|---|---|
| Fly.io Machines | per-second VM price | **$0.15/GB-mo rootfs only** | auto_stop=suspend, auto_start |
| E2B | $0.0504/vCPU-h + $0.0162/GiB-h | $0 paused (+storage >20 GiB) | $150/mo Pro base fee |
| Modal Sandboxes | $0.0000394/core-s + $0.0000067/GiB-s | $0 (scale-to-zero) | ~3× Modal standard rate |
| Cloudflare DO | 128 MB fixed alloc while active | $0 hibernated (WS hibernation) | wakes on message |
| Hetzner VPS (baseline) | ~$7/mo always-on 2 vCPU/4 GB | n/a | community default |

Sanity anchors for the calculator: an agent active ~1 h/day on E2B
(2 vCPU/4 GiB) ≈ **$3.5/mo**; always-on VPS ≈ **$7/mo**; so a Substrate-on-GKE
number in the low single digits $/mo (excluding LLM tokens) is the target zone.

**Caveat:** OpenClaw/Hermes specifics post-date model training; several sources
are SEO-flavored hosting blogs. GitHub issues, official docs, and arXiv papers
above are the strongest sources. LLM token costs are out of scope for this
model (they dwarf hosting at default heartbeat settings — see issue #23254).
