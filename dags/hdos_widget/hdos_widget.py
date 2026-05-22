from airflow import DAG  # type: ignore
from airflow.operators.bash import BashOperator  # type: ignore
from datetime import datetime
import sys

SCRIPT_BASE = "/home/ubuntu/daihai_script/hdos_widget"
if SCRIPT_BASE not in sys.path:
    sys.path.insert(0, SCRIPT_BASE)

from hdos_widget_config import (
    BRONZE_WAREHOUSE,
    GOLD_WAREHOUSE,
    MINIO_ACCESS_KEY,
    MINIO_ENDPOINT,
    MINIO_SECRET_KEY,
    RAW_WAREHOUSE,
    SCRIPT_BASE,
    SILVER_WAREHOUSE,
    SPARK_SUBMIT_BIN,
)

S3A_PATH_STYLE_ACCESS = "true"
S3A_IMPL = "org.apache.hadoop.fs.s3a.S3AFileSystem"
S3A_SSL_ENABLED = "false"
S3A_CONF = (
    f"--conf spark.hadoop.fs.s3a.endpoint={MINIO_ENDPOINT} "
    f"--conf spark.hadoop.fs.s3a.access.key={MINIO_ACCESS_KEY} "
    f"--conf spark.hadoop.fs.s3a.secret.key={MINIO_SECRET_KEY} "
    f"--conf spark.hadoop.fs.s3a.path.style.access={S3A_PATH_STYLE_ACCESS} "
    f"--conf spark.hadoop.fs.s3a.impl={S3A_IMPL} "
    f"--conf spark.hadoop.fs.s3a.connection.ssl.enabled={S3A_SSL_ENABLED}"
)

SPARK_COMMON = """
        --master yarn \
        --deploy-mode client \
        --driver-memory 2G \
        --executor-memory 2G \
        --executor-cores 2 \
        --conf spark.executor.instances=4 \
        --conf spark.dynamicAllocation.enabled=false \
        --conf spark.sql.shuffle.partitions=32 \
        --conf spark.network.timeout=300s \
        --conf spark.sql.execution.arrow.enabled=false \
        --conf spark.sql.execution.arrow.pyspark.enabled=false \
        --conf spark.sql.iceberg.vectorization.enabled=false \
        --conf spark.executor.memoryOverhead=512m \
        --conf spark.memory.offHeap.enabled=false \
        --conf spark.sql.files.maxPartitionBytes=134217728 \
        --conf spark.executor.extraJavaOptions=\"-Dio.netty.transport.noNative=true -Dio.netty.handler.ssl.noOpenSsl=true -Dorg.wildfly.openssl.disable=true --add-opens=java.base/java.nio=ALL-UNNAMED --add-opens=java.base/sun.nio.ch=ALL-UNNAMED\" \
        --conf spark.driver.extraJavaOptions=\"-Dio.netty.transport.noNative=true -Dio.netty.handler.ssl.noOpenSsl=true -Dorg.wildfly.openssl.disable=true\" \
        --conf spark.sql.extensions=org.apache.iceberg.spark.extensions.IcebergSparkSessionExtensions \
        --conf spark.sql.adaptive.enabled=true \
        --conf spark.sql.adaptive.coalescePartitions.enabled=true \
        --conf spark.sql.adaptive.skewJoin.enabled=true \
        --conf spark.sql.adaptive.advisoryPartitionSizeInBytes=256MB
""".strip()

POSTGRES_JDBC_PACKAGE = "org.postgresql:postgresql:42.7.3"
ICEBERG_PACKAGES = "org.apache.iceberg:iceberg-spark-runtime-3.5_2.12:1.4.3,org.apache.hadoop:hadoop-aws:3.3.4,com.amazonaws:aws-java-sdk-bundle:1.12.262"
GOLD_TOPICS = {
    "gold_encounter_activity": "encounter_activity",
    "gold_finance_classification": "finance_classification",
    "gold_inpatient_summary": "inpatient_summary",
    "gold_bed_occupancy": "bed_occupancy",
    "gold_clinical_pathway": "clinical_pathway",
}

default_args = {
    "owner": "foxai",
    "start_date": datetime(2024, 1, 1),
    "retries": 1,
    "depends_on_past": False,
}

