import logging
from pathlib import Path
import sys

from pyspark.sql import SparkSession
from pyspark.sql.functions import current_timestamp, lit

SCRIPT_DIR = Path(__file__).resolve().parent
if str(SCRIPT_DIR) not in sys.path:
    sys.path.insert(0, str(SCRIPT_DIR))

from hdos_widget_config import (
    PG_DATABASE,
    PG_HOST,
    PG_PASSWORD,
    PG_PORT,
    PG_USER,
    RAW_NAMESPACE,
    SOURCES,
    SourceConfig,
)

logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
logger = logging.getLogger("hdos_widget_postgres_to_raw")


def load_source(spark: SparkSession, jdbc_url: str, source: SourceConfig) -> None:
    raw_fqn = f"raw_catalog.{RAW_NAMESPACE}.{source.name}_raw"
    reader = (
        spark.read.format("jdbc")
        .option("url", jdbc_url)
        .option("user", PG_USER)
        .option("driver", "org.postgresql.Driver")
        .option("fetchsize", "1000")
        .option("query", source.query)
    )
    if PG_PASSWORD:
        reader = reader.option("password", PG_PASSWORD)

    df = (
        reader.load()
        .withColumn("_source_schema", lit(source.schema))
        .withColumn("_source_table", lit(source.name))
        .withColumn("_ingested_at", current_timestamp())
    )
    if source.primary_key not in df.columns:
        raise ValueError(f"Missing primary key column in source extract: {source.name}.{source.primary_key}")

    logger.info("RAW_SOURCE=%s.%s", source.schema, source.name)
    logger.info("RAW_TARGET=%s", raw_fqn)
    logger.info("RAW_PRIMARY_KEY=%s", source.primary_key)
    logger.info("RAW_COLUMN_COUNT=%s", len(df.columns))
    logger.info("RAW_ROW_COUNT=%s", df.count())

    df.writeTo(raw_fqn).createOrReplace()
    logger.info("RAW_WRITE_COMPLETE=%s", raw_fqn)


def main() -> None:
    spark = SparkSession.builder.appName("hdos_widget_postgres_to_raw").getOrCreate()
    spark.sparkContext.setLogLevel("WARN")

    jdbc_url = f"jdbc:postgresql://{PG_HOST}:{PG_PORT}/{PG_DATABASE}"
    spark.sql(f"CREATE NAMESPACE IF NOT EXISTS raw_catalog.{RAW_NAMESPACE}")

    logger.info("RAW_SOURCE_COUNT=%s", len(SOURCES))
    for source in SOURCES:
        load_source(spark, jdbc_url, source)

    spark.stop()


if __name__ == "__main__":
    main()
