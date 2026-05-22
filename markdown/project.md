# FoxAI Project Memory

## Purpose

Long-form project context for future sessions — architecture, active systems, decisions, progress.
Not a rule file. Rules live in the separate rules.md.

---

## 1. Current Active Work

### Primary focus

- Platform packaging is now the active top priority.
- Current product direction:
  - create one customer-facing installer file only
  - terminal-based interactive flow
  - NameNode setup runs first, then DataNode setup runs automatically
  - exact package/runtime versions must stay pinned to the current tested setup scripts
  - optional login/config values should support blank input falling back to defaults
- Current implementation checkpoint:
  - unified installer entrypoint now exists at `scripts/foxai_installer.sh`
  - first native Go installer source now exists at `scripts/foxai_installer.go`
  - premise-specific notes now exist at `scripts/foxai_installer_premise_notes.md`
  - source-of-truth setup logic remains `scripts/setup_namenode_v5.sh` and `scripts/setup_datanode.sh`
- Current packaging boundary:
  - include platform installation, configuration, and cluster bootstrap
  - exclude FoxAI DAGs, FoxAI job scripts, customer DAGs, and customer job scripts
- Immediate goal: harden the unified installer against the existing tested manual flow, then define the customer script/template path on top of the installed platform

### Secondary active track

- Customer script/template or extension path after platform installation
- Licensing is intentionally sequenced after packaging and customer-path work
- Combined-domain medallion pipeline safe-hardening pass is temporarily on hold
- HDOS hospital-facing sample refactor is deployed and user-confirmed working on `tb_patientrecord` with a deterministic 1000-row development slice
- A separate local `hdos_widget` DAG workspace now exists for the executive-dashboard widgets without disturbing the validated `hdos_sample` DAG

### Open items

- Validate `scripts/foxai_installer.sh` in a real environment against the current tested two-script flow
- Audit and harden `scripts/foxai_installer.go` against the tested shell flow before building the customer-facing Linux binary
- Confirm the installer does not lose any required step from:
  - `scripts/setup_namenode_v5.sh`
  - `scripts/setup_datanode.sh`
- Define the customer-facing template/authoring path after install:
  - where customer DAGs/scripts live
  - what example/template, if any, should be included
  - what remains customer-owned versus platform-owned
- Keep premise-specific logic explicit for future customer rollout:
  - Kakao apt mirror override
  - fixed install paths and hostnames
  - pinned Java/Hadoop/Spark versions
- Licensing phase remains later and generic:
  - simple license check / permission gate
  - no UI in the current phase
- PostgreSQL serving-layer integration via Spark / Thrift / JDBC
- Validate targeted replay on the deployed `hdos_widget` DAG:
  - source extracts: `tb_patientrecord`, `tb_invoice`, `tb_treatment`, `tb_bed`, `tb_department`, `tb_phacdodieutri`, `tb_phacdodieutri_phieudieutri`
  - Gold tasks branch from `bronze_to_silver` as replayable same-level Airflow tasks
  - current widget outputs: encounter activity, finance classification, inpatient summary, bed occupancy, clinical pathway
  - current observed runtime behavior: Gold tasks executed sequentially in the environment even though the DAG shape allows sibling replay
- Reproducible realtime streaming baseline metrics

---

## 2. Infrastructure

| Component     | Detail                                          |
| ------------- | ----------------------------------------------- |
| Cluster       | 1 NameNode + 5 DataNodes on `192.168.100.x`     |
| Storage       | MinIO `http://192.168.100.66:9001`              |
| Orchestration | Airflow `http://192.168.100.66:8081`            |
| Processing    | Spark on YARN                                   |
| Table format  | Iceberg + HadoopCatalog (`type=hadoop`)         |
| HDFS role     | Cluster/runtime support only — not lake storage |

### Active ports

| Service              | Port  | Status               |
| -------------------- | ----- | -------------------- |
| HDFS NameNode        | 9000  | Internal             |
| YARN ResourceManager | 8088  | Web UI               |
| MinIO API            | 9001  | Web UI               |
| Airflow              | 8081  | Web UI               |
| Spark Thrift Server  | 10000 | Optional             |
| Apache Superset      | 8084  | Optional             |
| Trino                | 8083  | Installed, sidelined |

### External source systems

- HDOS Windows source machine:
  - host: `192.168.100.78`
  - remote desktop endpoint: `160.30.252.78:33896`
  - Windows user: `ehc`
  - Windows password: `R00T@!@#`
- HDOS PostgreSQL now verified reachable from the cluster:
  - host: `192.168.100.78`
  - port: `5630`
  - database: `test05052026`
  - schema in use for current hospital tables: `public`
  - user: `postgres`
  - password: currently blank / trust-auth path in use for internal testing
  - current access note:
    - Windows PostgreSQL service listens on `0.0.0.0:5630`
    - `pg_hba.conf` was widened to allow cluster subnet `192.168.100.0/24` during HDOS sample testing