RAW_SCRIPT = f"{SCRIPT_BASE}/postgres_to_raw.py"
BRONZE_SCRIPT = f"{SCRIPT_BASE}/raw_to_bronze.py"
SILVER_SCRIPT = f"{SCRIPT_BASE}/bronze_to_silver.py"
GOLD_SCRIPT = f"{SCRIPT_BASE}/silver_to_gold.py"
CONFIG_FILE = f"{SCRIPT_BASE}/hdos_widget_config.json"


def spark_submit_command(script_path: str, catalogs: str, extra_args: str = "") -> str:
    return f"""
        export FOXAI_CONFIG_FILE={CONFIG_FILE}
        {SPARK_SUBMIT_BIN} \
        {SPARK_COMMON} \
        --packages {ICEBERG_PACKAGES}{catalogs} \
        {S3A_CONF} \
        {script_path} {extra_args}
"""


with DAG(
    dag_id="hdos_widget",
    default_args=default_args,
    schedule_interval=None,
    catchup=False,
) as dag:
    postgres_to_raw = BashOperator(
        task_id="postgres_to_raw",
        bash_command=spark_submit_command(
            RAW_SCRIPT,
            f",{POSTGRES_JDBC_PACKAGE} "
            f"--conf spark.sql.catalog.raw_catalog=org.apache.iceberg.spark.SparkCatalog "
            f"--conf spark.sql.catalog.raw_catalog.type=hadoop "
            f"--conf spark.sql.catalog.raw_catalog.warehouse={RAW_WAREHOUSE}",
        ),
    )

    raw_to_bronze = BashOperator(
        task_id="raw_to_bronze",
        bash_command=spark_submit_command(
            BRONZE_SCRIPT,
            f" --conf spark.sql.catalog.raw_catalog=org.apache.iceberg.spark.SparkCatalog "
            f"--conf spark.sql.catalog.raw_catalog.type=hadoop "
            f"--conf spark.sql.catalog.raw_catalog.warehouse={RAW_WAREHOUSE} "
            f"--conf spark.sql.catalog.bronze_catalog=org.apache.iceberg.spark.SparkCatalog "
            f"--conf spark.sql.catalog.bronze_catalog.type=hadoop "
            f"--conf spark.sql.catalog.bronze_catalog.warehouse={BRONZE_WAREHOUSE}",
        ),
    )

    bronze_to_silver = BashOperator(
        task_id="bronze_to_silver",
        bash_command=spark_submit_command(
            SILVER_SCRIPT,
            f" --conf spark.sql.catalog.bronze_catalog=org.apache.iceberg.spark.SparkCatalog "
            f"--conf spark.sql.catalog.bronze_catalog.type=hadoop "
            f"--conf spark.sql.catalog.bronze_catalog.warehouse={BRONZE_WAREHOUSE} "
            f"--conf spark.sql.catalog.silver_catalog=org.apache.iceberg.spark.SparkCatalog "
            f"--conf spark.sql.catalog.silver_catalog.type=hadoop "
            f"--conf spark.sql.catalog.silver_catalog.warehouse={SILVER_WAREHOUSE}",
        ),
    )

    gold_tasks = []
    for task_id, topic in GOLD_TOPICS.items():
        gold_task = BashOperator(
            task_id=task_id,
            bash_command=spark_submit_command(
                GOLD_SCRIPT,
                f" --conf spark.sql.catalog.silver_catalog=org.apache.iceberg.spark.SparkCatalog "
                f"--conf spark.sql.catalog.silver_catalog.type=hadoop "
                f"--conf spark.sql.catalog.silver_catalog.warehouse={SILVER_WAREHOUSE} "
                f"--conf spark.sql.catalog.gold_catalog=org.apache.iceberg.spark.SparkCatalog "
                f"--conf spark.sql.catalog.gold_catalog.type=hadoop "
                f"--conf spark.sql.catalog.gold_catalog.warehouse={GOLD_WAREHOUSE}",
                f"--topic {topic}",
            ),
        )
        gold_tasks.append(gold_task)

    postgres_to_raw >> raw_to_bronze >> bronze_to_silver >> gold_tasks
