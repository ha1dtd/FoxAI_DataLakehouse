# FoxAI Installer Test Matrix

## Purpose

This matrix defines the minimum scenarios the FoxAI installer must support before new UX or feature work is considered done.

Each scenario should answer:

- starting condition
- operator input
- expected detection
- expected prompt/decision
- expected end state

## Scenario 1: Fresh NameNode + Fresh DataNodes

- Start:
  - new NameNode
  - all DataNodes fresh
- Expected:
  - installer completes end to end
  - no reuse conflict prompt
  - configs written across all nodes
  - final binary/platform state is usable

## Scenario 2: Fresh NameNode + One Reused Compatible DataNode + One Fresh DataNode

- Start:
  - new NameNode
  - one old DataNode from a prior matching cluster state
  - one fresh DataNode
- Expected:
  - installer classifies reused node correctly
  - safe reuse path is explicit
  - installer completes without corrupting the reused node

## Scenario 3: Fresh NameNode + One Conflicting DataNode

- Start:
  - new NameNode
  - one DataNode with conflicting old state
- Expected:
  - installer reports conflict clearly
  - installer offers explicit choices:
    - stop
    - wipe and reuse
    - skip
    - review case by case if applicable

## Scenario 4: Existing Healthy Cluster Rerun

- Start:
  - FoxAI cluster already working
- Expected:
  - installer does not blindly rewrite everything
  - user sees what is already healthy
  - mode behavior matches the chosen mode:
    - `preflight` reports only
    - `repair` or `reconcile` asks before changing drift

## Scenario 5: Partially Installed DataNode

- Start:
  - DataNode has incomplete FoxAI-managed state
  - examples:
    - missing env block
    - partial Hadoop/Spark sync
    - HDFS storage exists but config is incomplete
- Expected:
  - node is classified as `partial`
  - installer reports exact differences
  - installer asks which items to fix before mutating

## Scenario 6: Bulk DataNode Input By Comma-Separated List

- Start:
  - operator has many DataNode IPs ready
- Input:
  - comma-separated list
- Expected:
  - installer parses correctly
  - duplicate or invalid IPs are rejected cleanly
  - final active node list is shown back to the operator

## Scenario 7: Bulk DataNode Input By IP Range

- Start:
  - operator knows a sequential range such as `x.x.x.1-10`
- Input:
  - range form
- Expected:
  - installer expands correctly
  - invalid ranges are rejected clearly
  - final active node list is shown back to the operator

## Scenario 8: DataNode Username Same As NameNode User

- Start:
  - all nodes use the same Linux username
- Expected:
  - installer offers same-user default
  - operator can accept with Enter

## Scenario 9: Custom DataNode Username

- Start:
  - DataNodes use a different Linux username
- Expected:
  - installer allows override
  - SSH/sudo/bootstrap logic uses the chosen value consistently

## Scenario 10: One DataNode Fails During Sync Or Remote Setup

- Start:
  - one target DataNode fails during rsync or remote setup
- Expected:
  - installer reports exactly which node failed
  - failure is visible in final summary
  - rerun path is safe and understandable

## Scenario 11: User Skips Some DataNodes Mid-Run

- Start:
  - one or more target nodes are skipped due to conflict or user choice
- Expected:
  - active node set is reduced correctly
  - local hosts/workers/Hadoop config is rewritten to the final active set
  - skipped nodes are excluded from sync and remote setup

## Scenario 12: Read-Only Validation Of Refactored Build

- Start:
  - new folder-based source tree is the active build path
- Run:
  - `--dry-run`
  - `--preflight`
- Expected:
  - new source tree behavior matches the legacy reference flow
  - no source-layout regression is introduced by the refactor

## Scenario 13: Unsupported OS Family Detection

- Start:
  - installer runs on a Linux distro outside the current official baseline
- Expected:
  - installer detects the OS family before attempting package/bootstrap work
  - installer exits clearly with the detected distro/family
  - no partial install mutation happens after that unsupported-environment decision

## Scenario 14: Unsupported CPU Architecture Detection

- Start:
  - installer runs on unsupported CPU architecture such as `arm64` while only `x86_64` is officially supported
- Expected:
  - installer detects the architecture early
  - installer exits clearly with the detected architecture
  - no partial install mutation happens after that unsupported-environment decision

## Scenario 15: Custom Base Path Override

- Start:
  - operator wants to install FoxAI-managed paths outside the default base layout
- Input:
  - custom base path values
- Expected:
  - installer accepts the overrides
  - local and remote config generation uses those paths consistently
  - rerun/probe logic still compares against the chosen custom paths, not the defaults

## Scenario 16: Custom Artifact Source / Internal Mirror

- Start:
  - customer cannot use the public default artifact source directly
- Input:
  - custom internal URL or mirror values for platform downloads
- Expected:
  - installer uses the provided source instead of the default public source
  - failure messages clearly identify artifact-source problems
  - baseline online path still works when overrides are not provided

## Scenario 17: Skip Some DataNodes Mid-Run

- Start:
  - one or more target nodes are skipped due to conflict or user choice
- Expected:
  - final active node set is reduced correctly
  - local hosts/workers/Hadoop config is rewritten to the final active set
  - skipped nodes are excluded from sync and remote setup
  - kept nodes still complete normally

## Scenario 18: Same-Cluster Rerun With Existing Nodes Entered Under `NEW`

- Start:
  - FoxAI cluster already working
  - operator accidentally enters already-existing same-cluster DataNodes under `NEW`
- Expected:
  - installer does not blindly mutate already-converged compatible nodes
  - final cluster shape remains correct
  - remote sync/setup no-ops for compatible nodes
  - summary makes it clear which nodes were already converged versus actually mutated

## Scenario 19: Fresh GCloud SSH Readiness Race

- Start:
  - freshly created cloud VMs where one early SSH check passes but a later remote step still fails because key/auth readiness is not stable yet
- Expected:
  - installer re-shows the SSH bootstrap/manual-key recovery path
  - operator can recover and continue the same run
  - final cluster path still completes without forcing a full restart as the only option

## Confirmation Checklist For Expansion

Before implementing expansion scenarios 13-16, confirm:

1. official first support statement stays `Ubuntu/Debian x86_64 online`
2. whether RHEL-family support is needed now or later
3. whether ARM support is needed now or later
4. which base paths must be configurable in the next pass
5. whether custom artifact source support should cover:
   - internal URLs/mirrors only
   - or pre-staged local artifacts too
6. whether full offline / air-gapped support is in scope now or deferred

## Completion Rule

A new installer feature is not complete until:

- the relevant scenario is added to this matrix
- the implementation matches the contract in `installer_contract.md`
- the scenario result is verified and recorded in project memory if it changes the working baseline
