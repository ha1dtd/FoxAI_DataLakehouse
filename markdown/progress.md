# progress.md

## How To Use This File (Agent Instructions)

- Read this file at session start, after any compact, and before resuming any task.
- Never rely on chat memory alone — always read current on-disk file state before editing.
- After every file edit, deploy, validation milestone, or root-cause investigation milestone, immediately update the relevant task block below.
- When a task is fully complete and deployed, move it to the Completed Archive section.
- Never delete a task — archive it. Never summarize away exact file states.
- Record exact remote artifact paths, snapshot ids, replay ids, and blocker/root-cause signals needed to resume without chat memory.
- `Current Phase` and `Next Exact Step` must be specific enough that a new session can continue the task immediately.
- If adding a new task, copy the Task Template at the bottom.

---

## Active Tasks

---
---

### Task 2 — Combined-Domain Safe-Hardening

**Goal:** Complete the agreed safe-fix sequence across combined-domain pipeline files.

**Mode:** Hotfix (each fix one file at a time)

**Current Phase:** On hold by user as of 2026-05-20. Fixes 1-3 are complete and deployed. Fixes 4-5 remain partial and are not the current priority.

**Next Exact Step:**
None while on hold. If resumed later, continue with Fix 4 (explicit failure-context logging before re-raise) and Fix 5 (lightweight observability logs) in remaining files beyond `bronze_from_raw_domains.py`, then deploy.

**Fix Sequence & Status**

- Fix 1: Centralize config/env — complete + deployed
- Fix 2: Replace `print()` with structured logging — complete + deployed
- Fix 3: Finish shared-config cleanup — complete + deployed
- Fix 4: Add explicit failure-context logging before re-raise — partial, done in `bronze_from_raw_domains.py` only
- Fix 5: Add lightweight observability logs — partial, done in `bronze_from_raw_domains.py` only

**Files Remaining for Fixes 4-5**

- All files in `dag_combined_domains/` except `bronze_from_raw_domains.py`
- Agent must read each file before editing — do not assume current state

**Risks**

- Fix 4-5 are partially applied — all other files are still in original state
- Do not assume any file has been updated unless explicitly listed as complete above
- This task was deprioritized after packaging scope was narrowed to platform install/config/setup only.

---

### Task 4 — Platform Packaging Baseline + Customer Extension Path

**Goal:** Productize the platform bootstrap from `setup_namenode_v5.sh` and `setup_datanode.sh` first, then define the customer script/template/extension path on top of that installed platform, with licensing handled afterward as a separate later phase.

**Mode:** Refactor

**Current Phase:** Live Linux validation passed on 2026-05-26 for the main installer hardening scope. The contract-driven feature set is working in the tested scenarios:
- fresh cluster install: passed end to end
- fresh NameNode + fresh DataNode2 + old DataNode1 from a previous cluster: passed after the conflicting-node reinitialize fix
- same-cluster rerun behavior: validated as good after the rerun no-op hardening and `.bashrc` compare hotfix
- bulk DataNode input: validated by user as good
- NameNode IP prompt now defaults to the local private IPv4 detected on the NameNode host when the operator presses Enter
- default DataNode username behavior: validated by user as good
- partial / reused / conflicting classification flow: validated by user as good
- conflicting reused-node action now works with the stronger `reinitialize old DataNode state` path
- NameNode-led overwrite/resync flow remains intact after the classification improvements
- install mode now distinguishes:
  - final cluster shape
  - DataNodes that actually require mutation in this run
- same-cluster reruns can now no-op remote sync/setup for already-converged compatible DataNodes, even if the operator accidentally enters them under `NEW`
- later-step SSH recovery path is working for fresh GCloud machines where passwordless SSH readiness is inconsistent during early bootstrap
- remaining unvalidated scenario from this pass:
  - skipping some nodes mid-run during the reused-node decision flow

Contract/planning update on 2026-05-26:
- updated `scripts/foxai_installer/md/installer_contract.md`
  - replaced the old short hardening-priority tail with the 2026-05-26 expansion contract
  - now defines:
    - current baseline support claim
    - staged expansion order
    - immediate next expansion scope
    - deferred scope
    - required user confirmations before broader compatibility work
- updated `scripts/foxai_installer/md/installer_test_matrix.md`
  - added the matching expansion scenarios for:
    - unsupported OS family detection
    - unsupported CPU architecture detection
    - custom base path override
    - custom artifact source / internal mirror
    - same-cluster rerun with nodes mistakenly entered under `NEW`
    - fresh GCloud SSH readiness race
  - also added a confirmation checklist for expansion work

Known environment note from live testing:
- on freshly created GCloud VMs, the first SSH/bootstrap step can report success and later remote execution can still fail on a new node until key/auth readiness stabilizes
- current installer behavior is acceptable:
  - it re-shows the manual SSH bootstrap path
  - after the operator pastes the key, the run continues normally
- treat this as a GCloud / OS Login / guest-agent timing issue unless later evidence shows otherwise

**Next Exact Step:** For the current installer pass, only one meaningful runtime scenario remains unvalidated:
1. test skipping some nodes mid-run in the reused-node decision flow
   - confirm skipped nodes are removed from the final active cluster shape for that run
   - confirm local hosts/workers/config rewrites match the kept nodes only
   - confirm kept nodes still complete normally
2. if that scenario passes, this installer hardening pass can be treated as complete for the current packaging scope
3. after that, before starting broader compatibility work, collect user confirmations for the 2026-05-26 expansion contract:
   - keep official baseline as `Ubuntu/Debian x86_64 online` or change it
   - whether RHEL-family support is needed now or later
   - whether ARM support is needed now or later
   - which install paths must become configurable in the next pass
   - whether artifact-source support means:
     - internal URLs / mirrors only
     - or also pre-staged local artifacts
   - whether full offline / air-gapped support is in scope now or deferred
4. only after those confirmations:
   - start the next compatibility pass
   - or move to customer extension handoff if expansion is deferred

**Files In Scope**

- `scripts/setup_namenode_v5.sh`
  - status: present | verified: local
  - substantial Namenode bootstrap script (~427 lines)
  - installs/configures cluster prerequisites and platform runtime pieces

- `scripts/setup_datanode.sh`
  - status: present | verified: local
  - substantial DataNode bootstrap script (~176 lines)
  - prepares node join/bootstrap path for cluster deployment

- `docs/KeHoachTrienKhai.xlsx`
  - status: updated | verified: local
  - planning sheet now reflects generic packaging/add-on/licensing wording and current statuses

- `scripts/foxai_installer.sh`
  - status: in progress | verified: local
  - new single-file terminal installer entrypoint
  - combines the current tested NameNode flow and automatic remote DataNode execution into one executable shell installer

- `scripts/foxai_installer.go`
  - status: active single source of truth | verified: local compile
  - first native Linux Go installer source for the packaging track
  - preserves the one-file interactive installer shape
  - includes:
    - NameNode + remote DataNode orchestration
    - pinned Java/Hadoop/Spark constants
    - dynamic worker/hosts/replication generation from entered DataNode count
    - end-of-run hardware prompt with `auto probe` vs `manual entry`
    - printed Spark recommendations without mutating DAG/job configs
    - safe test modes:
      - `--dry-run` prints the planned NameNode/DataNode execution flow only
      - `--recommend-only` skips install steps and runs only the hardware/Spark recommendation flow
  - current hardening target:
    - behave like a production installer on an existing FoxAI cluster, not only like a translated bootstrap script
  - current on-disk change in this pass:
    - MinIO endpoint prompt now defaults from the entered NameNode IP as `<namenode-ip>:9001` instead of a fixed old on-prem IP
    - added first-step bootstrap dependency check/install before the normal mode flow
    - bootstrap step now checks for very base local tool dependencies and installs missing ones with `apt-get` before the existing installer logic runs
    - corrected the bootstrap privilege regression after GCloud testing:
      - local privileged commands now use `root if already root, else sudo if available`
      - `sudo` is not treated as a bootstrap dependency package
      - the first-step package install no longer tries plain `apt` as a normal OS Login user
    - corrected fresh-VM workers-file handling:
      - stock Hadoop `workers` content of `localhost` is now treated as a safe default baseline in install mode instead of drift
    - corrected fresh-VM sync ordering:
      - the installer now ensures `rsync` exists on each DataNode before the NameNode tries to `rsync` Hadoop and Spark there
    - corrected remote base-package ordering:
      - immediately after passwordless SSH and NOPASSWD sudo are ready, the NameNode now SSHes into every DataNode and installs the full DataNode base package set before continuing with local NameNode bootstrap and file sync
    - corrected install-mode rerun behavior:
      - if the NameNode was already formatted by a previous partial install, install mode now treats that as a resumable run and continues through the idempotent steps instead of hard-blocking
    - corrected remote sync permissions:
      - NameNode-to-DataNode `rsync` now runs the receiver side as `sudo rsync`, so syncing into privileged paths like `/opt/spark` no longer fails on a fresh DataNode
      - Hadoop sync remains user-owned under `/home/<user>/hadoop`, while Spark sync uses elevated receiver mode plus remote `chown` normalization for `/opt/spark`
    - corrected install-mode drift handling:
      - managed drift in install mode no longer hard-stops immediately
      - for drifted managed files/blocks, the installer now prompts with 4 options:
        - stop
        - replace with installer value
        - enter custom replacement content
        - skip this step
    - corrected remote shell execution UX:
      - non-interactive remote DataNode bootstrap/setup commands no longer force a TTY, so the remote script is not echoed back line-by-line into the terminal
    - implemented reused-DataNode lifecycle handling in `install`:
      - after NameNode format/verify, the installer reads the local NameNode `clusterID`
      - each target DataNode is probed for `~/hadoopdata/datanode/current/VERSION`
      - nodes are classified as:
        - fresh
        - compatible
        - conflicting
        - unreadable
      - conflicting/unreadable nodes now support bulk resolution:
        - stop
        - wipe all reused/unreadable DataNodes and reuse them
        - skip all reused/unreadable DataNodes for this run
        - review one by one
      - per-node review supports:
        - stop
        - wipe old HDFS DataNode storage and reuse
        - skip this node
        - keep old storage and force continue (unsafe)
      - wipe action removes only `~/hadoopdata/datanode` and preserves packages, Java, Hadoop/Spark files, SSH, `/etc/hosts`, and shell environment
      - if nodes are skipped, the active install target set is reduced before sync and remote setup, and local managed hosts/Hadoop config is rewritten to the final active node set in the same run
    - current bootstrap dependency set for the main installer:
      - `python3`
      - `openssh-client` for `ssh` and `ssh-copy-id`
      - `rsync`
      - `wget`
      - `tar`
    - added read-only `--preflight`
    - added guarded local config evaluation for `/etc/hosts`, Hadoop XML files, workers, and `hadoop-env.sh`
    - changed install behavior so exact managed state skips while drift raises explicit terminal errors
    - changed existing DataNode handling from default rsync reconciliation to read-only verification in install mode
    - softened `ssh-copy-id` handling by verifying passwordless SSH before failing
  - consolidation completed on 2026-05-26:
    - replaced previous `scripts/foxai_installer.go` contents with the exact then-current `scripts/installer.go` code
    - local verification passed:
      - `go build -o /tmp/foxai_installer_check scripts/foxai_installer.go`
    - `scripts/foxai_installer.go` is now the only unified installer source of truth for this path
  - current refactor note on 2026-05-26:
    - user approved a folder-based industry-standard split
    - this file must remain on disk unchanged as a legacy reference while the new source tree exists beside it
    - status after refactor:
      - preserved unchanged as requested

