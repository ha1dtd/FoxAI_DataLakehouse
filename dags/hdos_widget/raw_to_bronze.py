import logging
from pathlib import Path
import sys

from pyspark.sql import SparkSession
from pyspark.sql.functions import col

SCRIPT_DIR = Path(__file__).resolve().parent
if str(SCRIPT_DIR) not in sys.path:
    sys.path.insert(0, str(SCRIPT_DIR))

from hdos_widget_config import BRONZE_NAMESPACE, RAW_NAMESPACE, SOURCES, SourceConfig

logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
logger = logging.getLogger("hdos_widget_raw_to_bronze")


def write_bronze_source(spark: SparkSession, source: SourceConfig) -> None:
    raw_fqn = f"raw_catalog.{RAW_NAMESPACE}.{source.name}_raw"
    bronze_fqn = f"bronze_catalog.{BRONZE_NAMESPACE}.{source.name}_bronze"
    metadata_columns = ["_source_schema", "_source_table", "_ingested_at"]

    base_df = spark.read.table(raw_fqn)
    if source.primary_key not in base_df.columns:
        raise ValueError(f"Missing primary key column in raw layer: {source.name}.{source.primary_key}")

    source_columns = [name for name in base_df.columns if name not in metadata_columns]
    ordered_columns = source_columns + [name for name in metadata_columns if name in base_df.columns]
    df = base_df.select(*(col(name) for name in ordered_columns))

    logger.info("BRONZE_SOURCE=%s", raw_fqn)
    logger.info("BRONZE_TARGET=%s", bronze_fqn)
    logger.info("BRONZE_PRIMARY_KEY=%s", source.primary_key)
    logger.info("BRONZE_COLUMN_COUNT=%s", len(df.columns))
    logger.info("BRONZE_ROW_COUNT=%s", df.count())

    df.writeTo(bronze_fqn).createOrReplace()
    logger.info("BRONZE_WRITE_COMPLETE=%s", bronze_fqn)


def main() -> None:
    spark = SparkSession.builder.appName("hdos_widget_raw_to_bronze").getOrCreate()
    spark.sparkContext.setLogLevel("WARN")

    spark.sql(f"CREATE NAMESPACE IF NOT EXISTS bronze_catalog.{BRONZE_NAMESPACE}")

    logger.info("BRONZE_SOURCE_COUNT=%s", len(SOURCES))
    for source in SOURCES:
        write_bronze_source(spark, source)

    spark.stop()


if __name__ == "__main__":
    main()
