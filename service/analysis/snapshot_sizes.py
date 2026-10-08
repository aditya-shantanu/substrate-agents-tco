#!/usr/bin/env python3
"""Per-agent snapshot size and its breakdown for one run's fleet, while the
fleet still exists.

  snapshot_sizes.py --prefix pa-...-microvm-pause --lifecycle pause|suspend \
      --bucket snapshot-... --out results/<run>/   [--atespace agents-sim]

suspend: objects under gs://<bucket>/benchmark-workloads/glutton/atespaces/
<atespace>/actors/<uid>/snapshots/<id>/*, grouped by file name (microVM:
memory-ranges.zstd, rootfs-upper.tar.zstd, …; gVisor: its checkpoint files).
pause: the node-local checkpoint files under /var/lib/ate/actors/<uid>/,
read through a privileged probe pod on the actor's node (one node here).
Uses the current KUBECONFIG; kubectl-ate from $SUBSTRATE_REPO/bin.

Writes <out>/snapshots.json and prints a markdown summary line.
"""
import argparse
import collections
import json
import os
import re
import statistics
import subprocess
import sys
import time

ATE = os.path.expanduser(os.environ.get("KUBECTL_ATE", "~/repos/substrate/bin/kubectl-ate"))


def sh(cmd, **kw):
    return subprocess.run(cmd, capture_output=True, text=True, **kw)


def actors(atespace, prefix):
    r = sh([ATE, "get", "actors", "-a", atespace, "-o", "json"])
    d = json.loads(r.stdout)
    items = d.get("actors") or d.get("items") or d
    return [(a["metadata"]["name"], a["metadata"]["uid"], a.get("status", {})) for a in items
            if a["metadata"]["name"].startswith(prefix + "-")]


def pct(v, p):
    return v[min(len(v) - 1, max(0, int(p * len(v)) - 1))] if v else 0


def summarize(per_actor_files):
    """per_actor_files: {uid: {basename: bytes}} (latest snapshot of each actor)."""
    totals = sorted(sum(f.values()) for f in per_actor_files.values())
    by_file = collections.defaultdict(list)
    for files in per_actor_files.values():
        for name, b in files.items():
            by_file[name].append(b)
    breakdown = {name: {"mean": statistics.fmean(v), "p50": pct(sorted(v), .5), "max": max(v), "n": len(v)}
                 for name, v in by_file.items()}
    return {"actors": len(totals), "total_mean": statistics.fmean(totals) if totals else 0,
            "total_p50": pct(totals, .5), "total_p90": pct(totals, .9), "total_max": totals[-1] if totals else 0,
            "breakdown": dict(sorted(breakdown.items(), key=lambda kv: -kv[1]["mean"]))}


def suspend_sizes(bucket, atespace, uids, template="glutton"):
    root = f"gs://{bucket}/benchmark-workloads/{template}/atespaces/{atespace}/actors/"
    r = sh(["gcloud", "storage", "ls", "-l", "-r", root])
    per = collections.defaultdict(lambda: collections.defaultdict(dict))  # uid -> snapid -> {file: bytes}
    for line in r.stdout.splitlines():
        m = re.match(r"\s*(\d+)\s+\S+\s+gs://\S+/actors/([^/]+)/snapshots/([^/]+)/(\S+)", line)
        if m and m[2] in uids:
            per[m[2]][m[3]][m[4]] = int(m[1])
    latest = {}
    for uid, snaps in per.items():
        # the snapshot with the most bytes is the current full one (deltas are smaller)
        latest[uid] = max(snaps.values(), key=lambda f: sum(f.values()))
    return latest, {uid: len(s) for uid, s in per.items()}


