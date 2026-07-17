# Graceful Pod Termination for KubeRay

**Status:** §3.1 implemented, unit-tested, and verified end-to-end on a
real `kind` + KubeRay cluster (§7.2). §3.2 (controller-side drain before
`WorkersToDelete` deletion) and §3.4 (PodDisruptionBudget generation)
remain proposed but **not implemented** in this pass - deliberately
scoped out to keep the first PR small and reviewable, matching the
project's own contribution guidance. See the PR this document accompanies
for exactly what shipped.
**Author:** Ganga (<gangavh@gmail.com>)
**Created:** 2026-07-12
**Scope:** `ray-operator` (RayCluster, RayJob controllers)
**Grounding:** every claim below is cited to a real file:line in
`/Users/bigdreams/oss/kuberay` or `/Users/bigdreams/oss/ray` (both full
clones, checked out at commits current as of this document's date).
Nothing here is carried over from an earlier, Spark/Flink-shaped design —
the structure and every decision below follow from what was actually
found reading this codebase, not from a template. §2 and §3.1 were
corrected after implementation and live testing surfaced a real gap
between what the source appeared to say and what it actually does -
tracked explicitly rather than silently fixed, see §3.1.

---

## Summary

KubeRay has no concept of graceful Pod termination anywhere in it. Grepped
directly, confirmed absent: no `PodDisruptionBudget` object is ever
created (`grep -rl PodDisruptionBudget ray-operator` — zero hits), no
`TerminationGracePeriodSeconds` is ever set by the controller, no
`preStop` hook is ever injected. Every Pod KubeRay creates — head or
worker — gets Kubernetes' bare defaults: 30 seconds, no warning, no
protection against voluntary eviction.

Separately, and more surprising: even KubeRay's **own**, already-working
scale-down path — the one where Ray's own autoscaler decides to remove an
idle worker and hands KubeRay a list of Pod names via
`ScaleStrategy.WorkersToDelete` — has a real race in it today. The
autoscaler calls Ray's own graceful-drain RPC first, but with only a
5-second timeout and no guarantee the drain actually finished
(`ray/python/ray/autoscaler/_private/autoscaler.py:712`); KubeRay's
controller then does an unconditional `r.Delete(ctx, &pod)`
(`raycluster_controller.go:832`) with **zero Ray-API awareness of its
own** — it doesn't check whether the drain succeeded, is still in
progress, or was even attempted. A task that takes longer than 5 seconds
to finish can lose its work today, even on the "happy path" KubeRay
already built for this.

This document proposes closing both gaps by **reusing Ray's own
purpose-built node-draining RPC** (`DrainNode` at the GCS layer,
forwarded to `DrainRaylet` at the raylet layer) rather than inventing a
new mechanism — the same discipline as reusing `ray health-check` instead
of writing a bespoke liveness probe. This is a KubeRay-only change; no
Ray core patch is required, because the RPC and a public CLI to call it
(`ray drain-node`) already exist and are already used internally by Ray's
own autoscaler (§2).

## 1. Problem, precisely

Three separate, independently verified gaps, presented in order of how
directly they're fixed by the same underlying mechanism.

### 1.1 Kubernetes-initiated eviction is a blind kill

A node drain, a cluster-autoscaler consolidation, a spot-instance
reclaim, or a plain `kubectl delete pod` sends `SIGTERM` to the Ray
container immediately — there is no `preStop` hook to intercept it and no
non-default `terminationGracePeriodSeconds` to buy time even if there
were one. On bare `SIGTERM`, `raylet`'s handler
(`ray/src/ray/raylet/main.cc:1141-1161`) goes straight to
`shutdown_raylet_gracefully` (`main.cc:464-498`) — this disconnects
cleanly from GCS and stops services in an orderly *process* sense, but it
does **not** wait for in-flight tasks to finish. Compare this to what
Ray's own drain path does when actually invoked (§2): tasks that are
already running are left to complete, and the node only shuts down once
it's observed to be idle. Bare `SIGTERM` skips that idle-wait entirely.
The gap is not "Ray can't drain gracefully" — it's "nothing tells it to
before Kubernetes kills the process."

### 1.2 Even Ray's own autoscaler-driven removal races the delete

This is the one worth stating plainly because it means today's
"supported" path is not actually safe. `StandardAutoscaler.terminate_scheduled_nodes`
(`ray/python/ray/autoscaler/_private/autoscaler.py:631-651`) calls
`self.drain_nodes_via_gcs(self.nodes_to_terminate)` (line 641) — which
calls `self.gcs_client.drain_nodes(node_ids, timeout=5)`
(`autoscaler.py:712`, **5-second timeout**, best-effort, does not block
until the node reports idle) — and only *then* calls
`self.provider.terminate_nodes(...)` (line 643). For the KubeRay node
provider specifically, "terminate" doesn't delete a Pod directly — it
patches `workersToDelete` onto the `RayCluster` object
(`ray/python/ray/autoscaler/_private/kuberay/node_provider.py:154`).
KubeRay's `reconcilePods` then reads that list
(`raycluster_controller.go:827`) and deletes each named Pod
unconditionally (`raycluster_controller.go:832`,
`if err := r.Delete(ctx, &pod); err != nil { ... }` — no Ray API call, no
drain-status check, nothing). If the drained task takes longer than 5
seconds, the Pod delete has already been queued and the task is killed
mid-execution regardless of what the drain RPC was trying to accomplish.
**This bug exists independent of anything Kubernetes-external — it fires
on ordinary autoscaler idle-node scale-down.**

### 1.3 `RayJob` has an admitted, unfixed gap for the identical reason

`rayjob_controller.go:372-374` states it directly:

> "Currently, Ray doesn't have a best practice to stop a Ray job
> gracefully... KubeRay doesn't stop the Ray job before suspending...
> users need to set the Pod's preStop hook by themselves."

Users are being told to hand-roll exactly the mechanism this document
proposes adding natively. Once §3.1's `preStop` hook exists for ordinary
worker/head termination, `RayJob`'s suspend path gets the same protection
for free by reusing it — this is not separate work, it's the same fix
applied to a path that already asks for it in a code comment.

## 2. What Ray already provides — the mechanism this design reuses

Ray has a real, two-layer node-draining protocol. Understanding it fully
is the entire foundation of this design, so it's covered before any
proposal.

**GCS layer:** `ray.rpc.autoscaler.NodeManagerService.DrainNode`
(`ray/src/ray/protobuf/autoscaler.proto:341-373,433`). Request carries
`node_id`, a `reason` enum (`IDLE_TERMINATION` or `PREEMPTION`),
`reason_message`, and `deadline_timestamp_ms`. Handled by
`GcsAutoscalerStateManager::HandleDrainNode`
(`ray/src/ray/gcs/gcs_autoscaler_state_manager.cc:481-528`), which
forwards to the raylet.

**Raylet layer:** `ray.rpc.NodeManagerService.DrainRaylet`
(`ray/src/ray/protobuf/node_manager.proto:345-363,510`), handled by
`NodeManager::HandleDrainRaylet` (`ray/src/ray/raylet/node_manager.cc:2243-2293`).
This is where the actual behavior lives:

- Marks the node draining (`LocalResourceManager::SetLocalNodeDraining`,
  `local_resource_manager.cc:529-530`).
- **Correction (found only by running this live, not by reading the code
  alone — see the implementation notes in §3.1):** the two drain reasons
  behave very differently, and an earlier draft of this document
  conflated them. Reading `HandleDrainRaylet` precisely
  (`node_manager.cc:2243-2293`):
  - `DRAIN_NODE_REASON_IDLE_TERMINATION` is the *only* reason that checks
    idleness at all (`IsLocalNodeIdleForDrain()`, `node_manager.cc:2259`).
    If the node isn't idle, the request is **rejected outright** — not
    queued, not deferred — and the CLI exits non-zero
    (`scripts.py:2701-2704`). The caller is expected to retry.
  - `DRAIN_NODE_REASON_PREEMPTION` is **unconditionally accepted** and, in
    the same handler call, immediately cancels *every* lease in the local
    lease manager — including currently-running ones — via
    `local_lease_manager_.CancelLeasesWithoutReply(... return true ...)`
    (`node_manager.cc:2280-2291`, the predicate matches all work
    unconditionally), so the scheduler can reroute them elsewhere. It does
    **not** wait for anything.
  - There is no raylet-side "wait until idle" behavior for either reason.
    Genuine task-completion protection only exists if the *caller* builds
    it, by **polling `IDLE_TERMINATION` in a loop** until it's accepted
    (the node went idle - i.e. the task finished) or a deadline is
    reached, falling back to a single forced `PREEMPTION` call so
    eviction is never blocked past budget. §3.1 implements exactly this.
- `deadline_timestamp_ms` is advisory only — the raylet does not
  self-force-kill at the deadline; per the CLI's own help text, "it's the
  caller's responsibility to do that" (`scripts.py:2650-2655`). This
  matters directly for §3: Kubernetes' `terminationGracePeriodSeconds` is
  what actually enforces the deadline; the drain RPC's deadline parameter
  is purely informational to Ray's own scheduler.

**Callable from outside Ray's own autoscaler**, confirmed two ways:
a hidden-but-real CLI, `ray drain-node` (`scripts.py:2656-2699`), whose
own docstring says "This is a developer-facing command used to request
that GCS gracefully drain a node (e.g., before manual termination)" — the
exact scenario this document is about — and a direct Python binding,
`GcsClient.drain_node(node_id, reason, reason_message, deadline_ms)`
(`ray/python/ray/includes/gcs_client.pxi:636-654`). Exact CLI signature,
confirmed from source (`scripts.py:2606-2699`):

```text
ray drain-node \
  --address <gcs-address> \
  --node-id <hex-node-id> \
  --reason DRAIN_NODE_REASON_PREEMPTION \
  --reason-message "<why>" \
  --deadline-remaining-seconds <N>
```

(`--reason` takes the full protobuf enum value name -
`DRAIN_NODE_REASON_PREEMPTION` / `DRAIN_NODE_REASON_IDLE_TERMINATION` -
not the short form; passing the short form is accepted by no validation
and fails silently under `|| true`-style error suppression, which is
exactly how the first implementation attempt of this design broke.)

Neither surface is marked stable (`hidden=True`, no public-API
guarantee) — a real risk this design must account for (§6), not ignore.

## 3. Design

### 3.1 Worker Pod graceful termination (the clean case)

A new, opt-in `preStop` lifecycle hook, injected onto the Ray container in
every worker Pod template alongside a matching
`terminationGracePeriodSeconds`, running:

```sh
# --node-id omitted deliberately: the CLI defaults to the current node
# (scripts.py:2624-2626), so no node-ID plumbing is needed at all.
end=$(( $(date +%s) + <cli-timeout> ));
while [ "$(date +%s)" -lt "$end" ]; do
  if timeout 5s ray drain-node --reason DRAIN_NODE_REASON_IDLE_TERMINATION \
      --reason-message "Kubernetes-initiated Pod termination" \
      --deadline-remaining-seconds <drain-deadline> >/dev/null 2>&1; then
    exit 0;
  fi;
  sleep 2;
done;
timeout 5s ray drain-node --reason DRAIN_NODE_REASON_PREEMPTION \
  --reason-message "Kubernetes-initiated Pod termination" \
  --deadline-remaining-seconds <drain-deadline> || true
```

**This is a correction from an earlier draft of this document**, made
after building and live-testing the simpler version first. The original
plan was a single `PREEMPTION` call, reasoning that non-rejectable meant
"the raylet's own idle-wait gate does the waiting" — §2's correction
explains why that's wrong: `PREEMPTION` cancels running work immediately,
it doesn't wait for anything. Live-testing the single-call version
confirmed this the hard way: a task killed 3 seconds into a 15-second
sleep failed and was retried on a fresh node, indistinguishable from the
no-`preStop` baseline. The fix is the loop above: poll the *rejectable*
`IDLE_TERMINATION` reason - which only ever succeeds once the node has
actually gone idle - until it's accepted or the budget runs out, and use
`PREEMPTION` only as the final, bounded fallback so eviction is never
blocked past `terminationGracePeriodSeconds`. Verified end-to-end on a
real `kind` + KubeRay cluster: a worker Pod deleted mid-task now
completes the task in place (same Pod, same IP) before the Pod actually
terminates, instead of failing and being rescheduled (§7 has the full
timestamped evidence).

`terminationGracePeriodSeconds` itself is not currently set by the
controller at all (§1.1) — this design adds a default (proposed: 30s,
matching the `deadline-remaining-seconds` the drain RPC is told about) and
a per-`workerGroupSpec` override, following the same override-precedence
pattern GCS FT already uses (`configureGCSFaultTolerance`,
`common/pod.go:77-163`): only set a value the user's own pod template
hasn't already specified, exactly like that function's null-check-before-
default discipline.

### 3.2 Fixing the autoscaler-driven deletion race (§1.2)

This is the highest-confidence, most surgical piece of this proposal —
closer to a bug fix than a feature. Before `reconcilePods`
(`raycluster_controller.go:827-844`) issues `r.Delete(ctx, &pod)` for a
Pod named in `worker.ScaleStrategy.WorkersToDelete`, the controller should
itself call the same `DrainNode` RPC (§2) — via a GCS client from Go,
targeting the head's GCS address it already knows (it already resolves
this for other purposes) — with `reason=PREEMPTION` and a bounded wait
(not the autoscaler's 5-second best-effort call, a controller-owned
timeout, configurable, defaulting to something larger — e.g. 30s) for the
raylet to actually report itself drained/idle before proceeding to
delete. If the timeout elapses, delete anyway — same "never block
eviction past budget" principle as §3.1's Pod-side hook, just enforced
controller-side instead of Pod-side.