---

## 3. Active Systems

### A. Combined-domain medallion pipeline

- Flow: Kafka → raw registry → bronze → silver → gold
- DAG: `/home/ubuntu/airflow/dags/dag_combined_domains.py`
- Scripts: `/home/ubuntu/daihai_script/dag_combined_domains/`
- Configs:
  - `ingest_sources_kafka_domains.json`
  - `domain_registry_v2.json`
  - `foxai_config.py`
  - `foxai_config.json`
- Registry contract: `kafka_consume_to_raw_domains.py` writes `raw_catalog.registry.raw_registry` — `bronze_from_raw_domains.py` must read the same table
- Current hardening status:
  - Fixes 4-5 pending

### B. Realtime histogram demo

- Kafka demo DAG id: `realtime_fare_amount_pipeline`
- Kafka broker: `192.168.100.66:9092`
- Topic: `realtime_fare_amount_demo`
- Consumer group: `realtime-fare-amount-demo-airflow`
- DAG file: `/home/ubuntu/airflow/dags/realtime_fare_amount_single_dag.py`
- Scripts: `/home/ubuntu/daihai_script/realtime_histogram_demo/`
- Viewer HTML: `/home/ubuntu/daihai_script/html_histograms/histogram_chat_viewer.html`
- Inbox: `inbox/rows` (single rows), `inbox/batch` (batch files)
- Airflow tasks: `consume_kafka_and_update_minio_state` → `gate_histogram_if_new_data` → `generate_histogram_snapshot`
- Snapshot output: `demo/<snapshot>/fare_amount/`
- Persistent state: `demo/realtime_fare_amount/state/`
- Kafka runtime config memory folded in here:
  - Kafka install: `/opt/confluent-7.8.0/`
  - KRaft config: `/opt/confluent-7.8.0/etc/kafka/kraft/server.properties`
  - Kafka log dir: `/tmp/kraft-combined-logs/`
  - Start script: `bin/kafka-server-start`
  - Topic script: `bin/kafka-topics`
  - Storage tool: `bin/kafka-storage`
  - Known topic: `raw_ingest_events` already created with 3 partitions

### C. Realtime RabbitMQ demo

- DAG id: `realtime_rabbitmq`
- Validation path now implemented on `realtime_rabbitmq` itself rather than the separate `realtime_validate` DAG
- Validation purpose now clarified:
  - use 5 rows mapped to 5 days
  - file mode compares one full-file run against row mode built incrementally one day at a time
  - row mode should emit 5 day-by-day charts
  - row-mode day 5 must match the file-mode final chart
- Validation checkpoint status:
  - `realtime_rabbitmq` DAG/scripts were refactored to the clarified 5-day validation behavior
  - output folders now use fixed shallow labels: `demo/file/...` and `demo/row_day1` through `demo/row_day5`
  - row mode now materializes parquet state after each accepted row
  - file mode and row mode now use separate state namespaces under `demo/realtime_rabbitmq_fare_amount/state/file/...` and `.../state/row/...`
  - user confirmed on `2026-05-20` that the DAG worked after these fixes
- RabbitMQ broker: `192.168.100.60:5672`
- Queue: `daihai_local_test_1`
- DAG file: `/home/ubuntu/airflow/dags/realtime_rabbitmq.py`
- Scripts: `/home/ubuntu/daihai_script/realtime_rabbitmq/`
- Receiver role: long-lived process outside Airflow; consumes queue, persists raw event to MinIO, triggers DAG via Airflow REST API
- Trigger behavior is intentionally automatic: once a message is received and persisted, receiver triggers the DAG without manual Airflow action
- Current trigger auth in receiver is hardcoded for now to Airflow API `http://192.168.100.66:8081/api/v1` with `admin/admin`
- Current temporary operating model: while Hoang is absent, our side manually simulates the upstream transmitter by sending file/row events through `rabbitmq_live_transmitter.py`
- Supported message types in current demo path: `file`, `row`
- Event landing prefix: `demo/realtime_rabbitmq_fare_amount/event/`
- Persistent state: `demo/realtime_rabbitmq_fare_amount/state/`
- Calculation artifact: `demo/file/fare_amount/calculation/summary.json` or `demo/row_dayN/fare_amount/calculation/summary.json`
- Chart output: `demo/file/fare_amount/inrange.png` or `demo/row_dayN/fare_amount/inrange.png`
- Current dedupe scope:
  - file-level dedupe by file event/hash
  - row-level dedupe by `event_id`
  - still requires validation against overlap/drift scenarios using a fixed 1-week taxi dataset
