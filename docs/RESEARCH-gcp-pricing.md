# Research: GCP/GKE pricing & constraints (us-central1, retrieved 2026-09-25)

Live-scraped from Google pricing pages by a research subagent on 2026-09-25.
Monthly figures use Google's 730 hr/mo convention. Spot prices are dynamic
(point-in-time). These are the calculator's default price book
(`tool/index.html` → `PRICEBOOK`).

## Nested virtualization (microVM / Cloud Hypervisor path)

- **Supported series:** N1, N2, N4, **N4D (only AMD exception)**, C2, C3, C4,
  C4N, A2, G2, H3, M4, Z3.
  **Not supported:** E2, N2D, C3D, C4D, T2D, all Arm (T2A/C4A/N4A), C2D, H4D.
  Source: machine series comparison table,
  https://docs.cloud.google.com/compute/docs/machine-resource
- Enable per node pool at creation: `--enable-nested-virtualization`
  (`advancedMachineFeatures.enableNestedVirtualization`). **GKE Standard only —
  Autopilot does not support it.** Node image UBUNTU_CONTAINERD, or
  COS_CONTAINERD on GKE ≥ 1.28.4-gke.1083000. Pods touching /dev/kvm need
  privileged (Substrate's atelet device plugin handles exposure as
  `ate.dev/kvm`). https://docs.cloud.google.com/kubernetes-engine/docs/how-to/nested-virtualization
- **Performance caveat:** ≥10% CPU penalty for nested workloads (Google's own
  guidance), more for I/O-bound.

## GKE Sandbox note (mostly irrelevant to Substrate)

Substrate does **not** use GKE Sandbox / RuntimeClass — it runs `runsc`
unprivileged inside ordinary pods. So gVisor-class workers run on **any**
machine family, any image with containerd, and even Autopilot is conceivable.
(GKE Sandbox facts, for completeness: cos_containerd required, E2 supported on
modern GKE, Intel SMT disabled by default on Sandbox nodes.)

## VM pricing (us-central1, on-demand / spot, $/hr)

| Family | $/vCPU OD | $/GiB OD | $/vCPU Spot | $/GiB Spot | Spot disc. | Nested virt | SUD |
|---|---|---|---|---|---|---|---|
| E2 | 0.021811 | 0.002923 | 0.013090 | 0.001754 | ~40% | no | no |
| N1 | 0.031611 | 0.004237 | 0.018960 | 0.002541 | ~40% | yes | ≤30% |
| N2 | 0.031611 | 0.004237 | 0.018960 | 0.002542 | ~40% | yes | ≤20% |
| N2D | 0.027502 | 0.003686 | 0.013410 | 0.001795 | ~51% | no | ≤20% |
| N4 | 0.031190 | 0.003540 | 0.017650 | 0.002004 | ~43% | yes | no |
| C3 | 0.034650 | 0.003938 | 0.013050 | 0.001483 | ~62% | yes | no |
| C3D | 0.029563 | 0.003959 | 0.007210 | 0.000966 | ~76% | no | no |
| C4 | 0.034650 | 0.003938 | 0.020740 | 0.002357 | ~40% | yes | no |
| C4D | 0.032704 | 0.003753 | 0.013900 | 0.001597 | ~58% | no | no |
| T2D | 0.027502 | 0.003686 | 0.016501 | 0.002212 | ~40% | no | no |

Example shapes (OD $/hr): e2-standard-16 **0.536**, n2-standard-16 **0.777**,
n4-standard-16 **0.726**, c3-standard-22 **1.109**, c4-standard-16 **0.791**,
c3d-standard-16 0.726 (spot 0.177!), t2d-standard-16 0.676.

**Committed use discounts:** resource-based **37% (1yr) / 55% (3yr)** — all
families above; flexible CUDs 28% / 46%. Sustained-use discounts only on
N1 (≤30%), N2/N2D/C2 (≤20%).

Sources: https://cloud.google.com/products/compute/pricing/general-purpose,
https://cloud.google.com/spot-vms/pricing,
https://docs.cloud.google.com/compute/docs/sustained-use-discounts

## GKE fees

- Cluster management: **$0.10/cluster/hr** ($74.40/mo free-tier credit for one
  zonal/Autopilot cluster). Extended support +$0.50/hr.
- Autopilot pod pricing (if ever relevant, gVisor only): $0.0445/vCPU-hr +
  $0.0049225/GiB-hr; spot $0.0133/$0.0014767 (~70% off); GKE CUDs 20%/45%.
- GKE Enterprise (optional): $0.00822/vCPU-hr.
  https://cloud.google.com/kubernetes-engine/pricing

## Storage (us-central1, $/GiB-month)

| Store | $/GiB-mo | Notes |
|---|---|---|
| **GCS Standard** | **0.020** | Substrate snapshot home; ops: Class A $5.00/M, Class B $0.40/M; same-region GCS↔VM transfer free |
| GCS Nearline | 0.010 | +$0.01/GiB retrieval — interesting for long-idle agents |
| GCS Coldline | 0.004 | +$0.02/GiB retrieval, 90-day min |
| pd-standard | 0.040 | |
| pd-balanced | 0.100 | node boot disks |
| pd-ssd | 0.170 | |
| Hyperdisk Balanced | 0.080 | + provisioned IOPS/throughput |
| Local SSD | 0.080 | |
| PD snapshot (std) | 0.050 | |

https://cloud.google.com/compute/disks-image-pricing, https://cloud.google.com/storage/pricing

## Network

- Same-zone internal: free. Cross-zone same-region: $0.01/GiB (multi-zone
  clusters: snapshot traffic stays VM↔GCS so mostly N/A).
- VM → GCS same region: **free** — snapshot upload/download costs only ops.
  https://cloud.google.com/vpc/network-pricing

## Modeling takeaways

1. **microVM scenario:** GKE Standard + nested-virt Intel family. Best OD value
   ≈ N4 ($0.03119/vCPU); best spot value ≈ C3 ($0.01305/vCPU, ~62% off).
   Apply ~10% nested-virt CPU penalty to effective capacity.
2. **gVisor scenario:** any family. Cheapest OD = E2 ($0.021811/vCPU);
   cheapest spot = C3D ($0.00721/vCPU).
3. **Snapshots:** GCS Standard $0.02/GiB-mo → a 1 GiB (zstd ~≤0.5 GiB) snapshot
   costs ~$0.01–0.02/mo at rest. Per-cycle ops (5–9 Class A writes/suspend,
   3–6 reads/resume) ≈ $0.00004/cycle — ~$0.06/mo at 50 cycles/day. Negligible
   but nonzero; the tool includes both.
4. CUD 3yr (55%) beats spot (40%) on N2/C4 but loses to spot on C3/C3D.