This does not replace the autoscaler's own `drain_nodes_via_gcs` call
(§1.2) — it's redundant with it by design, the same way §3.1's `preStop`
hook and this controller-side call are two independent paths that
converge on the same underlying RPC, giving the system two chances to get
the drain right instead of one 5-second best-effort attempt with no
verification. If the Pod's own `preStop` hook (§3.1) already completed a
drain by the time `WorkersToDelete` processing reaches it, the RPC is
idempotent — draining an already-draining or already-idle node is a
no-op, not an error.

### 3.3 Head Pod: scoped honestly

The head Pod's raylet benefits from exactly the same `preStop`/`DrainNode`
treatment as a worker's, for whatever tasks are scheduled on the head
itself (relevant only when the head has non-zero CPU/resources — many
production clusters set head CPU to 0 specifically to avoid scheduling
user work there, in which case this section is moot for that cluster).

**What this section does *not* do, stated plainly so it isn't
overclaimed:** draining the head's raylet has nothing to do with GCS
continuity. The GCS process is not a "task" the drain RPC protects —
`DrainRaylet` operates on the node's task/resource state, not on GCS's
own cluster metadata. A head Pod eviction still means the GCS is gone the
instant the Pod dies (mitigated, if at all, by GCS Fault Tolerance,
`kuberay-gcs-ft.md`) and still means every worker either fate-shares or
survives per the existing GCS-FT-dependent behavior already in the
controller (`raycluster_controller.go:1221-1234`'s note that head-Pod
deletion is only safe when GCS FT is enabled). **Reducing head-Pod-crash
downtime for RayService, or building real head failover, is a
substantially different and larger problem than this document addresses
— tracked separately, not folded in here to keep this proposal's blast
radius honest and its review scope small.**

