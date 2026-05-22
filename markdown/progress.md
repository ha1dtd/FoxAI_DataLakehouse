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

**Current Phase:** Resumed on 2026-05-21. A first native Linux Go installer source now exists at `scripts/foxai_installer.go` while `scripts/foxai_installer.sh` remains the behavioral reference path. The Go version mirrors the current combined NameNode/DataNode flow at a high level and adds an end-of-run hardware collection prompt for Spark recommendation output.

**Next Exact Step:** Exercise and harden `scripts/foxai_installer.sh` against the current tested deployment flow:
1. Audit `scripts/foxai_installer.go` step-by-step against `scripts/setup_namenode_v5.sh` and `scripts/setup_datanode.sh`
2. Close any remaining parity gaps before treating the Go path as the customer-facing installer
3. Validate the new end-of-run hardware prompt flow:
   - option 1: auto probe hardware
   - option 2: manual entry
   - print recommended Spark defaults only, without editing DAG/job configs
4. Later, build a stripped Linux binary from the Go source for customer delivery after parity and runtime validation are complete

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
  - status: new | verified: local compile
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

- `scripts/foxai_installer_premise_notes.md`
  - status: present | verified: local
  - small note documenting premise-specific logic and pinned versions inherited from the source scripts

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
- Current installer behavior:
  - one combined terminal prompt flow
  - exact pinned Hadoop/Spark/Java versions from the current setup scripts
  - current MinIO defaults with blank-input fallback
  - optional Kakao mirror override kept explicit as a premise-specific choice
  - local NameNode setup followed by automatic remote DataNode setup
- Current Go installer direction:
  - customer-facing path is intended to become a single Linux binary so customers do not receive readable shell source
  - `foxai_installer.go` currently uses local/remote command orchestration to mirror the tested shell behavior
  - after cluster setup completes, it prompts for hardware collection mode and prints recommended Spark settings
- The old plan-only prototype file was removed so `scripts/foxai_installer.sh` is the active single truth file for this packaging task.

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
- This task is temporarily paused while focus shifts to PostgreSQL pipeline integration questions.

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