- `scripts/installer.go`
  - status: removed
  - currently the newest unified installer source on disk
  - folded back into `scripts/foxai_installer.go` and deleted on 2026-05-26

- `scripts/gcloud_installer.go`
  - status: in progress | verified: local compile
  - separate GCloud-oriented Go installer variant
  - current on-disk change in this pass:
    - MinIO endpoint prompt now defaults from the entered NameNode IP as `<namenode-ip>:9001` instead of a fixed old on-prem IP
    - added first-step bootstrap dependency check/install before the normal mode flow
    - bootstrap step now checks for very base local tool dependencies and installs missing ones with `apt-get` before the existing installer logic runs
    - corrected the bootstrap privilege regression after GCloud testing:
      - local privileged commands now use `root if already root, else sudo if available`
      - `sudo` is not treated as a bootstrap dependency package
      - the first-step package install no longer tries plain `apt` as a normal OS Login user
    - corrected the manual SSH bootstrap UX:
      - reruns now verify passwordless SSH first and skip the key-print/paste prompt entirely when SSH is already ready on all DataNodes
    - corrected fresh-VM workers-file handling:
      - stock Hadoop `workers` content of `localhost` is now treated as a safe default baseline in install mode instead of drift
    - corrected fresh-VM sync ordering:
      - the installer now ensures `rsync` exists on each DataNode before the NameNode tries to `rsync` Hadoop and Spark there
    - corrected remote base-package ordering:
      - immediately after passwordless SSH and NOPASSWD sudo are ready, the NameNode now SSHes into every DataNode and installs the full DataNode base package set before continuing with local NameNode bootstrap and file sync
    - corrected install-mode rerun behavior:
      - if the NameNode was already formatted by a previous partial install, install mode now treats that as a resumable run and continues through the idempotent steps instead of hard-blocking
    - corrected remote sync permissions:
      - NameNode-to-DataNode `rsync` now runs the receiver side as `sudo rsync`, so syncing into privileged paths like `/opt/spark` no longer fails on a fresh DataNode
      - Hadoop sync remains user-owned under `/home/<user>/hadoop`, while Spark sync uses elevated receiver mode plus remote `chown` normalization for `/opt/spark`
    - corrected install-mode drift handling:
      - managed drift in install mode no longer hard-stops immediately
      - for drifted managed files/blocks, the installer now prompts with 4 options:
        - stop
        - replace with installer value
        - enter custom replacement content
        - skip this step
    - corrected remote shell execution UX:
      - non-interactive remote DataNode bootstrap/setup commands no longer force a TTY, so the remote script is not echoed back line-by-line into the terminal
    - implemented reused-DataNode lifecycle handling in `install`:
      - after NameNode format/verify, the installer reads the local NameNode `clusterID`
      - each target DataNode is probed for `~/hadoopdata/datanode/current/VERSION`
      - nodes are classified as:
        - fresh
        - compatible
        - conflicting
        - unreadable
      - conflicting/unreadable nodes now support bulk resolution:
        - stop
        - wipe all reused/unreadable DataNodes and reuse them
        - skip all reused/unreadable DataNodes for this run
        - review one by one
      - per-node review supports:
        - stop
        - wipe old HDFS DataNode storage and reuse
        - skip this node
        - keep old storage and force continue (unsafe)
      - wipe action removes only `~/hadoopdata/datanode` and preserves packages, Java, Hadoop/Spark files, SSH, `/etc/hosts`, and shell environment
      - if nodes are skipped, the active install target set is reduced before sync and remote setup, and local managed hosts/Hadoop config is rewritten to the final active node set in the same run
    - current bootstrap dependency set for the GCloud installer:
      - `python3`
      - `openssh-client` for `ssh`
      - `rsync`
      - `wget`
      - `tar`
    - replaced `ssh-copy-id` bootstrap with a manual pause-and-verify flow
    - prints the NameNode public key for copy/paste into each DataNode `authorized_keys`
    - prints the required DataNode-side `~/.ssh` permission commands
    - waits for user confirmation, then verifies passwordless SSH to all target DataNodes before continuing
    - writes its manifest under `~/.foxai-gcloud-installer/last-run.json`

- `scripts/installers/installer`
  - status: built | verified: local artifact
  - Linux x86-64 binary compiled from the folder-based module at `scripts/foxai_installer/`
  - intended handoff artifact for the unified main installer path on Linux hosts
  - rebuilt on 2026-05-26 after the live-validation hotfix pass:
    - build command:
      - from workdir `scripts/foxai_installer/`:
        - `GOOS=linux GOARCH=amd64 go build -o ../installers/installer .`
    - artifact verification:
      - `ls -l`: `3996316` bytes
      - file type: `ELF 64-bit LSB executable, x86-64`
      - sha256: `b660aba699a085b728595ba8d0a4183dc3eaf615c903577a8991ca7281268e76`

- `scripts/installers/gcloud_installer`
  - status: removed by user
  - Linux x86-64 binary compiled from `scripts/gcloud_installer.go`
  - intended handoff artifact for testing on GCloud VMs
  - on-disk note on 2026-05-26:
    - user removed the previous artifact from `scripts/installers/`

- `scripts/foxai_installer_premise_notes.md`
  - status: updated | verified: local
  - small note documenting premise-specific logic and pinned versions inherited from the source scripts
  - now reflects Java 11 as the only managed Java runtime in the installer family

