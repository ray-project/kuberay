"""
Demonstrates suspending and resuming Agent Sandbox pods with GKE Pod
Snapshots from a Ray job.

Each SnapshotSandboxExecutor actor drives one simulated multi-turn agent
rollout. At every turn boundary — the moment the actor has a command result
and is waiting on the model — it suspends its sandbox (memory snapshot +
pod termination, freeing the pod's CPU/memory reservation) and resumes it
right before the next turn. The resume restores the full process tree and
memory from the snapshot, which the demo proves by starting a background
counter process on turn one and verifying that the same PID is still alive,
with its state advanced, after every suspend/resume cycle.
"""

import sys
import time
import ray

# Background process started inside the sandbox on the first turn. It holds
# state in guest memory and under /tmp — both are lost on a plain pod
# restart, but survive a GKE Pod Snapshot suspend/resume cycle.
COUNTER_DAEMON = (
    "import os, sys, time\n"
    "if os.fork() > 0:\n"
    "    sys.exit(0)  # let commands.run() return while the child keeps running\n"
    "os.setsid()\n"
    "with open('/tmp/daemon.pid', 'w') as f:\n"
    "    f.write(str(os.getpid()))\n"
    "counter = 0\n"
    "while True:\n"
    "    counter += 1\n"
    "    with open('/tmp/counter.tmp', 'w') as f:\n"
    "        f.write(str(counter))\n"
    "    os.replace('/tmp/counter.tmp', '/tmp/counter')\n"
    "    time.sleep(0.2)\n"
)

# Reads the daemon's PID and counter, raising if the process is gone.
CHECK_STATE = (
    "import os\n"
    "pid = int(open('/tmp/daemon.pid').read())\n"
    "os.kill(pid, 0)  # raises if the daemon did not survive\n"
    "print(pid, open('/tmp/counter').read())\n"
)

# The "agent turns": snippets executed between suspend/resume cycles.
CODE_SNIPPETS = [
    (
        "compute_fib.py",
        "a, b = 0, 1\n"
        "for _ in range(20):\n"
        "    a, b = b, a + b\n"
        "print(f'fib(20) = {a}')",
    ),
    (
        "json_aggregation.py",
        "import json\n"
        "data = [1, 4, 9, 16, 25]\n"
        "print(json.dumps({'mean': sum(data) / len(data), 'max': max(data)}))",
    ),
]

# Simulated model inference latency between turns. The sandbox stays
# suspended for this window, so its resources are free for other work.
MODEL_LATENCY_SECONDS = 10


@ray.remote(num_cpus=0)
class SnapshotSandboxExecutor:
    """A Ray actor that claims one sandbox and suspends/resumes it between turns."""

    def __init__(self, worker_id: int):
        from k8s_agent_sandbox.gke_extensions.snapshots import PodSnapshotSandboxClient
        from k8s_agent_sandbox.models import SandboxInClusterConnectionConfig

        self.worker_id = worker_id

        # PodSnapshotSandboxClient validates that the GKE Pod Snapshot CRDs
        # are installed and hands out sandboxes with suspend()/resume().
        self.client = PodSnapshotSandboxClient(
            connection_config=SandboxInClusterConnectionConfig(
                use_pod_ip=True,
                server_port=8888,
            ),
            cleanup=True,
        )

        t_claim = time.time()
        self.sandbox = self.client.create_sandbox(
            warmpool="python-snapshot-pool",
        )
        print(f"[executor-{worker_id}] claimed sandbox '{self.sandbox.claim_name}' in {time.time() - t_claim:.3f}s")

    def start_agent_state(self) -> tuple[int, int]:
        """Start the in-memory counter daemon and return its (pid, counter)."""
        self.sandbox.files.write("counter_daemon.py", COUNTER_DAEMON)
        self.sandbox.files.write("check_state.py", CHECK_STATE)
        res = self.sandbox.commands.run("python counter_daemon.py", timeout=10)
        if res.exit_code != 0:
            raise RuntimeError(f"failed to start counter daemon: {res.stderr}")
        time.sleep(1)  # let the daemon tick at least once
        return self.check_agent_state()

    def check_agent_state(self) -> tuple[int, int]:
        """Return the daemon's (pid, counter); raises if the daemon is gone."""
        res = self.sandbox.commands.run("python check_state.py", timeout=10)
        if res.exit_code != 0:
            raise RuntimeError(f"daemon state lost: {res.stderr}")
        pid, counter = res.stdout.split()
        return int(pid), int(counter)

    def suspend(self) -> dict:
        """Snapshot the sandbox, then terminate its pod."""
        t0 = time.time()
        res = self.sandbox.suspend(snapshot_before_suspend=True)
        if not res.success:
            raise RuntimeError(f"suspend failed: {res.error_reason}")
        snapshot_uid = res.snapshot_response.snapshot_uid if res.snapshot_response else None
        # Wait until the snapshot is reported Ready: a resume issued before
        # the upload completes would cold-start the pod instead of restoring.
        for _ in range(30):
            listed = self.sandbox.snapshots.list()
            if listed.success and any(s.snapshot_uid == snapshot_uid for s in listed.snapshots):
                break
            time.sleep(2)
        else:
            raise RuntimeError(f"snapshot {snapshot_uid} did not become ready in time")
        return {"seconds": time.time() - t0, "snapshot_uid": snapshot_uid}

    def resume(self) -> dict:
        """Recreate the pod, restoring memory from the latest snapshot."""
        t0 = time.time()
        res = self.sandbox.resume()
        if not res.success:
            raise RuntimeError(f"resume failed: {res.error_reason}")
        return {
            "seconds": time.time() - t0,
            "restored_from_snapshot": res.restored_from_snapshot,
            "snapshot_uid": res.snapshot_uid,
        }

    def execute(self, name: str, code: str, timeout: int = 10) -> dict:
        """Write the code to the sandbox and run it (one agent turn)."""
        try:
            self.sandbox.files.write(name, code)
            t0 = time.time()
            res = self.sandbox.commands.run(f"python {name}", timeout=timeout)
            return {
                "name": name,
                "exit_code": res.exit_code,
                "stdout": res.stdout,
                "stderr": res.stderr,
                "duration": time.time() - t0,
            }
        except Exception as e:
            return {
                "name": name,
                "exit_code": None,
                "error": str(e),
            }

    def cleanup(self):
        """Delete the snapshot triggers and release the sandbox."""
        self.sandbox.terminate()


