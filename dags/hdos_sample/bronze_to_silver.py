import logging
from pathlib import Path
import sys

from pyspark.sql import SparkSession
from pyspark.sql.functions import coalesce, col, lit, to_date, trim, when
from pyspark.sql.types import StringType

SCRIPT_DIR = Path(__file__).resolve().parent
if str(SCRIPT_DIR) not in sys.path:
    sys.path.insert(0, str(SCRIPT_DIR))

from hdos_sample_config import BRONZE_NAMESPACE, PG_SOURCE_PRIMARY_KEY, PG_SOURCE_TABLE, SILVER_NAMESPACE

logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
logger = logging.getLogger("hdos_sample_bronze_to_silver")


def clean_string(column_name: str):
    return when(trim(col(column_name)) == "", None).otherwise(trim(col(column_name)))


def build_clean_projection(base_df):
    projection = []
    for field in base_df.schema.fields:
        if isinstance(field.dataType, StringType):
            projection.append(clean_string(field.name).alias(field.name))
        else:
            projection.append(col(field.name))
    return projection


def main() -> None:
    spark = SparkSession.builder.appName("hdos_sample_bronze_to_silver").getOrCreate()
    spark.sparkContext.setLogLevel("WARN")

    bronze_fqn = f"bronze_catalog.{BRONZE_NAMESPACE}.{PG_SOURCE_TABLE}_bronze"
    silver_fqn = f"silver_catalog.{SILVER_NAMESPACE}.{PG_SOURCE_TABLE}_silver"

    spark.sql(f"CREATE NAMESPACE IF NOT EXISTS silver_catalog.{SILVER_NAMESPACE}")

    base_df = spark.read.table(bronze_fqn)
    if PG_SOURCE_PRIMARY_KEY and PG_SOURCE_PRIMARY_KEY not in base_df.columns:
        raise ValueError(f"Missing configured primary key column in bronze layer: {PG_SOURCE_PRIMARY_KEY}")

    df = (
        base_df.select(*build_clean_projection(base_df))
        .filter(col(PG_SOURCE_PRIMARY_KEY).isNotNull())
        .dropDuplicates([PG_SOURCE_PRIMARY_KEY])
        .withColumn(
            "encounter_date",
            coalesce(
                to_date(col("patientrecorddate")),
                to_date(col("receptiondate")),
                to_date(col("medicalrecorddate_in")),
                to_date(col("medicalrecorddate_out")),
            ),
        )
        .withColumn("reception_date", to_date(col("receptiondate")))
        .withColumn("admission_date", to_date(col("medicalrecorddate_in")))
        .withColumn("discharge_date", to_date(col("medicalrecorddate_out")))
        .withColumn("insurance_start_date", to_date(col("fromdate")))
        .withColumn("insurance_end_date", to_date(col("todate")))
        .withColumn(
            "primary_diagnosis_icd10",
            coalesce(
                col("chandoan_kb_main_icd10"),
                col("chandoan_in_icd10"),
                col("chandoan_out_main_icd10"),
            ),
        )
        .withColumn(
            "secondary_diagnosis_icd10",
            coalesce(
                col("chandoan_kb_ex_icd10"),
                col("chandoan_in_icd10_kemtheo"),
                col("chandoan_out_ex_icd10"),
            ),
        )
        .withColumn(
            "has_insurance_code",
            when(col("insurancecode").isNull(), lit(False)).otherwise(lit(True)),
        )
        .withColumn(
            "is_bhyt_covered",
            when(coalesce(col("tongbhyt"), lit(0.0)) > lit(0.0), lit(True)).otherwise(lit(False)),
        )
    )

    logger.info("SILVER_SOURCE=%s", bronze_fqn)
    logger.info("SILVER_TARGET=%s", silver_fqn)
    logger.info("SILVER_PRIMARY_KEY=%s", PG_SOURCE_PRIMARY_KEY or "UNSET")
    logger.info("SILVER_COLUMN_COUNT=%s", len(df.columns))
    logger.info("SILVER_ROW_COUNT=%s", df.count())

    df.writeTo(silver_fqn).createOrReplace()
    logger.info("SILVER_WRITE_COMPLETE=%s", silver_fqn)
    spark.stop()


if __name__ == "__main__":
    main()