- `scripts/foxai_installer/`
  - status: created | verified: local compile
  - new industry-standard source tree for the unified installer
  - created on 2026-05-26 with the following layout:
    - `main.go`
    - `go.mod`
    - `internal/installer/constants.go`
    - `internal/installer/types.go`
    - `internal/installer/run.go`
    - `internal/installer/prompts.go`
    - `internal/installer/verify.go`
    - `internal/installer/ssh.go`
    - `internal/installer/bootstrap.go`
    - `internal/installer/install_namenode.go`
    - `internal/installer/install_datanode.go`
    - `internal/installer/reuse.go`
    - `internal/installer/config_files.go`
    - `internal/installer/recommend.go`
    - `internal/installer/exec.go`
    - `internal/installer/remote_script.go`
  - behavior contract status:
    - one output binary preserved:
      - `scripts/installers/installer`
    - legacy reference preserved:
      - `scripts/foxai_installer.go`
    - first-pass refactor goal met:
      - structural split completed without intentionally changing installer behavior
  - current doc addition planned on 2026-05-26:
    - add a minimal installer contract
    - add a scenario-based installer test matrix
    - keep both docs inside this folder so the source tree carries its own operating contract
  - documentation added on 2026-05-26:
    - `installer_contract.md`
      - captures supported target, one-binary build contract, installer goal, modes, required inputs, node state model, safety rules, ownership rules, success criteria, and the next hardening priorities
    - `installer_test_matrix.md`
      - captures the minimum scenario set the installer must support before new work is considered done
    - local verification:
      - both files were created and re-read from disk successfully
  - contract-driven hardening implemented on 2026-05-26:
    - added repo-local update script:
      - `tools/apply_contract_hardening_phase1.py`
    - `internal/installer/prompts.go`
      - added bulk IP entry support while preserving the current existing/new node split
      - current supported methods:
        - one by one
        - comma-separated list
        - combined IP/range expression
      - added same-as-current-user default for the DataNode username prompt
    - `internal/installer/constants.go`
      - added DataNode state:
        - `partial`
    - `internal/installer/reuse.go`
      - expanded install-time DataNode classification to:
        - `fresh`
        - `compatible`
        - `partial`
        - `conflicting`
        - `unreadable`
      - partial detection now calls out clearer incomplete-state signals such as:
        - datanode storage exists but `VERSION` is missing
        - Hadoop config under the managed Hadoop home is incomplete
        - Spark home exists but `spark-submit` is missing
        - managed env block is incomplete
        - managed runtime exists without datanode storage
      - core sync/install model intentionally preserved:
        - partial nodes now get clearer operator decisions
        - NameNode-led overwrite/resync flow was kept intact
    - local verification:
      - `python3 scripts/foxai_installer/tools/apply_contract_hardening_phase1.py`
      - `gofmt -w scripts/foxai_installer/internal/installer/constants.go scripts/foxai_installer/internal/installer/prompts.go scripts/foxai_installer/internal/installer/reuse.go`
      - `GOOS=linux GOARCH=amd64 go build -o ../installers/installer .` from `scripts/foxai_installer/`
  - live-validation hotfixes implemented on 2026-05-26:
    - added repo-local update script:
      - `tools/apply_live_hotfix_phase2.py`
    - `internal/installer/exec.go`
      - added safer remote command helpers so remote shell snippets now execute through one quoted remote `bash -lc` command string
    - `internal/installer/ssh.go`
      - extracted reusable passwordless-SSH recovery helpers
      - if a later remote step fails and BatchMode SSH still is not actually ready, the installer now re-shows the SSH bootstrap/manual-key guidance instead of hard-failing immediately
    - `internal/installer/bootstrap.go`
      - remote base-package step now retries through the SSH recovery path
    - `internal/installer/install_datanode.go`
      - Hadoop rsync
      - Spark rsync
      - Spark ownership normalization
      - remote rsync bootstrap
      - remote DataNode setup
      - all now retry through the SSH recovery path if passwordless SSH is missing at that later stage
    - `internal/installer/reuse.go`
      - conflicting reused-node wipe now uses the safer remote bash execution helper
      - fixes the live bug where the recommended wipe path failed with `rm: missing operand`
    - local verification:
      - `python3 scripts/foxai_installer/tools/apply_live_hotfix_phase2.py`
      - `gofmt -w scripts/foxai_installer/internal/installer/exec.go scripts/foxai_installer/internal/installer/ssh.go scripts/foxai_installer/internal/installer/install_datanode.go scripts/foxai_installer/internal/installer/bootstrap.go scripts/foxai_installer/internal/installer/reuse.go`
      - `GOOS=linux GOARCH=amd64 go build -o ../installers/installer .` from `scripts/foxai_installer/`
  - follow-up live hotfix on 2026-05-26:
    - first remote-bash fix was still not sufficient in the conflicting-node wipe path during real execution
    - `internal/installer/exec.go`
      - changed `runRemoteBashCommand` again so remote shell snippets are now sent over stdin with:
        - `ssh ... bash -s --`
      - this removes the nested quoted `bash -lc` transport that was still fragile in live SSH execution
    - intended effect:
      - conflicting reused-node wipe should no longer fail with `rm: missing operand`
    - local verification:
      - `gofmt -w scripts/foxai_installer/internal/installer/exec.go`
      - `GOOS=linux GOARCH=amd64 go build -o ../installers/installer .` from `scripts/foxai_installer/`
  - follow-up correction on 2026-05-26:
    - live failure showed the real wipe call site in `internal/installer/reuse.go` was still using the old direct:
      - `ssh ... bash -lc ...`
    - corrected `wipeRemoteDataNodeStorage` to actually use:
      - `runRemoteBashCommand(...)`
      - `withRemoteSSHRecovery(...)`
    - this is the direct fix for the still-reproducing:
      - `rm: missing operand`
    - local verification:
      - `gofmt -w scripts/foxai_installer/internal/installer/reuse.go`
      - `GOOS=linux GOARCH=amd64 go build -o ../installers/installer .` from `scripts/foxai_installer/`
  - follow-up consistency correction on 2026-05-26:
    - after re-checking the module, one more old-style remote shell path remained in the reused-node probe itself
    - `internal/installer/reuse.go`
      - moved the probe path to the same:
        - `runRemoteBashCommand(...)`
        - `withRemoteSSHRecovery(...)`
      - removes the last remaining direct `ssh ... bash -lc ...` call in the installer module
    - local verification:
      - `gofmt -w scripts/foxai_installer/internal/installer/reuse.go`
      - `GOOS=linux GOARCH=amd64 go build -o ../installers/installer .` from `scripts/foxai_installer/`
  - reused-node regression fix on 2026-05-26:
    - live Linux validation showed the stricter `conflicting` classification itself was correct, but the follow-up action was too shallow:
      - wiping only `~/hadoopdata/datanode` left old YARN runtime state behind on the reused host
      - result: reused host could start `DataNode` but not `NodeManager`, and `start-yarn.sh` on the NameNode appeared hung on that node
    - `internal/installer/reuse.go`
      - replaced the conflicting/partial reuse action from `wipeRemoteDataNodeStorage` to `reinitializeRemoteDataNodeState`
      - the new action now:
        - stops old `DataNode` and `NodeManager` daemons if present
        - kills leftover matching JVMs defensively
        - wipes `~/hadoopdata/datanode`
        - clears `/tmp/hadoop-$USER*`
        - clears `$HADOOP_HOME/logs`
        - preserves the existing NameNode-led resync/setup flow afterward
      - updated operator wording from `wipe old HDFS DataNode storage` to `reinitialize old DataNode state`
    - local verification:
      - `gofmt -w scripts/foxai_installer/internal/installer/reuse.go`
      - `GOOS=linux GOARCH=amd64 go build -o ../installers/installer .` from `scripts/foxai_installer/`
      - artifact `scripts/installers/installer`
      - sha256: `935bc236ed346553c8609ca610df97d560c2959929d40a198bcf85376af182da`
  - live Linux validation result on 2026-05-26:
    - user tested the current installer against both:
      - fresh cluster
      - fresh NameNode + fresh DataNode2 + old DataNode1 from a previous cluster
    - validation outcome:
      - both scenarios now run successfully end to end
      - bulk DataNode input, default DataNode username behavior, and the new classification flow are all user-confirmed working
      - the conflicting reused-node path no longer breaks YARN bring-up after the reinitialize fix
    - live environment caveat retained:
      - on fresh GCloud machines, SSH readiness can still be inconsistent during early bootstrap
      - installer recovery path now handles that by falling back to the manual key/bootstrap step and then continuing normally
  - NameNode IP prompt UX hotfix on 2026-05-26:
    - `internal/installer/prompts.go`
      - replaced the mandatory manual NameNode IP prompt with auto-detect + default behavior
      - installer now scans local active network interfaces for the first private IPv4 on the NameNode host
      - prompt now shows:
        - `Namenode private IP [<detected-ip>]:`
      - pressing Enter accepts the detected local private IP
      - manual override is still allowed and still validated as an IP
      - if no usable private IPv4 is detected, installer falls back to the old fully manual prompt
    - local verification:
      - `gofmt -w scripts/foxai_installer/internal/installer/prompts.go`
      - `GOOS=linux GOARCH=amd64 go build -o ../installers/installer .` from `scripts/foxai_installer/`
      - artifact `scripts/installers/installer`
      - sha256: `fc0c7a06deeacdba63087340cc35237eef054a8b56e6557d939d5b91507b9698`
  - rerun no-op hardening on 2026-05-26:
    - user pointed out that a real operator may accidentally enter already-existing DataNodes under `NEW` during a rerun
    - installer now treats this safely in `install` mode by separating:
      - the final cluster shape
      - the subset of DataNodes that still require sync/setup in this run
    - `internal/installer/reuse.go`
      - compatible detection is now stronger than clusterID-only
      - probe now compares remote node state against the NameNode-managed content for:
        - `core-site.xml`
        - `hdfs-site.xml`
        - `workers`
        - `mapred-site.xml`
        - `yarn-site.xml`
        - `hadoop-env.sh`
        - managed `~/.bashrc` block
        - managed `/etc/hosts` block
        - Spark binary presence
        - DataNode storage `VERSION` clusterID
      - only if that state is already exact does the node remain `compatible`
      - compatible nodes are now skipped from remote sync/setup and recorded as already converged
      - operator-skipped conflicting/partial nodes still shrink the final cluster shape as before
    - `internal/installer/install_namenode.go`
      - stores the mutation target list for the current install run
      - skips remote sync entirely when no DataNodes actually need work
    - `internal/installer/run.go`
      - install mode now runs remote DataNode setup only for mutation targets instead of blindly for every requested node
      - install summary now reports cluster shape and mutation-target count separately
    - local verification:
      - `gofmt -w scripts/foxai_installer/internal/installer/reuse.go scripts/foxai_installer/internal/installer/install_namenode.go scripts/foxai_installer/internal/installer/run.go scripts/foxai_installer/internal/installer/types.go`
      - `GOOS=linux GOARCH=amd64 go build -o ../installers/installer .` from `scripts/foxai_installer/`
      - artifact `scripts/installers/installer`
      - sha256: `da5b3f8bcf9535010bb5a232168f5b22c0c424a98a045b3476a04a94037d3f3b`
  - compatible-probe `.bashrc` comparison hotfix on 2026-05-26:
    - live rerun test showed same-cluster DataNodes were being misclassified as `partial` with:
      - `managed env block differs from namenode-managed content`
    - root cause:
      - `desiredEnvBlock()` in `internal/installer/config_files.go` had a stray trailing tab from source indentation
      - the new exact hash-based compatible probe compared against that malformed expected block
      - `source ~/.bashrc` was not the cause because it does not modify the file on disk
    - fix:
      - removed the stray trailing tab from the expected managed `.bashrc` block
    - local verification:
      - `gofmt -w scripts/foxai_installer/internal/installer/config_files.go`
      - `GOOS=linux GOARCH=amd64 go build -o ../installers/installer .` from `scripts/foxai_installer/`
      - artifact `scripts/installers/installer`
      - sha256: `77cc98f580f1844f0226d401755efe666ba4d08f2ea54e68b995e34b8cc493c5`
  - post-install verification hardening on 2026-05-26:
    - live run showed the optional `start-dfs.sh && start-yarn.sh` verification step could appear hung, especially when one node did not fully bring up all YARN processes
    - `internal/installer/exec.go`
      - added `runCommandWithTimeout(...)`
    - `internal/installer/run.go`
      - changed the optional service-start path to:
        - run `start-dfs.sh` separately with a timeout
        - run `start-yarn.sh` separately with a timeout
        - warn and continue into verification instead of blocking indefinitely
    - `internal/installer/verify.go`
      - added timeouts to local `jps` and `yarn node -list` verification commands
    - intended behavior:
      - installer should no longer appear frozen forever in the optional post-install start/verify step
      - operator still gets warnings plus verification output instead of a stuck terminal
    - local verification:
      - `gofmt -w scripts/foxai_installer/internal/installer/exec.go scripts/foxai_installer/internal/installer/run.go scripts/foxai_installer/internal/installer/verify.go`
      - `GOOS=linux GOARCH=amd64 go build -o ../installers/installer .` from `scripts/foxai_installer/`
  - timeout/process-tree correction on 2026-05-26:
    - live Ctrl-C output showed the optional service-start step was hanging inside Hadoop `pdsh`, which meant the first timeout wrapper was not killing the full spawned process tree
    - `internal/installer/exec.go`
      - rewrote `runCommandWithTimeout(...)` to:
        - start the command in its own process group
        - kill the whole process group on timeout
      - intended effect:
        - optional post-install `start-dfs.sh` / `start-yarn.sh` should no longer leave `pdsh` hanging in the foreground when the timeout is hit
    - local verification:
      - `gofmt -w scripts/foxai_installer/internal/installer/exec.go`
      - `GOOS=linux GOARCH=amd64 go build -o ../installers/installer .` from `scripts/foxai_installer/`

