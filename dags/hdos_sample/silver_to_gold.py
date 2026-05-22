import logging
from pathlib import Path
import sys

from pyspark.sql import DataFrame, SparkSession
from pyspark.sql.functions import coalesce, col, count, countDistinct, lit, sum, when

SCRIPT_DIR = Path(__file__).resolve().parent
if str(SCRIPT_DIR) not in sys.path:
    sys.path.insert(0, str(SCRIPT_DIR))

from hdos_sample_config import GOLD_NAMESPACE, PG_SOURCE_TABLE, SILVER_NAMESPACE

logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
logger = logging.getLogger("hdos_sample_silver_to_gold")


def write_gold_table(df: DataFrame, target_fqn: str) -> None:
    logger.info("GOLD_TARGET=%s", target_fqn)
    logger.info("GOLD_ROW_COUNT=%s", df.count())
    df.writeTo(target_fqn).createOrReplace()
    logger.info("GOLD_WRITE_COMPLETE=%s", target_fqn)


def main() -> None:
    spark = SparkSession.builder.appName("hdos_sample_silver_to_gold").getOrCreate()
    spark.sparkContext.setLogLevel("WARN")

    silver_fqn = f"silver_catalog.{SILVER_NAMESPACE}.{PG_SOURCE_TABLE}_silver"

    spark.sql(f"CREATE NAMESPACE IF NOT EXISTS gold_catalog.{GOLD_NAMESPACE}")

    silver_df = (
        spark.read.table(silver_fqn)
        .filter(col("encounter_date").isNotNull())
    )

    financial_fqn = f"gold_catalog.{GOLD_NAMESPACE}.{PG_SOURCE_TABLE}_daily_financial_summary"
    diagnosis_fqn = f"gold_catalog.{GOLD_NAMESPACE}.{PG_SOURCE_TABLE}_daily_diagnosis_summary"
    coverage_fqn = f"gold_catalog.{GOLD_NAMESPACE}.{PG_SOURCE_TABLE}_daily_coverage_summary"
    discharge_fqn = f"gold_catalog.{GOLD_NAMESPACE}.{PG_SOURCE_TABLE}_daily_discharge_summary"

    financial_df = (
        silver_df.groupBy(
            "encounter_date",
            "departmentid",
            "roomid",
            "dm_receptionobjectid",
            "dm_patientobjectid",
        )
        .agg(
            count("*").alias("encounter_count"),
            countDistinct("patientid").alias("distinct_patient_count"),
            sum(coalesce(col("tongchiphi"), lit(0.0))).alias("total_cost_amount"),
            sum(coalesce(col("tongbhyt"), lit(0.0))).alias("total_bhyt_amount"),
            sum(coalesce(col("tongbenhnhan"), lit(0.0))).alias("total_patient_paid_amount"),
            sum(coalesce(col("tongdathu"), lit(0.0))).alias("total_collected_amount"),
            sum(when(col("has_insurance_code"), lit(1)).otherwise(lit(0))).alias(
                "insured_encounter_count"
            ),
            sum(when(col("is_bhyt_covered"), lit(1)).otherwise(lit(0))).alias(
                "bhyt_covered_encounter_count"
            ),
        )
        .orderBy(col("encounter_date").desc(), col("encounter_count").desc())
    )

    diagnosis_df = (
        silver_df.withColumn(
            "diagnosis_icd10",
            coalesce(col("primary_diagnosis_icd10"), col("secondary_diagnosis_icd10")),
        )
        .filter(col("diagnosis_icd10").isNotNull())
        .groupBy("encounter_date", "departmentid", "diagnosis_icd10")
        .agg(
            count("*").alias("encounter_count"),
            countDistinct("patientid").alias("distinct_patient_count"),
            sum(coalesce(col("tongchiphi"), lit(0.0))).alias("total_cost_amount"),
            sum(coalesce(col("tongbhyt"), lit(0.0))).alias("total_bhyt_amount"),
        )
        .orderBy(col("encounter_date").desc(), col("encounter_count").desc())
    )

    coverage_df = (
        silver_df.groupBy(
            "encounter_date",
            "departmentid",
            "dm_patientobjectid",
            "dm_bhyt_loaiid",
            "has_insurance_code",
            "is_bhyt_covered",
        )
        .agg(
            count("*").alias("encounter_count"),
            countDistinct("patientid").alias("distinct_patient_count"),
            sum(coalesce(col("tongchiphi"), lit(0.0))).alias("total_cost_amount"),
            sum(coalesce(col("tongbhyt"), lit(0.0))).alias("total_bhyt_amount"),
            sum(coalesce(col("tongbenhnhan"), lit(0.0))).alias("total_patient_paid_amount"),
        )
        .orderBy(col("encounter_date").desc(), col("encounter_count").desc())
    )

    discharge_df = (
        silver_df.groupBy(
            "encounter_date",
            "departmentid",
            "dm_ketquadieutriid",
            "dm_hinhthucravienid",
            "dm_medicalrecordstatusid_out",
        )
        .agg(
            count("*").alias("encounter_count"),
            countDistinct("patientid").alias("distinct_patient_count"),
            sum(
                when(col("discharge_date").isNotNull(), lit(1)).otherwise(lit(0))
            ).alias("discharged_encounter_count"),
        )
        .orderBy(col("encounter_date").desc(), col("encounter_count").desc())
    )

    logger.info("GOLD_SOURCE=%s", silver_fqn)
    write_gold_table(financial_df, financial_fqn)
    write_gold_table(diagnosis_df, diagnosis_fqn)
    write_gold_table(coverage_df, coverage_fqn)
    write_gold_table(discharge_df, discharge_fqn)
    spark.stop()


if __name__ == "__main__":
    main()
