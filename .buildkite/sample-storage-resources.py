#!/usr/bin/env python3
"""Record and summarize the S3 storage pod's CPU and memory in the History Server e2e (see #5339).

Usage:
  sample-storage-resources.py sample <csv>                 # every 10s from the kubelet summary API, until killed
  sample-storage-resources.py summarize <csv> <gotest.log> # prints one Markdown table row
"""
import csv
import json
import re
import statistics
import subprocess
import sys
import time
from datetime import datetime

STORAGE_NAMESPACE = "minio-dev"
CLIENT_CONTAINER = "rc"
FIELDS = ["ts", "node_cpu_m", "node_ws_mib", "server_cpu_m", "server_ws_mib", "rc_ws_mib", "storage_pods"]


def kubectl(*args):
    return subprocess.run(["kubectl", *args], capture_output=True, text=True, timeout=10, check=True).stdout


def sample(path):
    node = kubectl("get", "nodes", "-o", "jsonpath={.items[0].metadata.name}")
    with open(path, "w", newline="") as f:
        w = csv.writer(f)
        w.writerow(FIELDS)
        while True:
            start = time.time()
            try:
                s = json.loads(kubectl("get", "--raw", f"/api/v1/nodes/{node}/proxy/stats/summary"))
                server_cpu = server_ws = rc_ws = pods = 0
                for pod in s.get("pods", []):
                    if pod["podRef"]["namespace"] != STORAGE_NAMESPACE:
                        continue
                    pods += 1
                    for c in pod.get("containers", []):
                        ws = (c.get("memory") or {}).get("workingSetBytes") or 0
                        if c["name"] == CLIENT_CONTAINER:
                            rc_ws += ws
                        else:
                            server_ws += ws
                            server_cpu += (c.get("cpu") or {}).get("usageNanoCores") or 0
                n = s["node"]
                w.writerow([int(start), round((n["cpu"].get("usageNanoCores") or 0) / 1e6),
                            round((n["memory"].get("workingSetBytes") or 0) / 2**20),
                            round(server_cpu / 1e6), round(server_ws / 2**20, 1), round(rc_ws / 2**20, 1), pods])
                f.flush()
            except Exception as e:  # keep sampling through API server hiccups
                print(f"sample failed: {e}", file=sys.stderr)
            time.sleep(max(0, 10 - (time.time() - start)))


def p95(values):
    values = sorted(values)
    return values[round(0.95 * (len(values) - 1))]


def summarize(path, log_path):
    rows = [{k: float(v) for k, v in r.items()} for r in csv.DictReader(open(path))]
    storage = [r for r in rows if r["storage_pods"] > 0]
    log = open(log_path, errors="replace").read()

    def logged_at(message):
        m = re.search(r"\[(\S+Z)\] " + message, log)
        return datetime.strptime(m.group(1), "%Y-%m-%dT%H:%M:%SZ") if m else None

    applied = logged_at(r"Successfully applied \.\./\.\./config/minio\.yaml")
    since_apply = lambda t: f"{(t - applied).seconds}s" if applied and t else "?"
    mem = [r["server_ws_mib"] for r in storage]
    cpu = [r["server_cpu_m"] for r in storage]
    print("| apply to Ready | apply to first write | server memory avg/p95/max (MiB) | server CPU avg/p95/max (m) "
          "| rc memory max (MiB) | node memory max (MiB) | node CPU avg (m) | failed bucket cleanups |")
    print("|---|---|---|---|---|---|---|---|")
    print(f"| {since_apply(logged_at('MinIO pods are running and ready'))} | {since_apply(logged_at('S3 API accepts writes'))} "
          f"| {statistics.mean(mem):.0f} / {p95(mem):.0f} / {max(mem):.0f} | {statistics.mean(cpu):.0f} / {p95(cpu):.0f} / {max(cpu):.0f} "
          f"| {max(r['rc_ws_mib'] for r in storage):.1f} | {max(r['node_ws_mib'] for r in rows):.0f} "
          f"| {statistics.mean(r['node_cpu_m'] for r in rows):.0f} | {log.count('Failed to delete bucket')} |")


if __name__ == "__main__":
    if sys.argv[1] == "sample":
        sample(sys.argv[2])
    else:
        summarize(sys.argv[2], sys.argv[3])