**Current On-Disk Truth**

- Two real bootstrap scripts already exist under `scripts/`:
  - `scripts/setup_namenode_v5.sh`
  - `scripts/setup_datanode.sh`
- The scripts are concrete platform installers/configurators, not packaging wrappers yet.
- `setup_namenode_v5.sh` currently handles large parts of bootstrap directly, including package install, Java/Hadoop/Spark setup, SSH setup, host mapping, and cluster configuration flow.
- `setup_datanode.sh` currently handles DataNode-side prerequisites, environment setup, host mapping, and cluster join preparation.
- The repo contains active FoxAI DAG/job code under `dags/`, including:
  - `dags/combined_domains/`
  - `dags/realtime_rabbitmq/`
- No actual customer add-on/plugin implementation layer is present on disk yet; current plugin/licensing references are still planning/documentation text rather than runnable repo modules.
- No real licensing implementation is present on disk yet; current licensing references are still documentation-level only.
- Priority order now is:
  - first: package the platform bootstrap only
  - second: define/provide the customer script or extension/template path on top of the installed platform
  - last: licensing
- The base package must not bundle FoxAI demo DAGs, FoxAI job scripts, or customer job scripts.
- A template/example for customer authoring may be included later, but customer-specific logic remains outside the package.
- The desired customer-facing shape is now one file only, likely packaged later as a binary, but still terminal-based and interactive.
- Optional credentials/settings should support `blank => default` behavior where current scripts already provide defaults.
- A real unified installer entrypoint now exists at `scripts/foxai_installer.sh`.
- A first native Go installer source now also exists at `scripts/foxai_installer.go`.
- A separate GCloud bootstrap variant now also exists at `scripts/gcloud_installer.go`.
- Current installer behavior:
  - one combined terminal prompt flow
  - exact pinned Hadoop/Spark/Java 11 versions from the current setup scripts
  - current MinIO defaults with blank-input fallback
  - optional Kakao mirror override kept explicit as a premise-specific choice
  - local NameNode setup followed by automatic remote DataNode setup
- Current GCloud bootstrap behavior:
  - NameNode key generation/check still happens locally first
  - installer then pauses and prints the exact public key for manual paste into each DataNode `~/.ssh/authorized_keys`
  - passwordless SSH is verified before any remote DataNode setup continues
- Current Go installer direction:
  - customer-facing path is intended to become a single Linux binary so customers do not receive readable shell source
  - `foxai_installer.go` currently uses local/remote command orchestration to mirror the tested shell behavior
  - after cluster setup completes, it prompts for hardware collection mode and prints recommended Spark settings
  - current hardening gap:
    - install mode still contains bootstrap-style mutation points that are too aggressive for an already-installed cluster unless the desired state is matched exactly
  - latest on-disk productization pass on 2026-05-22:
    - added new top-level modes:
      - `--repair`
      - `--reconcile`
    - added installer manifest output:
      - `~/.foxai-installer/last-run.json`
    - added target DataNode selection prompt for existing-cluster mutation modes
    - added confirm-before-mutation flow for `repair` and `reconcile`
    - changed Hadoop config writers so:
      - `install` still blocks on drift
      - `repair` and `reconcile` overwrite FoxAI-managed drift after user confirmation
    - added post-mutation verification path:
      - optional `start-dfs.sh && start-yarn.sh`
      - `hdfs getconf -confKey dfs.replication`
      - local `jps`
      - `yarn node -list`
    - added printed summary entries plus manifest serialization for run outcome tracking
  - Current verification checkpoint after the first code edit in this pass:
  - `gofmt -w scripts/foxai_installer.go` passed
  - `go build -o /tmp/foxai_installer_check scripts/foxai_installer.go` passed
  - local runtime execution of the read-only modes was not possible on this workstation because the installer intentionally exits outside Linux (`this installer only supports Linux`)
  - Linux-targeted build verification passed:
    - `GOOS=linux GOARCH=amd64 go build -o /tmp/foxai_installer_linux_amd64 scripts/foxai_installer.go`
    - resulting artifact is an ELF x86-64 Linux binary
- Current install-first contract refactor on 2026-05-22:
  - `install` mode is now explicitly treated as fresh-cluster bootstrap only
  - current code changes in `scripts/foxai_installer.go`:
    - added fresh-install guard:
      - blocks install if any `ExistingNodeIPs` are provided
      - blocks install if Namenode already appears formatted
    - changed Namenode `.bashrc` handling to a FoxAI-managed env block instead of broad append/grep logic
    - changed DataNode `.bashrc` handling to the same FoxAI-managed env block style
    - changed DataNode `/etc/hosts` handling from minimal local block to full FoxAI cluster block
    - changed local `/etc/hosts` managed block handling to replace/update the FoxAI block rather than treating block drift as fatal in fresh install mode
    - simplified DataNode sync path back to full sync across install-target nodes because existing-cluster reconciliation is no longer part of `install`
  - verification after this refactor:
    - `gofmt -w scripts/foxai_installer.go` passed again
    - `go build -o /tmp/foxai_installer_check scripts/foxai_installer.go` passed again
  - verification after the full-product mode wiring on 2026-05-22:
    - `gofmt -w scripts/foxai_installer.go` passed
    - `go build -o /tmp/foxai_installer_check scripts/foxai_installer.go` passed
  - deploy checkpoint on 2026-05-22:
    - pushed source to Namenode:
      - `/home/ubuntu/daihai_script/install_script/foxai_installer.go`
    - pushed Linux binary to Namenode:
      - `/home/ubuntu/daihai_script/install_script/foxai-installer`
    - remote verification:
      - `ls -l` confirms file present on Namenode
      - `sha256`:
        - `31751b367bf092a600b7b1f419f5b760b9b4c4defa0fb8e0413574279af93f1b`
        - binary: `92c0a85fa9e6c97c1c8993bfb652a0c4222b3d8aae6e53ca27feeb2d53740120`
  - follow-up patch after first live `--preflight` run on 2026-05-22:
    - fixed incorrect expected `dfs.datanode.data.dir` path generation in Go installer
      - was incorrectly deriving `file:///ubuntu/...`
      - now derives `file:///home/<user>/...`
    - relaxed local `.bashrc` preflight evaluation so legacy correct env lines are accepted even before migrating into the new FoxAI managed block format
    - re-pushed updated Namenode artifacts:
      - source sha256: `742e41bd94b0a855553d4ec850e7e9f5527e547255c3dbea8e4c36b475ecffc2`
      - binary sha256: `b521c4b302a319e1d4aeaf2cd198298e76bd7d424c330e4d912fa8d425de7c49`
- Current live-cluster audit checkpoint on 2026-05-22:
  - Namenode host state:
    - `/home/ubuntu/hadoop`, `/opt/spark`, and `/home/ubuntu/hadoopdata` exist
    - `.bashrc` contains the full FoxAI Hadoop/Spark env block including `HADOOP_SSH_OPTS` and `PDSH_RCMD_TYPE`
    - `/etc/hosts` contains the full 1 Namenode + 5 DataNode FoxAI block
    - Hadoop XML files and `workers` file are present and cluster-consistent except for replication
  - Runtime state:
    - `yarn node -list` shows 5 running DataNodes
    - `hdfs getconf -confKey dfs.replication` returns `2`
  - DataNode1 host state:
    - `/home/ubuntu/hadoop`, `/opt/spark`, and `/home/ubuntu/hadoopdata/datanode` exist
    - passwordless sudo works
    - `.bashrc` has Java/Hadoop/Spark exports but is missing `HADOOP_SSH_OPTS` and `PDSH_RCMD_TYPE`
    - `/etc/hosts` already contains the full FoxAI block, not the minimal temporary block described in `setup_datanode.sh`
    - Hadoop XML files match the Namenode copies, including `dfs.replication=2`
- Immediate hotfix opened on 2026-05-22:
  - user wants the live cluster default HDFS replication restored from `2` to `3`
  - planned scope:
    - Namenode `/home/ubuntu/hadoop/etc/hadoop/hdfs-site.xml`
    - synced Hadoop config on DataNodes
  - planned verification:
    - inspect exact Namenode file before edit
    - update only `dfs.replication`
    - sync Hadoop config to DataNodes
    - verify with `hdfs getconf -confKey dfs.replication`
  - exact change made:
    - changed `<dfs.replication>` from `2` to `3` in Namenode `/home/ubuntu/hadoop/etc/hadoop/hdfs-site.xml`
    - created backup `/home/ubuntu/hadoop/etc/hadoop/hdfs-site.xml.bak_20260522_rep2`
    - synced the updated `hdfs-site.xml` to `datanode1` through `datanode5`
  - verification status:
    - Namenode file now shows `dfs.replication=3`
    - all five DataNodes now show `dfs.replication=3`
    - `hdfs getconf -confKey dfs.replication` returns `3` on Namenode
    - `hdfs getconf -confKey dfs.replication` also returns `3` from `datanode1`
  - note:
    - this fixes the default replication for new HDFS clients/config consumers
    - it does not by itself force existing files already written at replication `2` to be re-replicated