- Agreed next validation task:
  - compute offline batch truth for the same 1-week taxi slice
  - replay the identical data incrementally through RabbitMQ as file and/or row events
  - compare final state using exact row counts, distinct row keys, and histogram bin counts
  - run explicit duplicate, overlap, and receiver-restart scenarios to identify whether current dedupe is only transport-level or also business-row safe

### D. HDOS PostgreSQL sample DAG

- DAG id: `hdos_sample`
- DAG file on namenode: `/home/ubuntu/airflow/dags/hdos_sample.py`
- Scripts on namenode: `/home/ubuntu/daihai_script/hdos_sample/`
- Purpose:
  - prove PostgreSQL -> Spark JDBC -> MinIO/Iceberg -> Superset flow end to end
- Current verified sample source:
  - `public.tb_nhanvienlog`
- Current Iceberg outputs:
  - raw: `raw_catalog.hdos_sample.tb_nhanvienlog_raw`
  - bronze: `bronze_catalog.hdos_sample.tb_nhanvienlog_bronze`
  - silver: `silver_catalog.hdos_sample.tb_nhanvienlog_silver`
  - gold: `gold_catalog.hdos_sample.tb_nhanvienlog_daily_domain_summary`
- Current gold meaning:
  - daily login activity summary by `login_date`, `domain`, and `softversion`
  - metrics:
    - `login_count`
    - `distinct_employee_count`
    - `distinct_computer_count`
    - `distinct_ip_count`
- Runtime outcome on `2026-05-20`:
  - DAG deployed to namenode
  - Airflow registered the DAG and task graph
  - `postgres_to_raw` succeeded after widening PostgreSQL `pg_hba.conf` from only namenode to the full cluster subnet
  - downstream DAG execution worked and the user confirmed charting/query flow in Superset
- Confirmed hospital-grade source tables for the next HDOS iteration:
  - `tb_patientrecord`
  - `tb_servicedata`
  - `tb_invoice`
  - `tb_treatment`
  - `tb_nhanvien`
  - `tb_bed`
- Local findings note:
  - `dags/hdos_sample/HDOS_SOURCE_FINDINGS.md`

---

## 4. Key Local Files

### Frontend

- `html/histogram_chat_viewer.html`
- `html/airflow_monitor.html` + `airflow_monitor.css`
- `html/_dark_mode.css`

### Realtime demo scripts

- `realtime_histogram_demo/realtime_fare_amount_single_dag.py`
- `realtime_histogram_demo/realtime_fare_amount_kafka_consume_and_update_v2.py`
- `realtime_histogram_demo/realtime_fare_amount_histogram_job.py`
- `realtime_histogram_demo/realtime_fare_amount_inbox_poller.py`
- `realtime_histogram_demo/reset_realtime_fare_amount_demo.sh`
- `realtime_rabbitmq/realtime_rabbitmq.py`
- `realtime_rabbitmq/realtime_fare_amount_rabbitmq_ingest_event.py`
- `realtime_rabbitmq/realtime_fare_amount_rabbitmq_calculation_job.py`
- `realtime_rabbitmq/realtime_fare_amount_histogram_job.py`
- `realtime_rabbitmq/rabbitmq_live_receiver.py`
- `realtime_rabbitmq/rabbitmq_live_transmitter.py`

### Platform / legacy

- `dag/` — legacy taxi pipeline
- `dag_combined_domains/` — combined-domain pipeline
- `silver_histograms_dag/` / `silver_sample_histogram/` — histogram jobs

---

## 5. Last Updated

- `realtime_validate` file-mode hardening and deployment completed:
  - split file vs row state/output paths instead of one shared state
  - removed parquet decode/materialization from file ingest
  - changed file calculation to Spark-based `spark-submit` execution
  - added `comparison.json` artifact for validation output
  - added duplicate-rerun recovery when downstream artifacts are missing
  - reduced noisy task log output to compact summaries
  - fixed chart hangs caused by excessive raw-max bin/tick generation
  - changed `fare_amount` histogram bounds to use a two-stage IQR style upper bound
- `realtime_validate` row-file transport/state refactor completed and deployed:
  - `/rowfile` now stages MinIO chunk files + manifest and sends one `row_batch` event instead of one RabbitMQ message per row
  - receiver accepts `row_batch`
  - row ingest writes chunked row-state under `demo/realtime_validate_fare_amount/state/row/...`
  - full remote replay completed for `3328747` rows into `666` chunks
  - current blocker after this deploy is row-mode quantile instability in calculation, not transport correctness