### 3.4 PodDisruptionBudget generation

Independent of and complementary to §3.1-3.2: a `PodDisruptionBudget` per
worker group (`minAvailable` derived from `workerGroupSpec.minReplicas`,
so the group can never be evicted below its own configured floor via the
Kubernetes Eviction API) and, optionally, one for the head Pod
(`minAvailable: 1`, i.e. it can never be voluntarily evicted at all
without an explicit force-delete). This protects the subset of eviction
paths that go through the K8s **Eviction subresource** specifically
(`kubectl drain`, the cluster-autoscaler's default consolidation
behavior, some node-lifecycle controllers) — it does **not** protect
against paths that bypass the Eviction API and terminate the underlying
VM/node directly (many cloud providers' actual spot-reclaim mechanism).
This is why §3.1-3.2 (the `preStop`/drain mechanism) is the primary
defense and PDB is a secondary, narrower one — PDB stops the eviction
from being *attempted* where Kubernetes itself is the actor; the drain
hook protects the work even when the eviction can't be stopped at all.

### 3.5 RayJob (§1.3): solved as a consequence, not separately

Once §3.1's `preStop` hook exists on the underlying worker/head Pods a
`RayJob`'s cluster is built from, the exact gap named in
`rayjob_controller.go:372-374` — "users need to set the Pod's preStop
hook by themselves" — no longer requires the user to do anything. This
section exists only to record that no RayJob-specific controller code
changes further than §3.1-3.2 are needed; it is listed separately because
it's the concrete, maintainer-acknowledged evidence that this gap was
already recognized as worth fixing, not a hypothetical benefit invented
for this document.

## 4. Configuration surface

Following the existing precedent set by GCS FT's opt-in shape
(`ray.io/ft-enabled` annotation *or* a structured spec field, both
honored — `IsGCSFaultToleranceEnabled`, `util.go:765-768`):

- `spec.headGroupSpec.gracefulTermination` / `spec.workerGroupSpecs[].gracefulTermination`
  (structured, optional): `{enabled: bool, terminationGracePeriodSeconds: int, drainDeadlineSeconds: int}`.
- Annotation escape hatch `ray.io/graceful-termination-enabled` for
  parity with the FT annotation path, for users who don't want to touch
  structured spec fields.
- PDB generation (§3.4) gated by its own flag,
  `spec.workerGroupSpecs[].podDisruptionBudget.enabled` (default: follows
  whatever the graceful-termination default ends up being after
  discussion — proposed off-by-default for the very first release given
  PDB has cluster-wide scheduling implications a drain hook alone doesn't,
  then on-by-default once validated, mirroring how GCS FT itself started
  opt-in).
- Never overrides a user-supplied pod template that already sets
  `terminationGracePeriodSeconds` or a `preStop` hook — same discipline
  `configureGCSFaultTolerance` already uses for its own fields.

## 5. Failure modes and correctness

**GCS unreachable when the `preStop` hook fires.** The `ray drain-node`
call fails; the hook should not block waiting for GCS to come back — log
and exit 0 (a failed `preStop` hook does not block pod termination by
default `hookHandler` semantics, and shouldn't be made to here either) so
termination proceeds on the standard Kubernetes timeline. This degrades
to today's behavior (§1.1), not to a stuck termination.

**Drain deadline elapses before the raylet reports idle.** Exactly the
intended behavior — Kubernetes sends `SIGTERM` at
`terminationGracePeriodSeconds` regardless of drain progress, same "never
block eviction past budget" principle established in the Flink graceful-
eviction work this document deliberately does not otherwise borrow from
structurally, because the principle itself is sound independent of which
system it's applied to. This is also the case for a task that simply runs
longer than the configured budget: it is forcibly preempted at the
deadline and Ray retries it from scratch, same outcome as today's baseline
— the only difference is it was given `drainDeadlineSeconds` of runway
first instead of none. Sizing the grace period to the workload's realistic
task duration (a joint call for whoever owns the cluster and whoever owns
the workload — this document imposes no ceiling on
`terminationGracePeriodSeconds` itself) is what determines whether that
case is common or rare; it is not something this mechanism can solve on
its own. Application-level checkpointing and §3.4's PDB (which, for
evictions that go through the K8s Eviction API, blocks the eviction from
being attempted at all rather than racing a fixed countdown) are the
complementary tools for workloads whose task duration can't be bounded by
any single grace period.

**`drainDeadlineSeconds` set beyond the effective
`terminationGracePeriodSeconds`.** Since `drainDeadlineSeconds` is
independently settable from `terminationGracePeriodSeconds` (§4), and the
effective grace period can also come from a value the user already set
directly on the pod template rather than from
`GracefulTerminationOptions`, it's possible to configure a drain budget
kubelet will never actually let the hook use — it kills the `preStop`
process, and the container right behind it, the moment
`terminationGracePeriodSeconds` elapses, regardless of where the drain
loop is. Caught two ways: `utils.ResolveGracefulTerminationSeconds`
(`util.go`) resolves both values with the same precedence the Pod builder
itself uses (a pre-existing pod-template grace period wins over
`GracefulTerminationOptions`, which wins over the 30s default), and the
`RayCluster` validating webhook (`raycluster_webhook.go`) rejects the
combination outright at admission time when webhooks are enabled;
`common.configureGracefulTermination` also clamps `drainDeadlineSeconds`
down to the effective grace period defensively, so an install that
doesn't run the webhook still gets a Pod that behaves correctly rather
than one whose preStop hook is silently truncated mid-drain.

**`ray drain-node` / `GcsClient.drain_node` API instability.** Both are
explicitly unstable (`hidden=True`, no public-API guarantee, §2). This is
a real risk to a controller-side call (§3.2) more than the Pod-side CLI
call (§3.1) — a CLI invocation failing loudly is easy to make
non-blocking (previous paragraph); a Go client depending on the shape of
an internal gRPC service across Ray versions needs a compatibility matrix
and defensive handling of RPC failures (treat "can't drain" as "proceed
with today's behavior," never as "block the delete indefinitely").
Recommend pinning tested Ray version ranges explicitly in documentation,
the same discipline already implied by KubeRay's existing Ray-version
compatibility notes elsewhere in the docs.

**Idempotent double-drain.** Both §3.1 (Pod-side) and §3.2
(controller-side) may issue `DrainNode` for the same node in the same
termination sequence. Per §2's description of the handler, a second drain
request against an already-draining node is not documented as an error
case explicitly, but the state transition (`SetLocalNodeDraining`) is a
flag-set operation, not a queue — re-invoking should be a safe no-op.
**This needs empirical verification (§7) before being relied upon in
production**, not just inferred from reading the code.

## 6. What this document deliberately does not claim

- It does not make head-Pod loss cheaper for `RayService` (§3.3) — that's
  a separate, larger investigation (RayService's blue-green machinery
  already exists and is the natural place to build real failover; folding
  it into this document would make an otherwise small, reviewable change
  large and contentious).
- It does not fix the tracked upstream autoscaler/scheduler race during
  RayService HA scale-down (kuberay#2981) — different code path
  (scheduling, not termination), different root cause.
- It does not change anything about GCS Fault Tolerance, object lineage
  reconstruction, or actor checkpointing — those are Ray-core-level
  mechanisms this document's scope never touches.
- It does not claim `ray drain-node`/`GcsClient.drain_node` are stable
  public APIs — they are not, today, and this design's implementation
  must be resilient to that (§5), not pretend otherwise.

## 7. Testing strategy and verified evidence

Same empirical discipline used successfully for the analogous Flink
graceful-eviction work — unit tests are necessary but not sufficient.

### 7.1 Unit and static checks (done)

`TestConfigureGracefulTermination`, `TestIsGracefulTerminationEnabled`, and
`TestResolveGracefulTerminationSeconds`
(`controllers/ray/common/pod_test.go`, `controllers/ray/utils/util_test.go`)
cover: the feature is a no-op when disabled; `terminationGracePeriodSeconds`
defaults to 30s and is never overwritten if the user's own pod template
already sets it; the `preStop` script is never overwritten if one already
exists; custom `terminationGracePeriodSeconds`/`drainDeadlineSeconds`
values flow through to the generated script correctly; the mechanism
applies identically to head and worker Pods; a `drainDeadlineSeconds`
beyond the effective grace period is clamped rather than silently
producing a script kubelet will truncate mid-drain; a grace period the
user set directly on the pod template (bypassing
`GracefulTerminationOptions` entirely) still drives the drain budget
correctly. The `RayCluster` webhook suite (`raycluster_webhook_test.go`,
Ginkgo/envtest) additionally covers the admission-time rejection of a
`drainDeadlineSeconds` that exceeds the effective grace period. `go vet`,
`gofmt`, and `golangci-lint` are clean on every touched file.

### 7.2 E2E on a real `kind` + KubeRay cluster (done) — the evidence that matters

Two `RayCluster`s deployed side by side, identical except one has
`gracefulTerminationOptions` set (20s grace period, 20s drain deadline)
and the other doesn't. Each has a single-CPU worker and a zero-CPU head
(`num-cpus: "0"` in `rayStartParams`, so a submitted task is guaranteed to
land on the worker, not the head). A 15-second `@ray.remote` sleep task is
submitted from the head; ~3-4 seconds into its run, `kubectl delete pod`
is issued against the worker Pod running it - simulating an ordinary
voluntary eviction (node drain, spot reclaim) with the task still
mid-flight.

**Baseline (`gracefulTerminationOptions` unset):**

```text
22:10:28.515  driver submits task
22:10:28.951  task starts on worker (ip=10.244.0.22)
22:10:32      kubectl delete pod issued
22:10:32-33   pod gone almost immediately; raylet log:
              "Task slow_task failed... node it was running on is dead
               or unavailable"
22:10:43.266  Ray's own task retry starts the task over on a NEW node
              (ip=10.244.0.27)
22:10:58.339  task finishes (full second attempt)
22:10:58.426  driver gets the result
```

Total wall-clock time: **~30 seconds** for a 15-second task - one failed
attempt plus a full second attempt from zero, because nothing gave the
first attempt any time to finish.

**With graceful termination enabled**, same task, same timing of the
delete:

```text
22:11:46.395  driver submits task
22:11:46.781  task starts on worker (ip=10.244.0.26)
22:11:50      kubectl delete pod issued (~3s into the task)
22:11:50-22:12:04  pod stays Terminating (~16s - the preStop polling
                    loop retrying DRAIN_NODE_REASON_IDLE_TERMINATION
                    every 2s, being rejected while the task is still
                    running)
22:12:01.855  task finishes, in place, same worker (ip=10.244.0.26
              unchanged - not a new Pod)
22:12:01.956  driver gets the result
22:12:06      pod actually terminates (next idle-check succeeds once
              the task is done, script exits 0, SIGTERM follows)
```

Total wall-clock time: **~15.6 seconds** - essentially just the task's
own natural duration. No failure, no retry, no rescheduling. Reproduced
twice for confidence; both runs matched this shape within ~1 second.

This is the single most important number in this document: **for a task
killed partway through its work, the difference is one failed attempt
plus a full re-run from zero, versus the task simply finishing where it
was already running.** The gap scales with how far into its work the task
was when evicted - a task 90% done when evicted wastes 90% of its work in
the baseline case and 0% with this change, as long as the remaining work
fits inside the configured grace budget (§5 covers what happens when it
doesn't).

### 7.2.1 A longer task, evicted close to its own deadline (done)

The first scenario uses a 15s task against a 20s budget - comfortable
headroom. To check the mechanism still holds with a task that consumes
almost the entire configured grace period, not just a few seconds of it,
the same `kind` cluster ran a second, independent pair of experiments: a
**4-minute** (240s) `@ray.remote` task, `gracefulTerminationOptions` set
to `{terminationGracePeriodSeconds: 240, drainDeadlineSeconds: 240}`, and
`kubectl delete pod` issued at **3:50 (230s) into the task - 10 seconds
before it would finish on its own**. Same head/worker shape as §7.2
(zero-CPU head, single-CPU worker, so the task is guaranteed to land on
the worker), same `kind` cluster, task progress logged every 20s so the
run is independently verifiable from the raw log, not just the summary
below.

**Baseline (no `gracefulTerminationOptions`, default 30s grace period, no
`preStop` hook):**

```text
10:16:33  driver submits task
10:16:34  task starts on worker (ip=10.244.0.9)
10:20:15  task log: "TASK RUNNING t+220s" (last line before the delete)
10:20:23  kubectl delete pod issued (230s / 3:50 into the task)
10:20:25  pod already gone (~2s - no preStop hook, nothing slows SIGTERM)
          raylet: "Task slow_task failed... node it was running on is
          dead or unavailable. There are 3 retries remaining"
10:20:36  Ray's retry starts the task over on a NEW worker Pod
          (ip=10.244.0.11) - the 230s of already-completed work is gone
10:24:37  second attempt finishes (full 240s, from zero)
10:24:38  driver gets the result
```

Total wall-clock: **~485s (8:05)** for a 240s task - almost exactly double
the task's own duration, because the last 10 seconds of a finished attempt
were thrown away just as completely as the first 10 would have been.

**With graceful termination enabled** (`terminationGracePeriodSeconds:
240`, `drainDeadlineSeconds: 240`), identical task, identical delete
timing:

```text
10:09:42  driver submits task
10:09:43  task starts on worker (ip=10.244.0.6, pid=179)
10:13:24  task log: "TASK RUNNING t+220s" (last line before the delete)
10:13:32  kubectl delete pod issued (230s / 3:50 into the task)
10:13:32-10:13:48  pod stays Running (not Terminating-and-killed) - the
          preStop loop is polling DRAIN_NODE_REASON_IDLE_TERMINATION every
          2s and being rejected while the task is still running
10:13:44  task finishes, in place, same worker (ip=10.244.0.6, pid=179
          unchanged - not a new Pod, not a new process)
10:13:45  driver gets the result
10:13:50  pod actually terminates (idle-check succeeds once the task is
          done, script exits 0, SIGTERM follows - roughly 18s after the
          delete was issued, well inside the 240s budget)
```

Total wall-clock: **~243s (4:03)** - the task's own 240s plus about 3
seconds of overhead. No failure, no retry, no rescheduling, despite the
Pod being evicted 10 seconds before the task's natural finish.

**~2.0x faster wall-clock, and - the number that actually matters for
cost, not just latency - 230 seconds of already-completed compute thrown
away and fully redone in the baseline versus zero in the graceful case.**
Both experiments used the same driver script, the same delete-at-3:50
timing, and the same cluster; only `gracefulTerminationOptions` differs
between them. Meanwhile the RayCluster controller had already scheduled a
replacement worker Pod to restore the desired replica count in both runs -
the drain mechanism protects the in-flight task, it does not stall the
cluster's own self-healing.

### 7.3 Still to do before this feature graduates past opt-in

- **Integration (envtest):** confirm CRD defaulting/validation and
  reconciler behavior around the new field in the standard KubeRay
  integration-test harness (not yet run in this pass - the E2E evidence
  above used a real cluster directly, which is the stronger signal but
  doesn't replace envtest coverage for the CI matrix).
- **Chaos:** simulate a task that runs *longer* than the configured
  drain deadline, and confirm the `PREEMPTION` fallback still bounds
  total eviction time to `terminationGracePeriodSeconds` - i.e., that a
  pathological long-running task can't block a node drain indefinitely.
- **Idempotent double-drain:** confirm empirically (§9, open question 1)
  that a second `IDLE_TERMINATION` or `PREEMPTION` call against an
  already-draining node is a safe no-op, for the case where a future
  controller-side drain-before-delete (§8, considered and deferred) races
  with this Pod-side hook.

## 8. Alternatives considered

**Build a new drain mechanism instead of reusing `DrainNode`/`DrainRaylet`.**
Rejected outright — Ray already has exactly this, already uses it
internally (§1.2), and inventing a parallel mechanism would mean two
different systems deciding when a Ray node is "actually done with its
work," with no guarantee they agree.

**Only fix the `preStop`/PDB gap (§3.1, §3.4), leave the autoscaler race
(§3.2) alone.** Rejected: §1.2 demonstrated the race exists on Ray's
*own* supported scale-down path, independent of anything
Kubernetes-external — leaving it unfixed while adding new
Kubernetes-facing protection would be solving the less-certain problem
and ignoring a more-certain one found in the same investigation.

**A Phoenix-style external operator/sidecar instead of a native KubeRay
change.** Considered and rejected specifically for this problem (unlike
the head-pod-fencing idea explored in an earlier, discarded draft, where
an external component had a stronger justification): the mechanism this
document needs — a `preStop` hook and a `PodDisruptionBudget` — is pod-
spec-shaped, and KubeRay already constructs every Pod spec in the
codebase; there's no separate injection point to build a webhook or
sidecar around the way Spark's imperative pod creation required one. This
belongs in the controller that already owns the pod template, not beside
it.

## 9. Open questions

1. Does re-invoking `DrainNode` against an already-draining node
   (§3.1 + §3.2 both firing) behave as a safe no-op, or does it need
   explicit idempotency handling on KubeRay's side? (§5, flagged as
   needing empirical verification.)
2. What is the right default for `drainDeadlineSeconds` relative to
   `terminationGracePeriodSeconds` — should they be the same value, or
   should the drain deadline leave a safety margin the way the Flink
   design's `SAFETY_MARGIN_SECONDS` did for its own (structurally
   different) grace-period budget?
3. Is a Go GCS client call (§3.2) the right implementation shape for the
   controller-side drain, or should the controller instead exec into the
   head Pod and shell out to `ray drain-node` the same way the Pod-side
   hook does, for a single code path instead of two? Worth a short spike
   before committing to either.
4. Should PDB generation (§3.4) default on or off at GA — this is a
   judgment call about blast radius (a PDB can affect node-drain/cluster-
   upgrade behavior cluster-wide) that deserves maintainer input rather
   than being decided unilaterally in this document.