- The old plan-only prototype file was removed so `scripts/foxai_installer.sh` is the active single truth file for this packaging task.
- Installer hardening updates on 2026-05-25:
  - `scripts/gcloud_installer.go` was added as a separate Go entrypoint for GCloud-like environments where SSH trust must be bootstrapped manually on each DataNode
  - Java 17 installation was removed from:
    - `scripts/foxai_installer.go`
    - `scripts/gcloud_installer.go`
    - `scripts/foxai_installer.sh`
    - `scripts/setup_namenode_v5.sh`
    - `scripts/foxai_installer_premise_notes.md`
  - Java 11 is now the only managed Java runtime in the installer family
- Local artifact build checkpoint on 2026-05-25:
  - created output folder:
    - `scripts/installers/`
  - built Linux artifact:
    - `scripts/installers/foxai_installer`
  - artifact verification:
    - file type: ELF 64-bit LSB executable, x86-64
    - sha256: `085ced68c0fe8fa75cdeb5e49fe228f17e112ee1690ffbee549b6c736a3bc846`
  - built Linux artifact:
    - `scripts/installers/gcloud_installer`
  - artifact verification:
    - file type: ELF 64-bit LSB executable, x86-64
    - sha256: `2723c69932395f555ead341c951be555c7294506297520429742f728ca44e925`
- Bootstrap-dependency gate update on 2026-05-25:
  - both Go installers now run a new first step named `BOOTSTRAP DEPENDENCIES` before collecting the rest of the normal install flow
  - that step installs missing base commands the installers themselves assume exist, instead of failing immediately on a fresh VM
  - files changed:
    - `scripts/foxai_installer.go`
    - `scripts/gcloud_installer.go`
  - verification:
    - `gofmt -w scripts/foxai_installer.go scripts/gcloud_installer.go` passed
    - `GOOS=linux GOARCH=amd64 go build -o scripts/installers/foxai_installer scripts/foxai_installer.go` passed
    - `GOOS=linux GOARCH=amd64 go build -o scripts/installers/gcloud_installer scripts/gcloud_installer.go` passed
- Bootstrap first-step sudo removal on 2026-05-25:
  - removed `sudo` from the `BOOTSTRAP DEPENDENCIES` step in both Go installers
  - first-step package install now invokes `apt-get` directly instead of `sudo apt-get`
  - files changed:
    - `scripts/foxai_installer.go`
    - `scripts/gcloud_installer.go`
  - verification:
    - `gofmt -w scripts/foxai_installer.go scripts/gcloud_installer.go` passed
    - `GOOS=linux GOARCH=amd64 go build -o scripts/installers/foxai_installer scripts/foxai_installer.go` passed
    - `GOOS=linux GOARCH=amd64 go build -o scripts/installers/gcloud_installer scripts/gcloud_installer.go` passed
- Local/bootstrap sudo command removal on 2026-05-25:
  - removed literal `sudo` command prefixes from the local/bootstrap package-management and filesystem mutation commands in both Go installers
  - changed local/bootstrap command paths to direct:
    - `apt update`
    - `apt install`
    - `sed -i`
    - `ln -sf`
    - `gpg --dearmor`
    - direct writes to `/etc/apt/sources.list.d/...`
    - `mv`
    - `chown`
    - direct `python3` for the local `/etc/hosts` rewrite helper
  - also removed `sudo` from the embedded remote DataNode bootstrap script’s package/bootstrap commands before its normal package setup continues
  - rebuilt installer artifacts:
    - `scripts/installers/foxai_installer`
      - sha256: `a1d6fa9cbec321e73b28f58164618b5147456fe642c5b6c65eb44bf9c4209177`
    - `scripts/installers/gcloud_installer`
      - sha256: `eeb138f4cdd643b474c131a5e236ab94680bff837682cb0a6abff1ca427b39ac`
  - remaining `sudo` references are now limited to the explicit remote DataNode sudo-check / NOPASSWD logic and helper text, not the local/bootstrap command prefixes that were breaking at startup
- MinIO default UX fix on 2026-05-25:
  - both Go installers now derive the MinIO endpoint default from the entered NameNode IP:
    - `<namenode-ip>:9001`
  - files changed:
    - `scripts/foxai_installer.go`
    - `scripts/gcloud_installer.go`
  - verification:
    - `gofmt -w scripts/foxai_installer.go scripts/gcloud_installer.go` passed
    - `GOOS=linux GOARCH=amd64 go build -o scripts/installers/foxai_installer scripts/foxai_installer.go` passed
    - `GOOS=linux GOARCH=amd64 go build -o scripts/installers/gcloud_installer scripts/gcloud_installer.go` passed
- Startup memory path correction on 2026-05-25:
  - `agents/foxai.agent.md` now points startup reads to:
    - `markdown/rule.md`
    - `markdown/project.md`
    - `markdown/logs.md`
    - `markdown/progress.md`
  - `markdown/rule.md` now explicitly treats `markdown/progress.md` as the session state file instead of a root-level `progress.md`

**Risks**

- Prior packaging understanding was too broad and mixed platform bootstrap with pipeline content.
- The current setup scripts are interactive and environment-specific, so packaging work will need a deliberate boundary/spec before implementation.
- Customer extension/template work overlaps with packaging but is sequenced after the packaging boundary is defined.
- Licensing should stay generic and remain a later phase until packaging/customer-path work is clearer.
- Some logic in the source scripts is premise-specific, such as the Kakao apt mirror override, and should remain visible/extensible rather than hidden inside one monolithic flow.
- The unified installer now executes the intended orchestration shape on paper, but it still needs real-environment validation before it can be called production-ready.
- The Go installer is not yet parity-validated against the full tested shell flow, so it is not ready to replace the shell reference path yet.
- The current safe test path on a live cluster is to use `--dry-run` or `--recommend-only`, not the full install mode.
- The current customer-delivery intent is a stripped Linux binary built from Go, but the binary packaging and runtime validation steps have not been completed yet.
- Specific risks in this hardening pass:
  - existing-cluster safety depends on distinguishing exact-match managed state from drift
  - default config rewrites or `rsync --delete` behavior against existing nodes would make install mode too destructive for product-style use

---

### Task 6 — HDOS PatientRecord Hospital-Facing Refactor

**Goal:** Replace the `tb_nhanvienlog` technical demo logic in `hdos_sample` with a hospital-facing `tb_patientrecord` medallion flow that can handle a wide production-style schema while keeping MinIO + Iceberg and the existing Airflow DAG shape.

**Mode:** Refactor

**Current Phase:** `hdos_sample` remains deployed to namenode and runtime-validated by the user on 2026-05-21. The separate widget-focused DAG `hdos_widget` is also deployed to namenode on 2026-05-21, registered in Airflow, and user-confirmed working.

**Next Exact Step:**
1. Replay one Gold task by clearing only that task, confirming other Gold task outputs are untouched
2. Decide whether to keep the current single parameterized Gold script or split it into separate physical Gold files for operational clarity
3. Treat `Xe cấp cứu 115` / `Xe 115 hoạt động` and exact population-health registry parity as separate discovery tasks, because exact populated source tables are still unconfirmed or empty in the current demo database
4. If JDBC/Spark later fails on PostgreSQL-native columns in broader runs (especially `bytea` / `tsvector`), patch the relevant configured source query with compatibility casts

**Files In Scope**

- `dags/hdos_sample/hdos_sample_config.json`
  - status: updated | verified: local
  - source table switched to `tb_patientrecord`
  - deterministic dev query now set to `SELECT * FROM public.tb_patientrecord ORDER BY patientrecordid LIMIT 1000`
  - primary key config now set to `patientrecordid`

- `dags/hdos_sample/hdos_sample_config.py`
  - status: updated | verified: local
  - now exposes `PG_SOURCE_QUERY` and `PG_SOURCE_PRIMARY_KEY`

- `dags/hdos_sample/postgres_to_raw.py`
  - status: updated | verified: local syntax
  - now supports `query` vs `dbtable` source read mode from config
  - validates configured primary key existence
  - logs source mode, source label, row count, and column count

- `dags/hdos_sample/raw_to_bronze.py`
  - status: updated | verified: local syntax
  - old login-specific narrow projection removed
  - now preserves the full raw table shape into Bronze with metadata columns retained

- `dags/hdos_sample/bronze_to_silver.py`
  - status: updated | verified: local syntax
  - old login-specific Silver model removed
  - now keeps the wide table, trims all string columns, drops duplicate `patientrecordid`, and adds helper columns such as:
    - `encounter_date`
    - `reception_date`
    - `admission_date`
    - `discharge_date`
    - `insurance_start_date`
    - `insurance_end_date`
    - `primary_diagnosis_icd10`
    - `secondary_diagnosis_icd10`
    - `has_insurance_code`
    - `is_bhyt_covered`

- `dags/hdos_sample/silver_to_gold.py`
  - status: updated | verified: local syntax
  - old login/domain summary removed
  - now writes multiple hospital-topic Gold tables:
    - `gold_catalog.hdos.tb_patientrecord_daily_financial_summary`
    - `gold_catalog.hdos.tb_patientrecord_daily_diagnosis_summary`
    - `gold_catalog.hdos.tb_patientrecord_daily_coverage_summary`
    - `gold_catalog.hdos.tb_patientrecord_daily_discharge_summary`