- Kafka-first realtime histogram demo — full flow built and verified
- Poller support for `.json`, `.csv`, `.parquet`, `.xml`
- Fixed bootstrap vs consumer-group offset behavior
- Retry-safe histogram gate logic
- Flattened demo output path to `demo/<snapshot>/fare_amount/...`
- Viewer path logic synced with flattened structure
- Reset helper for MinIO state + Kafka replay
- RabbitMQ realtime demo deployed and working on namenode:
  - queue `daihai_local_test_1`
  - receiver persists raw events to MinIO then auto-triggers Airflow DAG
  - DAG split into ingest → gate → calculation → chart
  - receiver/transmitter + DAG/scripts pushed and remote hashes verified
  - verified trigger auth currently uses hardcoded Airflow API base `http://192.168.100.66:8081/api/v1` and `admin/admin`
  - confirmed receiver restart replays unacked messages after failed trigger, which is expected RabbitMQ behavior
  - current ops mode is temporary manual transmitter simulation while Hoang is absent
- `realtime_rabbitmq` 5-day validation refactor deployed on namenode:
  - local/remote `sha256` matched for DAG + ingest/calc/chart scripts
  - remote `pyarrow` availability confirmed (`23.0.1`)
  - Airflow CLI check confirmed DAG presence: `realtime_rabbitmq`
  - local/remote inbox samples were replaced with one 5-row batch file and five row-day JSON files only
  - row-day input `event_id` values were separated from file-mode row IDs to avoid duplicate suppression across modes
  - file mode and row mode state were split to stop row charts from appending on top of file-mode state
  - user confirmed on `2026-05-20` that the deployed DAG worked after these fixes
- Fixed registry mismatch in combined-domain bronze read path
- Airflow monitor live refresh behavior verified working after refactor
- Combined-domain safe hardening pass, deployed to namenode:
  - shared config layer added via `dag_combined_domains/foxai_config.py` + `dag_combined_domains/foxai_config.json`
  - combined-domain DAG and jobs updated to consume shared config
  - structured logging applied across combined-domain Python files
  - remaining DAG config cleanup completed
  - remote deployment verified by matching local/remote `sha256`
- Operator documentation + doc consolidation completed:
  - Vietnamese operator manual created at `docs/Tai_lieu_huong_dan_van_hanh_Data_Platform.docx`
  - document files consolidated into `docs/`
  - operator-guide generator preserved at `docs/generate_operator_guide_vi.js`
- Packaging-first product direction clarified and started:
  - packaging scope narrowed to installation, configuration, and setup only
  - customer DAGs and customer job scripts are outside the base package
  - one unified installer entrypoint created at `scripts/foxai_installer.sh`
  - premise-specific rollout notes recorded at `scripts/foxai_installer_premise_notes.md`
- HDOS PostgreSQL sample DAG completed and validated:
  - namenode can reach HDOS PostgreSQL at `192.168.100.78:5630`
  - Airflow DAG `hdos_sample` deployed under `/home/ubuntu/airflow/dags/hdos_sample.py`
  - runtime scripts/config deployed under `/home/ubuntu/daihai_script/hdos_sample/`
  - Spark JDBC package resolution for PostgreSQL was verified at runtime
  - sample medallion flow to Gold worked and Superset queries/charts were confirmed by the user

---

## 6. Near-term / Backlog

### Near-term

- Validate the unified installer `scripts/foxai_installer.sh` against the current tested deployment flow
- Harden the installer for production use while keeping exact pinned versions from the source scripts
- Define the customer script/template path after installation
- Stabilize Kafka KRaft-only startup runbook on namenode
- Taxi 2025 forecasting (train on 2020–2024, evaluate vs actuals)
- Combined-domain fix 4 and fix 5 remain available to resume later if reprioritized

### Longer-term

- Licensing system (integrate with other team's API)
- Binary wrapping/distribution of the unified installer, if a stricter compiled artifact is required after shell-installer validation
- Customer extension/template SDK
- Low-latency realtime processing track

---

## 7. Key Decisions & Constraints

- Hive Metastore dropped — HadoopCatalog is the current path
- Kafka is streaming infrastructure, not one-time migration tooling
- Performance is I/O/network bound — config tuning alone has limited impact
- Legacy taxi pipeline stays stable while newer pipelines expand alongside it
- Airflow: Python DAGs only
- For `html/airflow_monitor.html` task actions, use actual Airflow REST endpoints:
  - `POST /api/v1/dags/{dag_id}/clearTaskInstances`
  - `POST /api/v1/dags/{dag_id}/updateTaskInstancesState`
- Edits stay targeted — no broad overwrites, no parallel copies of active scripts

---

## 8. Resume Checklist

1. Which system is active? Combined-domain pipeline or realtime histogram demo?
2. For realtime demo: confirm Kafka topic, consumer group, DAG id, and target runtime files
3. Read relevant active code before editing
4. After namenode push, verify remote file content
5. Update this file only when architecture or progress meaningfully changes
6. Keep only active/in-progress work in task/active sections; move completed work into `## 5. Last Updated`
