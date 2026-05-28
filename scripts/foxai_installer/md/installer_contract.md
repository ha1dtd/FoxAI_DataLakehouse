# FoxAI Installer Contract

## Purpose

The FoxAI installer exists to turn customer Linux servers into a working FoxAI platform cluster so the remaining customer work is:

- provide their own credentials and environment-specific values
- write their own DAGs and task/job scripts

The installer is not responsible for shipping customer DAGs, customer scripts, or customer business logic.

## Supported Target

- OS: Linux
- CPU architecture: x86_64 / amd64
- Delivery artifact: one binary named `installer`
- Canonical build:

```bash
cd scripts/foxai_installer
GOOS=linux GOARCH=amd64 go build -o ../installers/installer .
```

## Source Of Truth

- Active module source: `scripts/foxai_installer/`
- Legacy reference only: `scripts/foxai_installer.go`
- Output binary: `scripts/installers/installer`

## Installer Goal

On success, the installer should leave the cluster in a state where:

- NameNode and DataNodes are configured for the FoxAI platform
- Hadoop and Spark are installed and wired together correctly
- cluster config is present and consistent across nodes
- SSH and required sudo behavior are ready for installer-managed operations
- the customer can proceed with their own scripts and credentials

## Modes

- `install`
  - create or converge a FoxAI cluster shape
  - may mutate local and remote hosts after confirmation where required
- `preflight`
  - read-only inspection
- `repair`
  - patch an existing cluster after reporting detected drift
- `reconcile`
  - align an existing cluster to the requested FoxAI shape
- `dry-run`
  - show intended flow only
- `recommend-only`
  - skip install actions and print hardware/Spark recommendations only

## Required Inputs

- NameNode private IP
- DataNode targets
- DataNode username
- MinIO endpoint
- MinIO access key
- MinIO secret key
- Kakao mirror choice

## Required Input Behavior

The installer must support these operator input patterns:

- single IP entry
- comma-separated IP list
- IP range input for sequential hosts

The installer should prefer safe defaults where the meaning is obvious:

- default DataNode username should be the same as the current/NameNode user unless overridden
- default MinIO endpoint should derive from the entered NameNode IP

The installer should also prefer safe local defaults where the host can tell us the answer:

- default NameNode IP should be the local private IPv4 detected on the NameNode host unless overridden

## Node State Model

Every target DataNode must be classified into one of these states before destructive action:

- `fresh`
  - no prior FoxAI/HDFS DataNode state detected
- `compatible`
  - prior state matches the current intended cluster and is safe to reuse
- `conflicting`
  - prior state exists but conflicts with the intended cluster
- `partial`
  - host contains incomplete or broken FoxAI-managed state
- `unreadable`
  - installer cannot determine enough state safely

## Safety Rules

- Never apply repairs blindly on partially installed or drifted hosts.
- If the installer detects drift, partial state, or conflicting state, it must:
  - report what it found
  - show the target node(s)
  - ask the user what to do before continuing
- The installer may automate normal fresh-path setup steps, but not ambiguous recovery decisions.
- The installer must preserve one final summary of what was checked, changed, skipped, or blocked.

## Ownership Rules

FoxAI owns the platform configuration it needs to make Hadoop, Spark, and cluster wiring work.

Practical ownership model:

- full ownership:
  - FoxAI-managed Hadoop config under the installer-managed Hadoop home
- managed-block ownership:
  - `/etc/hosts`
  - user `.bashrc`
- conditional ownership:
  - `/opt/spark` when the installer is the component provisioning Spark there
- never claim ownership over:
  - customer DAGs
  - customer job scripts
  - customer secrets beyond explicit installer-provided config inputs

## Success Criteria

The installer can report success only when the intended action for the chosen mode is complete and verification is sufficient for that mode.

Minimum success expectations for mutation modes:

- required binaries/config paths are present
- SSH to target DataNodes works for the installer path
- required sudo path works where the installer depends on it
- NameNode/DataNode config sync is complete for the active node set
- HDFS/YARN verification path is available and can be checked or explicitly reported if skipped by user choice

## Current Baseline Support

As of 2026-05-26, the installer baseline is:

- Linux
- x86_64 / amd64
- online install path
- apt-based distro
- FoxAI-managed cluster paths
- fresh servers or servers where FoxAI is taking ownership of its own platform paths

This baseline is the current support claim unless explicitly expanded later.

## 2026-05-26 Expansion Plan

The next expansion work is not “support any Linux server.” It is a staged compatibility plan.

### Expansion Order

1. make the current support boundary explicit in code
2. detect unsupported environments early and fail clearly
3. parameterize install paths and artifact sources
4. broaden distro support only after the baseline remains stable

### Immediate Expansion Scope

These items are in the next serious expansion scope:

1. OS family detection
   - detect Debian/Ubuntu vs unsupported families
   - fail clearly when outside the supported family
2. CPU architecture detection
   - detect `x86_64` / `amd64` vs unsupported architectures
   - fail clearly when unsupported
3. configurable base paths
   - keep FoxAI defaults
   - allow operator override for install base paths
4. configurable artifact sources
   - keep public defaults
   - allow internal mirror or custom URL override for Hadoop/Spark and related downloads

### Deferred Expansion Scope

These are explicitly not required in the immediate next pass unless the user changes scope:

- full offline bundle packaging
- ARM support
- RHEL-family package-manager support
- support for arbitrary pre-existing customer Hadoop/Spark clusters

## Required User Confirmations For Expansion

Before implementing the next expansion pass, the user must confirm these scope decisions:

1. official first support statement
   - keep `Ubuntu/Debian x86_64 online` as the explicit baseline
2. OS-family expansion
   - whether RHEL-family support is needed now or later
3. CPU expansion
   - whether ARM is needed now or later
4. path configurability
   - which install paths should become operator-configurable in the next pass
5. artifact-source configurability
   - whether the next pass should support:
     - custom internal URLs
     - mirror/proxy use
     - pre-staged local artifacts
6. offline scope
   - whether “offline” means:
     - mirror/proxy-friendly
     - or fully air-gapped bundle delivery

## Hard Rules For Expansion

When implementing the next compatibility pass:

- do not weaken the current working Debian/Ubuntu path
- do not claim support for a distro family until package install, Java bootstrap, and artifact install are all validated there
- do not claim full offline support until package and artifact acquisition are both handled without public internet
- do not silently continue on unsupported OS/arch; fail clearly with the detected value
- keep one delivery binary unless the user explicitly approves multi-artifact release strategy

## Near-Term Runtime Gap

One current runtime scenario remains explicitly unvalidated in the baseline path:

- skipping some DataNodes mid-run during the reused-node decision flow

That scenario should be validated before the current installer pass is considered fully closed.