- `dags/hdos_widget/`
  - status: deployed to namenode | verified: local syntax + source SQL checks + Airflow registration
  - copied from `hdos_sample` and refactored as a separate DAG id `hdos_widget`
  - uses a separate config module `hdos_widget_config.py` and config file `hdos_widget_config.json`
  - ingests seven widget source extracts:
    - `tb_patientrecord`
    - `tb_invoice`
    - `tb_treatment`
    - `tb_bed`
    - `tb_department`
    - `tb_phacdodieutri`
    - `tb_phacdodieutri_phieudieutri`
  - current Airflow shape:
    - `postgres_to_raw >> raw_to_bronze >> bronze_to_silver`
    - five same-level Gold tasks branch from `bronze_to_silver`:
      - `gold_encounter_activity`
      - `gold_finance_classification`
      - `gold_inpatient_summary`
      - `gold_bed_occupancy`
      - `gold_clinical_pathway`
  - deployed remote paths:
    - runtime: `/home/ubuntu/daihai_script/hdos_widget/`
    - DAG: `/home/ubuntu/airflow/dags/hdos_widget.py`

**Current On-Disk Truth**

- The local refactor now targets `public.tb_patientrecord`, not `public.tb_nhanvienlog`.
- Deployment verification completed on namenode:
  - runtime files pushed to `/home/ubuntu/daihai_script/hdos_sample/`
  - DAG pushed to `/home/ubuntu/airflow/dags/hdos_sample.py`
  - local and remote `sha256` matched for:
    - `hdos_sample_config.json`
    - `hdos_sample_config.py`
    - `postgres_to_raw.py`
    - `raw_to_bronze.py`
    - `bronze_to_silver.py`
    - `silver_to_gold.py`
    - `hdos_sample.py`
  - stale remote files removed:
    - `/home/ubuntu/daihai_script/hdos_sample/foxai_config.json`
    - `/home/ubuntu/daihai_script/hdos_sample/foxai_config.py`
  - Airflow CLI check passed:
    - `/home/ubuntu/airflow-venv/bin/airflow dags list | grep hdos_sample`
  - Airflow import error check now returns `No data found`
  - user confirmed on `2026-05-21` that the deployed DAG worked
- Verified source signals from namenode / PostgreSQL during this refactor:
  - total rows: `763,887`
  - distinct `patientrecordid`: `763,887`
  - distinct `patientid`: `316,233`
  - null `patientrecorddate`: `0`
  - null `patientname`: `0`
  - null `tongchiphi`: `0`
- Confirmed source schema profile:
  - `434` columns total
  - `181` integer
  - `169` text
  - `49` double precision
  - `27` timestamp without time zone
  - `4` bytea
  - `3` boolean
  - `1` tsvector
- Confirmed sample row fields are hospital-meaningful, including identifiers, encounter dates, department/room, ICD10 diagnosis fields, insurance code, and cost/coverage amounts.
- The Airflow DAG file `dags/hdos_sample/hdos_sample.py` was not changed in this pass because the task wiring still matches the same four-stage flow.
- Source coverage for the HDOS executive dashboard has now been documented in `dags/hdos_sample/HDOS_SOURCE_FINDINGS.md`:
  - exact / strong coverage confirmed for encounter activity, revenue, inpatient counts, BOR, and bed occupancy inputs
  - derivable coverage confirmed for finance classification and clinical pathway
  - partial-only coverage for alerts and population health in the current demo data
  - `Xe cấp cứu 115` / `Xe 115 hoạt động` remain unconfirmed from an exact populated source table
- Local validation for the new `hdos_widget` source extracts:
  - `tb_patientrecord` configured extract returns `1000` rows
  - `tb_invoice` configured extract returns `2179` rows for those encounters
  - `tb_treatment` configured extract returns `16842` rows for those encounters
  - `tb_bed` configured extract returns `1528` rows
  - `tb_department` configured extract returns `37` rows
  - `tb_phacdodieutri` configured extract returns `23` rows
  - `tb_phacdodieutri_phieudieutri` configured extract returns `127` rows
  - local checks passed:
    - `python3 -m py_compile dags/hdos_widget/*.py`
    - `python3 -m json.tool dags/hdos_widget/hdos_widget_config.json`
- Deployment verification for `hdos_widget`:
  - runtime files pushed to `/home/ubuntu/daihai_script/hdos_widget/`
  - DAG pushed to `/home/ubuntu/airflow/dags/hdos_widget.py`
  - Airflow registration check passed:
    - `/home/ubuntu/airflow-venv/bin/airflow dags list | grep hdos_widget`
  - Airflow import error check passed:
    - `/home/ubuntu/airflow-venv/bin/airflow dags list-import-errors`
    - result: `No data found`
- Runtime validation for `hdos_widget`:
  - user confirmed on `2026-05-21` that the pipeline worked
  - observed runtime behavior: Gold tasks are wired as same-level siblings after `bronze_to_silver`, but in the current Airflow environment they executed sequentially rather than in parallel
  - the sibling Gold task shape still enables targeted replay by clearing one Gold task only

**Risks**

- The current development path uses a deterministic 1000-row sample query; full-table performance and edge cases are still unvalidated.
- PostgreSQL-native `bytea` and `tsvector` fields may still require explicit cast/encoding logic at raw ingest time if Spark JDBC or Iceberg rejects them at runtime.
- Bronze currently preserves the full wide table but does not yet add table-specific canonical casting beyond what JDBC already provides.
- Gold outputs are first-pass marts based on source context and sample profiling; they may need adjustment after user review or richer hospital-table joins.

---

## Completed Archive

### Task 5 — HDOS Sample PostgreSQL To Lakehouse DAG

**Goal:** Create a simple sample DAG for one populated HDOS PostgreSQL table that ingests from PostgreSQL into raw, bronze, silver, and gold Iceberg tables on MinIO, ready for downstream Superset querying.

**Mode:** Hotfix

**Current Phase:** Complete — deployed and runtime-validated on `2026-05-20`.

**Next Exact Step:** None unless the user wants to replace the technical login sample with a more hospital-facing sample. If reopened, use the confirmed hospital-grade base tables listed in `dags/hdos_sample/HDOS_SOURCE_FINDINGS.md`.

**Files In Scope**

- `dags/hdos_sample/`
  - status: done | verified: local + remote runtime
  - isolated sample DAG path for HDOS PostgreSQL ingestion

- `/home/ubuntu/daihai_script/hdos_sample/`
  - status: done | verified: remote runtime
  - deployed runtime scripts/config on namenode

- `/home/ubuntu/airflow/dags/hdos_sample.py`
  - status: done | verified: remote runtime
  - deployed Airflow DAG file on namenode

- `public.tb_nhanvienlog` on PostgreSQL `test05052026`
  - status: verified source | verified: remote runtime via namenode + Spark
  - first working sample source table
  - columns used include PK `nhanvienlogid`, timestamp `logintime`, and operational dimensions such as `computername`, `username`, `domain`, `ipaddress`, `softversion`

**Current On-Disk Truth**

- Namenode can reach PostgreSQL at `192.168.100.78:5630`.
- Successful namenode connection was verified with:
  - `psql -h 192.168.100.78 -p 5630 -U postgres -d test05052026 -c "select current_database(), current_user;"`
- Database `test05052026` has schemas:
  - `api`
  - `archive_data`
  - `no_backup`
  - `public`
- The first usable populated sample table chosen for the working DAG is `public.tb_nhanvienlog`.
- `public.tb_cakhambenh_thoigian` was inspected earlier but returned `0` rows and was not used for the sample.
- Local sample pipeline files exist under `dags/hdos_sample/`:
  - `hdos_sample_config.json`
  - `hdos_sample_config.py`
  - `postgres_to_raw.py`
  - `raw_to_bronze.py`
  - `bronze_to_silver.py`
  - `silver_to_gold.py`
  - `hdos_sample.py`
  - `HDOS_SOURCE_FINDINGS.md`
- Current working sample logic uses:
  - PostgreSQL source table `public.tb_nhanvienlog`
  - raw Iceberg table `raw_catalog.hdos_sample.tb_nhanvienlog_raw`
  - bronze Iceberg table `bronze_catalog.hdos_sample.tb_nhanvienlog_bronze`
  - silver Iceberg table `silver_catalog.hdos_sample.tb_nhanvienlog_silver`
  - gold Iceberg table `gold_catalog.hdos_sample.tb_nhanvienlog_daily_domain_summary`
- Gold output meaning:
  - daily login activity summary by `login_date`, `domain`, and `softversion`
  - metrics:
    - `login_count`
    - `distinct_employee_count`
    - `distinct_computer_count`
    - `distinct_ip_count`
- Local verification completed:
  - `python3 -m py_compile dags/hdos_sample/*.py` passed
- Deployment completed:
  - runtime scripts/config pushed to `/home/ubuntu/daihai_script/hdos_sample/`
  - DAG pushed flat to `/home/ubuntu/airflow/dags/hdos_sample.py`
  - Airflow CLI confirmed DAG registration and task graph
- Runtime findings:
  - Spark successfully resolved and downloaded `org.postgresql:postgresql:42.7.3`
  - initial run failed because PostgreSQL `pg_hba.conf` only allowed namenode and blocked Spark executors on datanodes
  - PostgreSQL access was widened on the Windows host to cluster subnet `192.168.100.0/24`
  - after the subnet rule, `postgres_to_raw` succeeded
  - Iceberg emitted first-create `version-hint.text` warnings, but the task still committed and completed successfully
- User-confirmed outcomes:
  - the full `hdos_sample` DAG worked
  - Superset was able to query the Gold table and draw charts successfully
- Confirmed hospital-grade source tables for a future HDOS business sample are recorded in:
  - `dags/hdos_sample/HDOS_SOURCE_FINDINGS.md`

**Risks**

- PostgreSQL access is currently broadened for internal testing via subnet-level `pg_hba.conf` trust auth; this should be hardened later for production use.
- The current Gold table is a technical/operational login sample, not yet a hospital business KPI sample.
- A later HDOS iteration should likely move from `tb_nhanvienlog` to richer hospital tables such as `tb_patientrecord`, `tb_servicedata`, `tb_invoice`, `tb_treatment`, and `tb_nhanvien`.

### Task 1 — realtime_rabbitmq 5-Day File-vs-Row Validation