def main() -> int:
    ray.init()

    NUM_EXECUTORS = 2
    executors = []

    try:
        print(f"Starting {NUM_EXECUTORS} SnapshotSandboxExecutors...")
        executors = [SnapshotSandboxExecutor.remote(worker_id=i) for i in range(NUM_EXECUTORS)]

        # Turn 0: plant in-memory state that must survive every cycle.
        states = ray.get([e.start_agent_state.remote() for e in executors])
        for i, (pid, counter) in enumerate(states):
            print(f"[executor-{i}] counter daemon running: pid={pid} counter={counter}")

        for turn, (name, code) in enumerate(CODE_SNIPPETS):
            # Turn boundary: the "model is thinking", so suspend every sandbox.
            suspends = ray.get([e.suspend.remote() for e in executors])
            for i, s in enumerate(suspends):
                print(f"[turn {turn}][executor-{i}] suspended in {s['seconds']:.1f}s (snapshot {s['snapshot_uid']})")

            print(f"[turn {turn}] sandboxes suspended, waiting {MODEL_LATENCY_SECONDS}s for 'model inference'...")
            time.sleep(MODEL_LATENCY_SECONDS)

            # Model responded: resume every sandbox and verify state survived.
            resumes = ray.get([e.resume.remote() for e in executors])
            new_states = ray.get([e.check_agent_state.remote() for e in executors])
            for i, (r, (pid, counter)) in enumerate(zip(resumes, new_states)):
                old_pid, old_counter = states[i]
                assert r["restored_from_snapshot"], f"executor-{i} was not restored from a snapshot"
                assert pid == old_pid, f"executor-{i} daemon pid changed: {old_pid} -> {pid}"
                assert counter > old_counter, f"executor-{i} counter did not advance"
                print(
                    f"[turn {turn}][executor-{i}] resumed in {r['seconds']:.1f}s from snapshot "
                    f"{r['snapshot_uid']}; daemon pid={pid} survived, counter {old_counter} -> {counter}"
                )
                states[i] = (pid, counter)

            # Execute the turn's code inside the restored sandboxes.
            results = ray.get([e.execute.remote(name, code) for e in executors])
            for i, result in enumerate(results):
                print(f"[turn {turn}][executor-{i}] {result['name']} (exit={result.get('exit_code')}): "
                      f"{(result.get('stdout') or result.get('error') or '').strip()}")

        print("\nAll turns completed: process tree and memory survived every suspend/resume cycle.")

    finally:
        if executors:
            print("\nCleaning up sandboxes and snapshot triggers...")
            try:
                ray.get([e.cleanup.remote() for e in executors])
            except Exception as e:
                print(f"Cleanup error: {e}", file=sys.stderr)

        ray.shutdown()
        print("Done.")

    return 0


if __name__ == "__main__":
    sys.exit(main())
