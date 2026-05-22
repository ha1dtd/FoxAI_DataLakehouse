import argparse
import logging
from pathlib import Path
import sys

from pyspark.sql import DataFrame, SparkSession
from pyspark.sql.functions import coalesce, col, count, countDistinct, current_date, lit, round as spark_round, sum, when

SCRIPT_DIR = Path(__file__).resolve().parent
if str(SCRIPT_DIR) not in sys.path:
    sys.path.insert(0, str(SCRIPT_DIR))

from hdos_widget_config import GOLD_NAMESPACE, SILVER_NAMESPACE

logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
logger = logging.getLogger("hdos_widget_silver_to_gold")

TOPICS = {
    "encounter_activity",
    "finance_classification",
    "inpatient_summary",
    "bed_occupancy",
    "clinical_pathway",
}


def silver_table(table_name: str) -> str:
    return f"silver_catalog.{SILVER_NAMESPACE}.{table_name}_silver"


def gold_table(table_name: str) -> str:
    return f"gold_catalog.{GOLD_NAMESPACE}.{table_name}"


def write_gold_table(df: DataFrame, target_fqn: str) -> None:
    logger.info("GOLD_TARGET=%s", target_fqn)
    logger.info("GOLD_ROW_COUNT=%s", df.count())
    df.writeTo(target_fqn).createOrReplace()
    logger.info("GOLD_WRITE_COMPLETE=%s", target_fqn)


def build_encounter_activity(spark: SparkSession) -> tuple[DataFrame, str]:
    patientrecord = spark.read.table(silver_table("tb_patientrecord")).filter(col("encounter_date").isNotNull())
    df = (
        patientrecord.groupBy("encounter_date", "departmentid", "roomid")
        .agg(
            count("*").alias("encounter_count"),
            countDistinct("patientid").alias("distinct_patient_count"),
            sum(when(col("is_inpatient"), lit(1)).otherwise(lit(0))).alias("inpatient_encounter_count"),
            sum(when(col("is_discharged"), lit(1)).otherwise(lit(0))).alias("discharged_encounter_count"),
            sum(when(col("has_insurance_code"), lit(1)).otherwise(lit(0))).alias("insured_encounter_count"),
        )
        .orderBy(col("encounter_date").desc(), col("encounter_count").desc())
    )
    return df, gold_table("dashboard_daily_encounter_activity")


def build_finance_classification(spark: SparkSession) -> tuple[DataFrame, str]:
    invoice = spark.read.table(silver_table("tb_invoice")).filter(col("invoice_date").isNotNull())
    df = (
        invoice.groupBy(
            "invoice_date",
            "departmentid",
            "roomid",
            "finance_bucket",
            "dm_invoice_typeid",
            "dm_invoice_hinhthucid",
            "dm_invoice_typedetailid",
            "dm_nhomthanhtoanid",
            "dm_vienphi_nguonthanhtoanid",
        )
        .agg(
            count("*").alias("invoice_count"),
            countDistinct("patientrecordid").alias("distinct_encounter_count"),
            sum(coalesce(col("invoice_amount"), lit(0.0))).alias("total_invoice_amount"),
            sum(coalesce(col("discount_amount"), lit(0.0))).alias("total_discount_amount"),
        )
        .orderBy(col("invoice_date").desc(), col("total_invoice_amount").desc())
    )
    return df, gold_table("dashboard_daily_finance_classification")


def build_inpatient_summary(spark: SparkSession) -> tuple[DataFrame, str]:
    patientrecord = spark.read.table(silver_table("tb_patientrecord")).filter(col("encounter_date").isNotNull())
    df = (
        patientrecord.groupBy(
            "encounter_date",
            "departmentid",
            "dm_patientrecordtypeid",
            "dm_patientrecordstatusid",
            "dm_medicalrecordstatusid_out",
        )
        .agg(
            count("*").alias("encounter_count"),
            countDistinct("patientid").alias("distinct_patient_count"),
            sum(when(col("is_inpatient"), lit(1)).otherwise(lit(0))).alias("inpatient_encounter_count"),
            sum(when(col("is_discharged"), lit(1)).otherwise(lit(0))).alias("discharged_encounter_count"),
        )
        .orderBy(col("encounter_date").desc(), col("encounter_count").desc())
    )
    return df, gold_table("dashboard_daily_inpatient_summary")