**Goal:** Build the clarified validation behavior on `realtime_rabbitmq`: 5 rows mapped to 5 days, with file mode run once on the full file and row mode run incrementally one day at a time so the final row-mode day-5 chart matches the file-mode final chart.

**Mode:** Hotfix

**Current Phase:** Complete — archived per user direction after deployed runtime verification on 2026-05-20.

**Next Exact Step:** None unless the user reopens validation. If reopened, inspect existing MinIO artifacts under `demo/file/fare_amount/` and `demo/row_day1` through `demo/row_day5` before rerunning anything.

**Files In Scope**

- `realtime_rabbitmq/realtime_rabbitmq.py`
  - status: done | verified: remote runtime
  - DAG gate now validates `mode` and `snapshot_label` from ingest summary before allowing calculation/chart

- `realtime_rabbitmq/realtime_fare_amount_rabbitmq_ingest_event.py`
  - status: done | verified: remote runtime
  - file mode keeps full-file ingest semantics
  - row mode now writes cumulative parquet state after each accepted row
  - ingest summary now records `mode` and fixed MinIO folder label: `file` or `row_dayN`
  - file mode and row mode write to separate MinIO state namespaces

- `realtime_rabbitmq/realtime_fare_amount_rabbitmq_calculation_job.py`
  - status: done | verified: remote runtime
  - reads file mode from JSON state and row mode from parquet state
  - writes shallow MinIO output directly under `demo/file/...` or `demo/row_dayN/...`

- `realtime_rabbitmq/realtime_fare_amount_histogram_job.py`
  - status: done | verified: remote runtime
  - chart rendering now follows the same fixed folder labels from calculation

- `realtime_rabbitmq/inbox/batch/fare_amount_5day_validation_batch.json`
  - status: done | verified: local
  - canonical 5-row file-mode input for this task

- `realtime_rabbitmq/inbox/rows/fare_amount_row_day1.json`
  - status: done | verified: local
  - row-mode day 1 input

- `realtime_rabbitmq/inbox/rows/fare_amount_row_day2.json`
  - status: done | verified: local
  - row-mode day 2 input

- `realtime_rabbitmq/inbox/rows/fare_amount_row_day3.json`
  - status: done | verified: local
  - row-mode day 3 input

- `realtime_rabbitmq/inbox/rows/fare_amount_row_day4.json`
  - status: done | verified: local
  - row-mode day 4 input

- `realtime_rabbitmq/inbox/rows/fare_amount_row_day5.json`
  - status: done | verified: local
  - row-mode day 5 input

**Current On-Disk Truth**

- The implementation target is `realtime_rabbitmq`, not `realtime_validate`.
- Local inbox samples under `realtime_rabbitmq/inbox/` contain exactly one 5-row batch file and five single-row day files.
- The five row-day JSON inputs use row-specific `event_id` values (`fare-demo-5day-row-000N`) so row-mode events are not deduped against the file-mode batch rows.
- Local `realtime_rabbitmq` refactor changes do the following:
  - file mode keeps full-file ingest → calculate → chart
  - row mode appends one row per event and writes cumulative parquet state after each row
  - file mode and row mode use separate MinIO state namespaces, so row-mode state no longer appends onto file-mode state
  - row/file outputs write to shallow fixed MinIO folders:
    - `demo/file/fare_amount/...`
    - `demo/row_day1/fare_amount/...`
    - `demo/row_day2/fare_amount/...`
    - `demo/row_day3/fare_amount/...`
    - `demo/row_day4/fare_amount/...`
    - `demo/row_day5/fare_amount/...`
- Namenode deploy for the refactor is complete:
  - DAG pushed to `/home/ubuntu/airflow/dags/realtime_rabbitmq.py`
  - scripts pushed to `/home/ubuntu/daihai_script/realtime_rabbitmq/`
  - local/remote `sha256` matched for DAG + ingest/calc/chart files
  - remote `pyarrow` availability confirmed (`23.0.1`)
  - Airflow CLI confirmed DAG presence: `realtime_rabbitmq`
- User confirmed on `2026-05-20` that the deployed `realtime_rabbitmq` DAG worked after the row-event-id change and the file-vs-row state isolation fix.
- Explicit `row_day5` versus `file` chart parity is still not recorded in chat; that gap is archived here as a follow-up note rather than an active task.

**Risks**

- Row-mode parquet write/read depends on `pyarrow` availability in the remote runtime.
- If validation is reopened later, confirm whether final `row_day5` and `file` parity was checked before any rerun.
- Any future change must preserve the clarified comparison contract: 5 file/row day steps, 5 row charts, and day-5 parity with file mode.
- Any future refactor over 2 files must use the script-first approach from `rule.md`.

### Task 3 — Operator Documentation + Docs Folder Consolidation

**Goal:** Create a Vietnamese operator-facing `.docx` manual for the final-form Data Platform workflow and consolidate document files under one folder.

**Mode:** Hotfix

**Current Phase:** Complete

**Next Exact Step:** None unless user requests another doc edit/regeneration. If resuming doc work, start from the current generator and overwrite the existing `.docx` instead of creating parallel copies.

**Files In Scope**

- `docs/generate_operator_guide_vi.js`
  - status: done | verified: local
  - generator for the operator manual
  - current content reflects user-requested scope:
    - remove opening `Lưu ý phạm vi`
    - remove patchy HTML monitor operation section
    - section 4 rewritten to describe final-form pipeline generically
    - histogram kept as a short independent/currently-separate note

- `docs/Tai_lieu_huong_dan_van_hanh_Data_Platform.docx`
  - status: done | verified: local
  - generated successfully from the updated generator
  - current canonical operator manual output

- `Docs/`
  - status: done | verified: local
  - `outputs/` was renamed to `Docs/`
  - all root-level `.doc/.docx` files were moved into `Docs/`

**Current On-Disk Truth**

- Operator manual exists at `docs/Tai_lieu_huong_dan_van_hanh_Data_Platform.docx`.
- Current generator exists at `docs/generate_operator_guide_vi.js`.
- Existing document inventory consolidated under `Docs/`:
  - `BaoCao_FoxAI_Platform.docx`
  - `DataLakehouse_document.docx`
  - `FoxAI_Customer_Deployment_Plan.docx`
  - `FoxAI_Feature_Approach_Report.docx`
  - `FoxAI_Implementation_Plan_v2.docx`
  - `FoxAI_KeHoach_TrienKhai.docx`
  - `KeHoachTrienKhai.docx`
  - `Tai_lieu_huong_dan_van_hanh_Data_Platform.docx`
- No `.doc/.docx` files remain at repo root.
- Do not add the Superset/Spark Thrift runbook discussion as a tracked task here unless the user explicitly wants it treated as one.

**Risks**

- The useful operator-guide generator was preserved under `docs/`; disposable `.tmp_*` helpers and temp runtime were removed.
- The operator manual is meant to describe the finished production-facing workflow, not current experimental Kafka/RabbitMQ validation paths.

---

## Task Template (copy when adding new task)

### Task N — [Title]

**Goal:** [What are we trying to accomplish]

**Mode:** Hotfix / Refactor

**Current Phase:** [Phase N — description]

**Next Exact Step:** [Exact next action — specific enough that agent can act without asking]

**Files In Scope**

- `path/to/file.py`
  - status: pending / in progress / done | verified: local / remote / no
  - [what this file does in this task]
  - [any constraints or risks specific to this file]

**Current On-Disk Truth**

- [file]: [exact current state — be specific, no summaries]

**Risks**

- [anything uncertain or worth flagging]

---

## Last Updated

