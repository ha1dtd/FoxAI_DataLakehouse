import logging
from pathlib import Path
import sys

from pyspark.sql import SparkSession
from pyspark.sql.functions import current_timestamp, lit

SCRIPT_DIR = Path(__file__).resolve().parent
if str(SCRIPT_DIR) not in sys.path:
    sys.path.insert(0, str(SCRIPT_DIR))

from hdos_sample_config import (
    PG_DATABASE,
    PG_HOST,
    PG_PASSWORD,
    PG_PORT,
    PG_SOURCE_PRIMARY_KEY,
    PG_SOURCE_QUERY,
    PG_SOURCE_SCHEMA,
    PG_SOURCE_TABLE,
    PG_USER,
    RAW_NAMESPACE,
)

logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
logger = logging.getLogger("hdos_sample_postgres_to_raw")


def main() -> None:
    spark = SparkSession.builder.appName("hdos_sample_postgres_to_raw").getOrCreate()
    spark.sparkContext.setLogLevel("WARN")

    source_fqn = f"{PG_SOURCE_SCHEMA}.{PG_SOURCE_TABLE}"
    raw_fqn = f"raw_catalog.{RAW_NAMESPACE}.{PG_SOURCE_TABLE}_raw"
    jdbc_url = f"jdbc:postgresql://{PG_HOST}:{PG_PORT}/{PG_DATABASE}"

    spark.sql(f"CREATE NAMESPACE IF NOT EXISTS raw_catalog.{RAW_NAMESPACE}")

    reader = (
        spark.read.format("jdbc")
        .option("url", jdbc_url)
        .option("user", PG_USER)
        .option("driver", "org.postgresql.Driver")
        .option("fetchsize", "1000")
    )
    if PG_PASSWORD:
        reader = reader.option("password", PG_PASSWORD)
    if PG_SOURCE_QUERY:
        reader = reader.option("query", PG_SOURCE_QUERY)
        source_label = PG_SOURCE_QUERY
        source_mode = "query"
    else:
        reader = reader.option("dbtable", source_fqn)
        source_label = source_fqn
        source_mode = "table"

    df = (
        reader.load()
        .withColumn("_source_schema", lit(PG_SOURCE_SCHEMA))
        .withColumn("_source_table", lit(PG_SOURCE_TABLE))
        .withColumn("_ingested_at", current_timestamp())
    )
    if PG_SOURCE_PRIMARY_KEY and PG_SOURCE_PRIMARY_KEY not in df.columns:
        raise ValueError(f"Missing configured primary key column in source extract: {PG_SOURCE_PRIMARY_KEY}")

    logger.info("RAW_SOURCE_MODE=%s", source_mode)
    logger.info("RAW_SOURCE=%s", source_label)
    logger.info("RAW_PRIMARY_KEY=%s", PG_SOURCE_PRIMARY_KEY or "UNSET")
    logger.info("RAW_TARGET=%s", raw_fqn)
    logger.info("RAW_COLUMN_COUNT=%s", len(df.columns))
    logger.info("RAW_ROW_COUNT=%s", df.count())

    df.writeTo(raw_fqn).createOrReplace()
    logger.info("RAW_WRITE_COMPLETE=%s", raw_fqn)
    spark.stop()


if __name__ == "__main__":
    main()
