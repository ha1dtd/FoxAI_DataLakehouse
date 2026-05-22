import logging
from pathlib import Path
import sys

from pyspark.sql import DataFrame, SparkSession
from pyspark.sql.functions import coalesce, col, lit, regexp_replace, to_date, trim, when
from pyspark.sql.types import StringType

SCRIPT_DIR = Path(__file__).resolve().parent
if str(SCRIPT_DIR) not in sys.path:
    sys.path.insert(0, str(SCRIPT_DIR))

from hdos_widget_config import BRONZE_NAMESPACE, SILVER_NAMESPACE, SOURCES, SourceConfig

logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
logger = logging.getLogger("hdos_widget_bronze_to_silver")


def clean_string(column_name: str):
    return when(trim(col(column_name)) == "", None).otherwise(trim(col(column_name)))


def build_clean_projection(base_df: DataFrame):
    projection = []
    for field in base_df.schema.fields:
        if isinstance(field.dataType, StringType):
            projection.append(clean_string(field.name).alias(field.name))
        else:
            projection.append(col(field.name))
    return projection


def add_patientrecord_fields(df: DataFrame) -> DataFrame:
    return (
        df.withColumn(
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
        .withColumn("has_insurance_code", when(col("insurancecode").isNull(), lit(False)).otherwise(lit(True)))
        .withColumn("is_bhyt_covered", when(coalesce(col("tongbhyt"), lit(0.0)) > lit(0.0), lit(True)).otherwise(lit(False)))
        .withColumn("is_inpatient", when(col("medicalrecordid_in").isNotNull() | col("admission_date").isNotNull(), lit(True)).otherwise(lit(False)))
        .withColumn("is_discharged", when(col("discharge_date").isNotNull(), lit(True)).otherwise(lit(False)))
    )


def add_invoice_fields(df: DataFrame) -> DataFrame:
    return (
        df.withColumn("invoice_date", to_date(col("invoicedate")))
        .withColumn("invoice_amount", coalesce(col("sotienphieu"), lit(0.0)))
        .withColumn("discount_amount", coalesce(col("sotienmiengiam"), lit(0.0)))
        .withColumn(
            "finance_bucket",
            when(col("dm_invoice_typeid").isNotNull(), regexp_replace(col("dm_invoice_typeid").cast("string"), "^", "invoice_type_"))
            .otherwise(lit("unknown")),
        )
    )


def add_treatment_fields(df: DataFrame) -> DataFrame:
    return (
        df.withColumn("treatment_date_only", to_date(col("treatmentdate")))
        .withColumn("checkin_date", to_date(col("checkin_time")))
        .withColumn("end_date", to_date(col("end_treatmentdate")))
        .withColumn("is_emergency_treatment", when(coalesce(col("iscapcuu"), lit(0)) == lit(1), lit(True)).otherwise(lit(False)))
        .withColumn("has_alert_text", when(col("thongtincanhbaoxn").isNull(), lit(False)).otherwise(lit(True)))
    )


def add_bed_fields(df: DataFrame) -> DataFrame:
    return (
        df.withColumn("is_disabled_bed", when(coalesce(col("beddisable"), lit(0)) == lit(1), lit(True)).otherwise(lit(False)))
        .withColumn("is_occupied_bed", when(col("listmedicalrecordid").isNull(), lit(False)).otherwise(lit(True)))
        .withColumn("occupied_record_ids", col("listmedicalrecordid"))
    )


def add_pathway_fields(source_name: str, df: DataFrame) -> DataFrame:
    if source_name == "tb_phacdodieutri":
        return df.withColumn("pathway_version_date", to_date(col("version")))
    if source_name == "tb_phacdodieutri_phieudieutri":
        return df.withColumn("pathway_sheet_date", to_date(col("version")))
    return df


def add_source_specific_fields(source_name: str, df: DataFrame) -> DataFrame:
    if source_name == "tb_patientrecord":
        return add_patientrecord_fields(df)
    if source_name == "tb_invoice":
        return add_invoice_fields(df)
    if source_name == "tb_treatment":
        return add_treatment_fields(df)
    if source_name == "tb_bed":
        return add_bed_fields(df)
    return add_pathway_fields(source_name, df)


def write_silver_source(spark: SparkSession, source: SourceConfig) -> None:
    bronze_fqn = f"bronze_catalog.{BRONZE_NAMESPACE}.{source.name}_bronze"
    silver_fqn = f"silver_catalog.{SILVER_NAMESPACE}.{source.name}_silver"

    base_df = spark.read.table(bronze_fqn)
    if source.primary_key not in base_df.columns:
        raise ValueError(f"Missing primary key column in bronze layer: {source.name}.{source.primary_key}")

    df = (
        base_df.select(*build_clean_projection(base_df))
        .filter(col(source.primary_key).isNotNull())
        .dropDuplicates([source.primary_key])
    )
    df = add_source_specific_fields(source.name, df)

    logger.info("SILVER_SOURCE=%s", bronze_fqn)
    logger.info("SILVER_TARGET=%s", silver_fqn)
    logger.info("SILVER_PRIMARY_KEY=%s", source.primary_key)
    logger.info("SILVER_COLUMN_COUNT=%s", len(df.columns))
    logger.info("SILVER_ROW_COUNT=%s", df.count())

    df.writeTo(silver_fqn).createOrReplace()
    logger.info("SILVER_WRITE_COMPLETE=%s", silver_fqn)


def main() -> None:
    spark = SparkSession.builder.appName("hdos_widget_bronze_to_silver").getOrCreate()
    spark.sparkContext.setLogLevel("WARN")

    spark.sql(f"CREATE NAMESPACE IF NOT EXISTS silver_catalog.{SILVER_NAMESPACE}")

    logger.info("SILVER_SOURCE_COUNT=%s", len(SOURCES))
    for source in SOURCES:
        write_silver_source(spark, source)

    spark.stop()


if __name__ == "__main__":
    main()