2026-05-19T07:37:13Z — Added completed archive entry for operator documentation + `Docs/` consolidation. No Superset setup discussion was added as a tracked task. Task 1 and Task 2 status unchanged.
2026-05-19T08:52:52Z — Updated task memory after clarification from new supervisor input. Task 1 was re-scoped from the older large replay/chunk-state validation path to the actual 5-row / 5-day file-vs-row validation requirement. Added Task 4 to track packaging scope based on `setup_namenode_v5.sh` and `setup_datanode.sh`, with platform bootstrap separated from customer scripts/DAGs.
2026-05-19T09:40:18Z — Refactor Mode patch set for Task 1 was deployed to `realtime_rabbitmq`. Local inbox samples were replaced with one 5-row batch file plus five one-row day files. DAG/ingest/calc/chart files now write fixed MinIO folder labels `file` and `row_day1` through `row_day5`, and row mode materializes parquet state after each accepted row. Namenode deploy completed with matching local/remote `sha256`, remote `pyarrow` confirmed (`23.0.1`), and Airflow CLI confirmed DAG presence: `realtime_rabbitmq`. Runtime validation still pending.
2026-05-19T09:40:18Z — Updated the five `realtime_rabbitmq` row-day inbox files so their `event_id` values are row-specific (`fare-demo-5day-row-0001` ... `0005`) instead of matching the file-batch row IDs. This avoids row-mode duplicate suppression after file mode while keeping the same business-row values for chart comparison. Updated local files were pushed to the namenode inbox path and verified by content.
2026-05-19T09:40:18Z — Fixed file-vs-row state collision in `realtime_rabbitmq`: ingest/calculation now use separate MinIO state keys for file mode and row mode (`.../file/...` vs `.../row/...`), so row-mode charts no longer build on top of file-mode state. Updated ingest/calculation files were pushed to the namenode and verified by matching local/remote `sha256`.
2026-05-20T01:44:50Z — User confirmed the deployed `realtime_rabbitmq` DAG worked after the row-event-id change and the file-vs-row state isolation fix. Treat runtime execution as verified on namenode. The chat does not yet explicitly record whether `demo/row_day5/fare_amount/...` was compared against `demo/file/fare_amount/...`, so keep that as the next precise validation check unless the user says it was already done.
2026-05-20T02:05:00Z — Archived Task 1 from Active Tasks into Completed Archive per user direction. Active work in `progress.md` now stays focused on Combined-Domain Safe-Hardening (Task 2) and Packaging Baseline From Setup Scripts (Task 4).
2026-05-20T03:05:00Z — Audited the repo to re-anchor active work to on-disk truth. Confirmed real bootstrap scripts exist at `scripts/setup_namenode_v5.sh` and `scripts/setup_datanode.sh`, while add-on/licensing remain documentation-only with no runnable implementation layer on disk yet. Updated Task 4 to reflect the next major task as platform packaging boundary definition plus customer extension-path definition, without bundling FoxAI DAGs or customer scripts.
2026-05-20T03:20:00Z — Updated task priority after clarified product direction: Combined-Domain Safe-Hardening (Task 2) is temporarily on hold. Active sequencing is now packaging first from `scripts/setup_namenode_v5.sh` and `scripts/setup_datanode.sh`, then customer script/template or extension-path work, with licensing explicitly last.
2026-05-20T03:35:00Z — Started Phase 1 prototype work for Task 4. The immediate target was a single terminal-based installer entrypoint that combines current NameNode and DataNode input flow, keeps current pinned versions, and supports blank optional inputs using current defaults.
2026-05-20T03:50:00Z — Created an initial packaging prototype, then replaced it with a real unified installer path. Current active files are `scripts/foxai_installer.sh` and `scripts/foxai_installer_premise_notes.md`.
2026-05-20T04:00:00Z — Implemented `scripts/foxai_installer.sh` as the active single-file installer truth for Task 4. It preserves the source-script versions/defaults, runs the NameNode flow locally, then runs the DataNode flow remotely across all configured datanodes. Added `scripts/foxai_installer_premise_notes.md` to keep premise-specific assumptions explicit. Removed the old plan-only prototype file. Verification: `bash -n scripts/foxai_installer.sh` passed and the installer was marked executable.
2026-05-20T04:10:00Z — Synced project memory to the new product direction. `markdown/project.md` now reflects packaging first, customer template/extension path second, licensing later, and Combined-Domain hardening on hold. Near-term work now points at validating and hardening `scripts/foxai_installer.sh`.
2026-05-20T04:20:00Z — Packaging/protection work was put on hold temporarily after the unified shell installer draft. Leave `scripts/foxai_installer.sh` and `scripts/foxai_installer_premise_notes.md` as the resume point when returning to packaging. Current discussion focus shifted to PostgreSQL connection paths into the pipeline.
2026-05-25T00:00:00Z — Resumed Task 4 from `markdown/progress.md` and continued installer hardening from current on-disk state. Added `scripts/gcloud_installer.go` as a separate Go entrypoint for GCloud-like environments where the NameNode cannot use `ssh-copy-id` to bootstrap DataNode trust. The new flow now prints the NameNode public key, instructs manual DataNode `authorized_keys` update plus `chmod 700 ~/.ssh` and `chmod 600 ~/.ssh/authorized_keys`, waits for user confirmation, then verifies passwordless SSH before continuing. Verification: `gofmt -w scripts/gcloud_installer.go` passed; `go build -o /tmp/gcloud_installer_test scripts/gcloud_installer.go` passed.
2026-05-25T00:05:00Z — Simplified the installer family to Java 11 only. Removed Java 17 installation from `scripts/foxai_installer.go`, `scripts/gcloud_installer.go`, `scripts/foxai_installer.sh`, `scripts/setup_namenode_v5.sh`, and updated `scripts/foxai_installer_premise_notes.md` to match. Verification: `gofmt -w scripts/foxai_installer.go scripts/gcloud_installer.go` passed; `go build -o /tmp/foxai_installer_test scripts/foxai_installer.go` passed; `go build -o /tmp/gcloud_installer_test scripts/gcloud_installer.go` passed.
2026-05-25T00:10:00Z — Corrected startup memory instructions so future sessions read `markdown/rule.md`, `markdown/project.md`, `markdown/logs.md`, and `markdown/progress.md`. Updated `agents/foxai.agent.md` and `markdown/rule.md` so the session-state file path is no longer incorrectly described as a root-level `progress.md`.
2026-05-25T00:15:00Z — Relaxed the install-mode guard in `scripts/foxai_installer.go` and `scripts/gcloud_installer.go` so install mode no longer rejects runs that include existing DataNodes on the same NameNode. Mixed existing+new DataNode runs are now treated as cluster convergence, which is required for same-NameNode expansion workflows after the reused-DataNode lifecycle work. Next validation: rerun install on a cluster with the current NameNode, at least one existing DataNode, and at least one new DataNode, then verify sync/setup reaches the new node without guard failure.
2026-05-25T00:20:00Z — Fixed a non-interactive SSH verification mismatch in `scripts/foxai_installer.go` and `scripts/gcloud_installer.go`. The installer was probing `user@IP` with `BatchMode=yes` but without `StrictHostKeyChecking=no`, so a node could be manually reachable while installer verification still failed on host-key/hostname-vs-IP friction. All non-interactive SSH verification/preflight checks in both installers now include `StrictHostKeyChecking=no` to match the intended cluster runtime behavior. Next validation: rerun the GCloud/manual-SSH path against a node that was previously accepted interactively by hostname and confirm the installer no longer blocks at passwordless SSH verification for the IP form.
2026-05-25T00:25:00Z — Restored the runtime-shell verification fix in `scripts/foxai_installer.go` and `scripts/gcloud_installer.go`. Optional service-start and verification commands no longer depend on `source ~/.bashrc` in a non-interactive shell; both installers now export `JAVA_HOME`, `HADOOP_HOME`, `SPARK_HOME`, Hadoop/YARN config dirs, PATH, and `HADOOP_SSH_OPTS` explicitly before running `start-dfs.sh`, `start-yarn.sh`, `jps`, and `yarn node -list`. Next validation: rerun the optional post-install service-start prompt and confirm `start-dfs.sh` is found without manual shell setup.
2026-05-25T00:30:00Z — Normalized later remote SSH execution in `scripts/foxai_installer.go` and `scripts/gcloud_installer.go` after a mixed old/new cluster run showed bootstrap SSH could pass while the reused-DataNode state probe still failed. The reused-state probe, remote base-package check, remote rsync bootstrap, Spark ownership normalization, remote DataNode setup entry, and reused-DataNode wipe path now use the same non-interactive SSH policy as the verified bootstrap path: `BatchMode=yes`, `StrictHostKeyChecking=no`, and `ConnectTimeout=5`. Interactive `ssh -tt` NOPASSWD setup also now disables strict host-key checking for consistency. Next validation: rerun a same-NameNode expansion with one reused old DataNode and confirm the reused-state check reaches the prompt/result stage instead of stopping on SSH.
2026-05-25T00:35:00Z — Added an explicit post-write `source ~/.bashrc` step to both Go installers. On the NameNode, `ensureBashrc()` now sources `~/.bashrc` in a child shell immediately after writing the FoxAI managed env block. In the remote DataNode setup script, the shell now sources `$HOME/.bashrc` right after updating the managed env block before continuing. This improves same-run env availability, while the installer still cannot mutate the already-open parent shell that launched the binary. Next validation: rerun install and confirm no new error appears at the local/remote env-update step.
2026-05-25T00:40:00Z — Fixed the `SYNC TO DATANODES` transport mismatch in both Go installers. The remote SSH checks and prep steps already used explicit `ssh -o BatchMode=yes -o StrictHostKeyChecking=no -o ConnectTimeout=5`, but the actual Hadoop/Spark sync still used plain `rsync` with its default SSH transport. Both `scripts/foxai_installer.go` and `scripts/gcloud_installer.go` now pass `-e "ssh -o BatchMode=yes -o StrictHostKeyChecking=no -o ConnectTimeout=5"` to both rsync calls so the sync step uses the same verified SSH behavior as the rest of the installer. Next validation: rerun a mixed old/new cluster sync and confirm rsync no longer fails on a node that already passed installer SSH verification.
2026-05-25T00:45:00Z — Added a new unified Go entrypoint at `scripts/installer.go` instead of replacing the two existing installers. This new file is based on the current `foxai_installer.go` flow but changes SSH bootstrap to: verify existing passwordless SSH first, try `ssh-copy-id` on missing nodes, then fall back to the manual public-key paste flow from `gcloud_installer.go` only for nodes that still fail. It also uses its own manifest directory `~/.foxai-unified-installer`. Verification: `gofmt -w scripts/installer.go` passed; `GOOS=linux GOARCH=amd64 go build -o scripts/installers/installer scripts/installer.go` passed; artifact `scripts/installers/installer` sha256 `90c511e89fd1ca51e9295bee6102cd0ce6ed730cadfe9f5d307a1df7d6bed5d7`. Next validation: run it against both a simple server path where `ssh-copy-id` should work and a cloud-style path where manual fallback is needed, and confirm one binary handles both bootstrap modes without user file switching.
2026-05-25T00:50:00Z — Isolated `scripts/installer.go` from package-level duplicate declaration conflicts by changing its build tag from `linux` to `linux && unified_installer`. This keeps the unified source file in the repo without colliding with `scripts/foxai_installer.go` in editor/package analysis, while direct file builds for the dedicated binary remain valid. Next validation: confirm the red duplicate-declaration diagnostics disappear in the editor and that `GOOS=linux GOARCH=amd64 go build -o scripts/installers/installer scripts/installer.go` still produces the unified binary.
2026-05-25T00:55:00Z — Replaced the placeholder `graphs/diagram.py` example with a real DTL v3 redraw script written in the normal `diagrams`/Graphviz style: direct node imports, `Diagram(...)`, `Cluster(...)`, and `>>` / `-` edges. The script now uses official `diagrams.onprem.*` nodes where available (`Kafka`, `PostgreSQL`, `Spark`, `Airflow`, `Superset`, `Grafana`, `Mlflow`, `Qdrant`) and `diagrams.generic.storage.Storage` for MinIO/Iceberg-style storage blocks. Verification: `python3 graphs/diagram.py` passed and generated `graphs/dtlver3_redraw.png`. The original sample output `graphs/web_service.png` remains present as the library example artifact.