def pause_sizes(uids):
    node = sh(["kubectl", "get", "nodes", "-o", "jsonpath={.items[0].metadata.name}"]).stdout.strip()
    uid_list = " ".join(sorted(uids))
    # busybox find has no -printf: stat each file instead
    script = ("cd /host/var/lib/ate/actors || exit 1; for u in " + uid_list + "; do "
              "[ -d \"$u\" ] || continue; find \"$u\" -type f 2>/dev/null | while read -r f; do "
              "echo \"$u $(stat -c %s \"$f\") $f\"; done; done")
    pod = {"apiVersion": "v1", "kind": "Pod", "metadata": {"name": "snapprobe", "namespace": "default"},
           "spec": {"nodeName": node, "restartPolicy": "Never", "tolerations": [{"operator": "Exists"}],
                    "containers": [{"name": "p", "image": "busybox", "securityContext": {"privileged": True},
                                    "command": ["sh", "-c", script],
                                    "volumeMounts": [{"name": "host", "mountPath": "/host"}]}],
                    "volumes": [{"name": "host", "hostPath": {"path": "/"}}]}}
    sh(["kubectl", "delete", "pod", "snapprobe", "--ignore-not-found", "--wait=true"])
    sh(["kubectl", "apply", "-f", "-"], input=json.dumps(pod))
    for _ in range(60):
        ph = sh(["kubectl", "get", "pod", "snapprobe", "-o", "jsonpath={.status.phase}"]).stdout
        if ph in ("Succeeded", "Failed"):
            break
        time.sleep(3)
    out = sh(["kubectl", "logs", "snapprobe"]).stdout
    sh(["kubectl", "delete", "pod", "snapprobe", "--wait=false"])
    per = collections.defaultdict(dict)  # uid -> {relative path: bytes}
    for line in out.splitlines():
        parts = line.split(" ", 2)
        if len(parts) == 3:
            uid, size, path = parts
            per[uid][path[len(uid) + 1:]] = int(size)
    # group by basename within the newest checkpoint dir: keep the largest
    # file per basename (a paused actor keeps base + delta generations)
    latest = {}
    for uid, files in per.items():
        agg = collections.defaultdict(int)
        for path, b in files.items():
            agg[os.path.basename(path)] = max(agg[os.path.basename(path)], b)
        latest[uid] = dict(agg)
    return latest, {uid: len(f) for uid, f in per.items()}


def fmt(b):
    return f"{b / 2**20:,.0f} MiB"


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--prefix", required=True)
    ap.add_argument("--lifecycle", required=True, choices=["pause", "suspend"])
    ap.add_argument("--bucket", required=True)
    ap.add_argument("--atespace", default="agents-sim")
    ap.add_argument("--template", default="glutton", help="ActorTemplate name (its snapshotConfig.storageLocation prefix under benchmark-workloads/)")
    ap.add_argument("--out", required=True)
    a = ap.parse_args()
    acts = actors(a.atespace, a.prefix)
    uids = {uid for _, uid, _ in acts}
    states = collections.Counter(st.get("state", "?") for _, _, st in acts)
    if not uids:
        sys.exit(f"no actors with prefix {a.prefix}")
    latest, gens = (pause_sizes(uids) if a.lifecycle == "pause" else suspend_sizes(a.bucket, a.atespace, uids, a.template))
    s = summarize(latest)
    s.update({"prefix": a.prefix, "lifecycle": a.lifecycle, "states": dict(states),
              "files_or_generations_per_actor_mean": statistics.fmean(gens.values()) if gens else 0,
              "measured_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())})
    os.makedirs(a.out, exist_ok=True)
    with open(os.path.join(a.out, "snapshots.json"), "w") as f:
        json.dump(s, f, indent=1)
    parts = ", ".join(f"{name} {fmt(v['mean'])}" for name, v in list(s["breakdown"].items())[:4])
    print(f"{a.lifecycle}: {s['actors']} actors, per agent mean {fmt(s['total_mean'])} (p50 {fmt(s['total_p50'])}, max {fmt(s['total_max'])}); breakdown (mean per actor): {parts}")


if __name__ == "__main__":
    main()
