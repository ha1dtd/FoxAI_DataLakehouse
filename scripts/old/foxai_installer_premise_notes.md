# FoxAI Installer Premise Notes

This note highlights assumptions inherited directly from the current tested source scripts:

- Source-of-truth scripts:
  - `scripts/setup_namenode_v5.sh`
  - `scripts/setup_datanode.sh`
- Unified installer:
  - `scripts/foxai_installer.sh`

## Pinned Versions

These are intentionally fixed to the same versions used in the current tested scripts:

- Hadoop: `3.3.6`
- Spark: `3.5.8` via `spark-3.5.8-bin-hadoop3`
- Java 11 package: `temurin-11-jdk`

Do not loosen these versions in the installer unless the source scripts are intentionally upgraded and revalidated.

## Premise-Specific Logic

The current scripts include infrastructure choices that may be valid for the current on-prem environment but not for every customer:

- Kakao Ubuntu mirror override:
  - `http://archive.ubuntu.com/ubuntu` -> `http://mirror.kakao.com/ubuntu`
  - `http://security.ubuntu.com/ubuntu` -> `http://mirror.kakao.com/ubuntu`
- Fixed service/resource settings in Hadoop/YARN config:
  - HDFS port `9000`
  - YARN memory `13312`
  - YARN vcores `14`
- Fixed install locations:
  - Hadoop: `/home/<user>/hadoop`
  - Spark: `/opt/spark`
  - Java 11 home: `/usr/lib/jvm/temurin-11-jdk-amd64`
- Fixed hostname pattern:
  - `namenode`
  - `datanode1`, `datanode2`, ...

These should remain explicit and easy to override later rather than hidden in one opaque installer path.

## Current Defaults Collected By The Installer

The unified installer currently preserves the same defaults already present in the NameNode source script:

- MinIO endpoint: `192.168.100.66:9001`
- MinIO access key: `admin`
- MinIO secret key: `12345678`

Blank input keeps these defaults.

## Important Current Limitation

The current source scripts prompt for MinIO values, but the tested shell logic does not yet apply them elsewhere in the install flow. The unified installer preserves that current behavior so it stays aligned with the source scripts.

## Customer Packaging Boundary

Included in the installer/package:

- platform installation
- platform configuration
- cluster bootstrap
- pinned runtime prerequisites

Excluded from the installer/package:

- FoxAI demo DAGs
- FoxAI job scripts
- customer DAGs
- customer job scripts
- customer business logic
- licensing implementation
