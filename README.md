# canopy

A progressive-delivery (canary) controller for plain VM deployments — the
part of tools like Argo Rollouts or Flagger that shifts traffic and analyzes
metrics, built from scratch for targets that aren't Kubernetes: SSH,
systemd, and nginx.

`canopy` deploys a new version to a canary host, shifts a small percentage
of live traffic to it via nginx, watches real metrics (error rate, p95
latency) against the stable baseline, and automatically progresses,
promotes, or rolls back — all without a Kubernetes control plane.

## Why this exists

Most progressive-delivery tooling assumes Kubernetes. This project
demonstrates the same rollout mechanics — traffic shifting, canary
analysis, automated rollback — against the infrastructure primitives that
came before it and are still common: virtual machines, SSH, systemd units,
and a hand-configured load balancer. It's a companion piece to
[SLO Guardian](https://github.com/Shihasz/slo-guardian) (a Kubernetes operator), deliberately built on a
different stack to show the underlying concepts translate.

## How it works

    canopy deploy --version v2.0.0 --prior-version v1.9.0

1. Deploys `v2.0.0` to the canary host via SSH + systemd (renders a unit
   file, installs it, restarts the service).
2. Shifts nginx traffic weights: 10% → 25% → 50% → 100%.
3. After each shift, waits a warmup period, then queries Prometheus for
   canary vs. stable error rate and p95 latency.
4. **Pass** → advance to the next traffic step (or promote, at 100%).
   **Fail** → immediately redeploy the prior version and reset traffic to
   0% canary. **Inconclusive** (not enough data yet) → retry a few times,
   then fail safe (treat as Fail) rather than risk promoting on
   insufficient evidence.

## Architecture

| Package                  | Responsibility                                          |
|---------------------------|----------------------------------------------------------|
| `internal/rollout`        | Rollout state machine and domain types                   |
| `internal/transport`      | SSH command execution                                     |
| `internal/deploy`         | systemd unit rendering, deploy, rollback                  |
| `internal/loadbalancer`   | nginx upstream config templating and reload               |
| `internal/analysis`       | Canary vs. stable metric comparison, Prometheus provider   |
| `internal/orchestrator`   | Wires the above into one end-to-end rollout                |
| `internal/config`         | YAML config loading and validation                         |
| `cmd/canopy`              | CLI (cobra): `deploy`, `status`, `rollback`, `promote`      |

## Quickstart (local lab)

Requires Docker Desktop (with WSL2 integration, if on Windows) and Go 1.26+.

    git clone https://github.com/Shihasz/canopy.git
    cd canopy

    make lab-keys   # generate SSH keys for the lab (gitignored, never committed)
    make lab-up     # build and start canary/stable VMs, nginx, prometheus
    make build      # build the canopy binary

    ./bin/canopy --config test/e2e/canopy.e2e.yaml deploy \
      --version v2.0.0 --prior-version v1.9.0

Watch it progress through traffic steps and either promote or roll back
based on real metrics. Try `--version v2.0.0-bad` to see the automatic
rollback path (this version has a 50% injected error rate).

> **Note on host keys:** rebuilding lab images (`make lab-up --build`)
> regenerates SSH host keys. If SSH warns about a changed host key,
> that's expected — run `ssh-keygen -R '[localhost]:<port>'` as it
> suggests, then reconnect.

### Running the test suites

    make test               # unit tests (fast, no infrastructure needed)
    make test-integration   # integration tests against the lab (needs `make lab-up` first)
    bash test/e2e/run.sh    # full scripted demo: promote + rollback, via the real CLI

## What canopy does *not* do

By design, `canopy` doesn't build or distribute your application — it
expects release artifacts (`/opt/<service>/releases/<version>/`) to
already exist on the target hosts, the same way a real deployment
pipeline separates "build and ship the artifact" (CI, `rsync`, image
baking) from "orchestrate the rollout" (this tool). The local lab and
Terraform module both pre-bake a few sample versions to stand in for that
separate step.

## Real infrastructure: GCP Terraform module

`infra/gcp/` provisions two real Compute Engine VMs (canary + stable),
firewall rules, and a network — usable standalone, not just for this
project's tests.

    cd infra/gcp
    cp terraform.tfvars.example terraform.tfvars
    # edit terraform.tfvars: your GCP project ID, and restrict ssh_source_ranges
    # to your own IP rather than leaving it open

    terraform init
    terraform plan   # review what will be created
    terraform apply  # provisions real, billable resources

**This is not run automatically anywhere** — CI only checks `terraform
fmt` and `terraform validate` (both fully offline; the `google` provider
requires real credentials even to compute a plan, so `plan`/`apply` are
deliberately left as manual, credentialed, local actions). Remember to
`terraform destroy` when you're done to avoid ongoing charges.

## CI

GitHub Actions runs on every push: lint (`golangci-lint`) and `gofmt`
check → build → unit tests → integration tests against a real
docker-compose lab → Terraform format/validate. See
[`.github/workflows/ci.yml`](.github/workflows/ci.yml).

## Cleanup

To tear down everything created locally:

    make lab-down                    # stop and remove lab containers
    docker image prune -f            # remove dangling images from rebuilds
    docker volume prune -f           # remove unused volumes
    rm -f lab/keys/deploy_key*       # remove generated lab SSH keys

If you ran the Terraform module against real GCP resources:

    cd infra/gcp
    terraform destroy

## License

MIT — see [LICENSE](LICENSE).
