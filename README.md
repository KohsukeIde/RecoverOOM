# RecoverOOM

**RecoverOOM is an event-driven service for GKE that automatically recovers `OOMKilled` CronJob workloads by raising their memory limits and re-running the failed job — without manual intervention.**

When a CronJob pod is killed for exceeding its memory limit, RecoverOOM traces the pod back to its owning CronJob, doubles the container memory limit in the CronJob spec, applies the change, and immediately kicks off a fresh job from the updated template.

> **Relation to [m3dev/broom](https://github.com/m3dev/broom)**
> RecoverOOM solves the same problem as broom (auto-remediating OOM-killed CronJobs), and was built independently as a learning/operations project. The two differ in approach: broom is a Kubernetes *custom controller* configured through a CRD, while RecoverOOM is a *Pub/Sub-driven service* that reacts to GKE log events and drives changes through `kubectl`. If you want a production-grade, controller-native solution, use broom. This repo documents and refines my own take on the idea.

---

## Features

- **Automatic OOM detection** — reacts to GKE `OOMKilling` events delivered via Cloud Logging → Pub/Sub.
- **Ownership tracing** — resolves the failed Pod → Job → CronJob chain so the *source* CronJob spec is corrected, not just the transient job.
- **Memory limit bump** — multiplies the container memory limit (currently ×2) across `Ki` / `Mi` / `Gi` units.
- **Idempotency guard** — only applies a change when the live CronJob limit still matches the limit the OOM pod ran with, avoiding repeated doubling while a previous change propagates.
- **Immediate re-run** — creates a one-off Job from the updated CronJob (`kubectl create job --from=cronjob/...`) so work resumes without waiting for the next schedule.

> **Not yet implemented:** Slack notifications (the `Option.WebhookUrl` / channel fields are wired through but unused) and a configurable bump factor (the ×2 multiplier is currently hard-coded).

---

## How it works

```
GKE OOMKilling event
        │  (Cloud Logging sink)
        ▼
   Pub/Sub topic ── subscription: gcf-k8s-events-alert-export-k8s-events
        │
        ▼
   api/  (subscriber)  ── filters messages where jsonPayload.reason == "OOMKilling"
        │
        ▼
   RecoverOOM.Run(projectID, region, clusterName)
        │
        ├─ list GKE clusters, select the target cluster
        ├─ list Failed pods, keep those with containerStatus reason == "OOMKilled"
        ├─ trace Pod → Job → CronJob via owner references
        ├─ record each pod's max container memory limit
        │
        ▼  for each affected CronJob:
        ├─ kubectl get cronjob -o yaml         (dump current spec)
        ├─ double `memory:` limits in the YAML (regex over Ki/Mi/Gi)
        ├─ guard: live limit == OOM pod's limit ?
        │        ├─ yes → kubectl apply -f  +  kubectl create job --from=cronjob/...
        │        └─ no  → skip (a previous change is still settling)
        └─ done
```

Two entry points share the same core (`batchnet.go`):

| Path | Purpose |
|------|---------|
| `api/main.go`  | Long-running Pub/Sub subscriber. Triggered automatically by OOM events. The deployed component. |
| `cmd/main.go`  | One-shot CLI for manual runs / debugging: `go run ./cmd <projectID> <region> <clusterName>`. |

---

## Repository layout

```
.
├── batchnet.go          # Core logic: detect OOM pods, trace CronJob, bump memory, re-run
├── gcp_link.go          # Builds Cloud Console deep links for an event (for future notifications)
├── api/                 # Pub/Sub subscriber entry point (deployed service)
├── cmd/                 # CLI entry point for manual invocation
├── k8s/                 # Kustomize manifests (base + dev overlay): Deployment, SA, RBAC, namespace
├── oom_test.yaml        # Sample CronJob that intentionally OOMs, for end-to-end testing
├── Dockerfile           # Builds the api image (bundles kubectl)
└── skaffold.yaml        # Build/deploy pipeline
```

---

## Prerequisites

- A **GKE** cluster (the service authenticates to GKE via the in-cluster GCP auth provider).
- A **Cloud Logging sink → Pub/Sub** export of Kubernetes events, with a subscription named `gcf-k8s-events-alert-export-k8s-events`.
- A **GCP service account** bound to the workload (Workload Identity) with permission to list GKE clusters and read/modify CronJobs.
- Kubernetes RBAC granting the service account `edit` on the target namespaces (see `k8s/base/clusterrolebinding.yaml`).
- `kubectl` available in the runtime image (the `Dockerfile` installs it).

---

## Configuration

The subscriber reads the following environment variables:

| Variable     | Required | Description |
|--------------|----------|-------------|
| `PROJECT_ID` | yes      | GCP project the subscriber and target clusters live in. |
| `ENV`        | yes      | Deployment environment, injected into the namespace / image path via Kustomize. |

The target region and cluster name are taken from each incoming event's `resource.labels` (`location`, `cluster_name`).

---

## Getting started

### Build & test locally

```bash
go build ./...

# manual one-shot run against a cluster
go run ./cmd <projectID> <region> <clusterName>
```

### Try the OOM scenario

`oom_test.yaml` defines a CronJob that deliberately allocates beyond its 25Mi limit:

```bash
kubectl apply -f oom_test.yaml
# wait for the pod to be OOMKilled, then run RecoverOOM and confirm
# the CronJob's memory limit was doubled and a fresh job was created.
```

### Deploy

```bash
# via Skaffold (build image + apply kustomize dev overlay)
skaffold run

# or apply manifests directly
kubectl apply -k k8s/dev
```

---

## Limitations & roadmap

- Memory bump factor is fixed at ×2; make it configurable (broom supports both add and multiply).
- Slack notification path is stubbed but not implemented.
- GKE/GCP-specific (Pub/Sub trigger, GCP auth provider); not portable to other clusters as-is.
- Changes are driven through `kubectl` shell-outs rather than the Go client for the apply step.

---

## License

No license file is currently included. Add one before distributing.