def build_bed_occupancy(spark: SparkSession) -> tuple[DataFrame, str]:
    bed = spark.read.table(silver_table("tb_bed")).alias("bed")
    department = spark.read.table(silver_table("tb_department")).alias("dept")

    grouped = (
        bed.join(department, col("bed.departmentid") == col("dept.departmentid"), "left")
        .groupBy(
            col("bed.departmentid").alias("departmentid"),
            col("dept.departmentcode").alias("departmentcode"),
            col("dept.departmentname").alias("departmentname"),
            col("dept.sogiuongkehoach").alias("planned_bed_count"),
            col("dept.sogiuongthucke").alias("actual_bed_count"),
        )
        .agg(
            count("*").alias("configured_bed_count"),
            sum(when(col("bed.is_disabled_bed"), lit(1)).otherwise(lit(0))).alias("disabled_bed_count"),
            sum(when(col("bed.is_occupied_bed"), lit(1)).otherwise(lit(0))).alias("occupied_bed_count"),
        )
    )
    df = (
        grouped.withColumn("snapshot_date", current_date())
        .withColumn(
            "available_bed_count",
            col("configured_bed_count") - col("disabled_bed_count"),
        )
        .withColumn(
            "occupancy_ratio",
            spark_round(
                col("occupied_bed_count") / when(col("available_bed_count") == lit(0), lit(None)).otherwise(col("available_bed_count")),
                4,
            ),
        )
        .select(
            "snapshot_date",
            "departmentid",
            "departmentcode",
            "departmentname",
            "planned_bed_count",
            "actual_bed_count",
            "configured_bed_count",
            "disabled_bed_count",
            "available_bed_count",
            "occupied_bed_count",
            "occupancy_ratio",
        )
        .orderBy(col("occupancy_ratio").desc_nulls_last(), col("occupied_bed_count").desc())
    )
    return df, gold_table("dashboard_department_bed_occupancy")


def build_clinical_pathway(spark: SparkSession) -> tuple[DataFrame, str]:
    pathway = spark.read.table(silver_table("tb_phacdodieutri")).alias("p")
    sheet = spark.read.table(silver_table("tb_phacdodieutri_phieudieutri")).alias("s")

    df = (
        pathway.join(sheet, col("p.phacdodieutriid") == col("s.phacdodieutriid"), "left")
        .groupBy(
            col("p.phacdodieutriid").alias("phacdodieutriid"),
            col("p.maphacdo").alias("maphacdo"),
            col("p.tenphacdo").alias("tenphacdo"),
            col("p.listmabenhicd10").alias("listmabenhicd10"),
            col("p.dm_phacdogroupid").alias("dm_phacdogroupid"),
            col("p.songaydieutri").alias("configured_treatment_days"),
        )
        .agg(
            count("s.phacdodieutri_phieudieutriid").alias("pathway_sheet_count"),
            countDistinct("s.ngaydieutriphacdo").alias("distinct_protocol_day_count"),
        )
        .orderBy(col("pathway_sheet_count").desc(), col("tenphacdo").asc_nulls_last())
    )
    return df, gold_table("dashboard_clinical_pathway_summary")


def build_topic(spark: SparkSession, topic: str) -> tuple[DataFrame, str]:
    if topic == "encounter_activity":
        return build_encounter_activity(spark)
    if topic == "finance_classification":
        return build_finance_classification(spark)
    if topic == "inpatient_summary":
        return build_inpatient_summary(spark)
    if topic == "bed_occupancy":
        return build_bed_occupancy(spark)
    if topic == "clinical_pathway":
        return build_clinical_pathway(spark)
    raise ValueError(f"Unsupported gold topic: {topic}")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Build one hdos_widget Gold topic")
    parser.add_argument("--topic", required=True, choices=sorted(TOPICS))
    return parser.parse_args()


def main() -> None:
    args = parse_args()
    spark = SparkSession.builder.appName(f"hdos_widget_gold_{args.topic}").getOrCreate()
    spark.sparkContext.setLogLevel("WARN")

    spark.sql(f"CREATE NAMESPACE IF NOT EXISTS gold_catalog.{GOLD_NAMESPACE}")
    logger.info("GOLD_TOPIC=%s", args.topic)
    df, target_fqn = build_topic(spark, args.topic)
    write_gold_table(df, target_fqn)
    spark.stop()


if __name__ == "__main__":
    main()
