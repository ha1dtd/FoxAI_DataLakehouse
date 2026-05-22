# HDOS Source Findings

Last updated: 2026-05-21

## Purpose

This note records the HDOS PostgreSQL source findings already confirmed during the first `hdos_sample` investigation, so we do not need to rescan the database for the same baseline context.

Database connection used during verification:

- Host: `192.168.100.78`
- Port: `5630`
- Database: `test05052026`
- Schema used for hospital data review: `public`

## Confirmed Populated Tables

These tables were directly checked and confirmed to have data:

- `public.tb_patientrecord`
  - Confirmed row count: `763,887`
- `public.tb_servicedata`
  - Confirmed row count: `13,037,989`
- `public.tb_invoice`
  - Confirmed row count: `1,728,292`
- `public.tb_treatment`
  - Confirmed row count: `15,152,227`
- `public.tb_nhanvien`
  - Confirmed to have data
- `public.tb_bed`
  - Confirmed row count: `1,528`
- `public.tb_department`
  - Confirmed row count: `37`
- `public.tb_canhbaodichvu`
  - Confirmed row count: `2`
- `public.tb_canhbaonhapkhoa`
  - Confirmed row count: `1`
- `public.tb_phacdodieutri`
  - Confirmed row count: `23`
- `public.tb_phacdodieutri_phieudieutri`
  - Confirmed row count: `127`
- `public.tb_nhanvienlog`
  - Confirmed to have data
  - Used for the first connectivity/demo DAG

## Hospital-Meaningful Base Tables

These are the strongest confirmed hospital-related source tables for a business-ready medallion pipeline:

- `tb_patientrecord`
  - Patient encounter / patient record level data
  - Includes patient, encounter, diagnosis, admission/discharge, and billing summary fields
- `tb_servicedata`
  - Ordered/performed service activity
  - High-volume transactional fact table
- `tb_invoice`
  - Billing and payment facts
- `tb_treatment`
  - Treatment / clinical workflow data
  - Includes diagnosis, treatment dates, and treatment execution fields
- `tb_nhanvien`
  - Staff dimension / personnel lookup
- `tb_bed`
  - Bed / room / chamber / department resource data
- `tb_department`
  - Department dimension with department names and planned/actual bed counts
- `tb_canhbaodichvu`
  - Alert rule/config table
- `tb_canhbaonhapkhoa`
  - Admission alert rule/config table
- `tb_phacdodieutri`
  - Clinical pathway / treatment protocol definitions
- `tb_phacdodieutri_phieudieutri`
  - Clinical pathway execution / worksheet records

## Current Interpretation

- `tb_nhanvienlog` is suitable as a technical connectivity demo.
- The more hospital-meaningful next version of `hdos_sample` should be built from:
  - `tb_patientrecord`
  - `tb_servicedata`
  - `tb_invoice`
  - `tb_treatment`
  - `tb_nhanvien`
- `tb_bed` is also meaningful and can be added depending on whether bed occupancy or room/department resource reporting is needed.

## Recommended Next Pipeline Shape

If we replace the current login demo with a hospital-facing sample, the recommended medallion flow is:

- Raw:
  - ingest each base table separately from PostgreSQL
- Bronze:
  - typed landing for each base table
- Silver:
  - join and clean patient, service, invoice, treatment, and staff data
- Gold:
  - expose hospital business outputs such as:
    - service revenue
    - patient counts
    - treatment counts
    - discharge/admission summaries

## Notes

- Earlier `pg_stat_user_tables` estimates were not reliable enough for source selection.
- Actual SQL reads and direct table checks were used as the source of truth for the findings above.

## Dashboard Widget Source Coverage

Status legend:

- `Exact`: the source tables for the widget are confirmed and populated
- `Derivable`: the widget is sourceable, but needs business logic or joins across several tables
- `Partial`: some of the widget can be sourced, but not the full frontend behavior yet
- `Unconfirmed`: no exact populated source has been confirmed yet

### Widget Coverage Matrix

| Dashboard widget | Status | Recommended source tables | Verification notes |
| --- | --- | --- | --- |
| `Lượt khám hôm nay` | `Exact` | `tb_patientrecord`, optional `tb_reception` later | `tb_patientrecord` is populated (`763,887` rows) and contains encounter dates, patient IDs, patient names, departments, rooms, and diagnosis fields. |
| `Doanh thu` | `Exact` | `tb_invoice`, `tb_servicedata`, fallback totals in `tb_patientrecord` | `tb_invoice` (`1,728,292` rows) and `tb_servicedata` (`13,037,989` rows) are both populated. `tb_invoice` contains `invoicedate`, `departmentid`, `roomid`, `sotienphieu`, and invoice type fields. |
| `BN nội trú` | `Derivable` | `tb_patientrecord`, `tb_treatment` | `tb_patientrecord` contains inpatient-related dates, admission/discharge fields, and patient record status fields. `tb_treatment` (`15,152,227` rows) can support treatment-state logic. |
| `BOR toàn viện` | `Derivable` | `tb_bed`, `tb_department`, `tb_patientrecord` and/or `tb_treatment` | `tb_bed` (`1,528` rows) is populated and currently has `105` rows with non-empty `listmedicalrecordid`, which is enough to model occupied beds. `tb_department` (`37` rows) contains department names and planned/actual bed counts. |
| `Công suất giường theo khoa` | `Derivable` | `tb_bed`, `tb_department`, `tb_room`, `tb_patientrecord` and/or `tb_treatment` | The bed and department tables are populated and have the dimensions needed for department-level occupancy. This widget should be modeled as a bed-occupancy gold mart. |
| `Cảnh báo active` | `Partial` | `tb_canhbaodichvu`, `tb_canhbaonhapkhoa`, `tb_treatment`, possibly service/result facts | HDOS contains populated alert-rule tables: `tb_canhbaodichvu` (`2` rows) and `tb_canhbaonhapkhoa` (`1` row). These confirm alert configuration exists, but they do not by themselves explain the large active-alert count shown in the frontend. An additional alert-event fact source or derived alert logic is still needed. |
| `Cảnh báo đang kích hoạt` | `Partial` | `tb_canhbaodichvu`, `tb_canhbaonhapkhoa`, `tb_treatment`, possibly lab/service result tables | The current DB confirms alert rule/config data, but not yet a populated standalone alert-event table matching the frontend list. This widget is likely derivable, but the exact source logic is not fully proven yet. |
| `Dòng bệnh nhân hôm nay` | `Exact` | `tb_patientrecord`, `tb_treatment` | `tb_patientrecord` contains the encounter-level timestamps and patient identities needed for today-flow metrics. `tb_treatment` can refine stage logic if the frontend definition uses treatment states. |
| `Phân loại doanh thu` | `Derivable` | `tb_invoice`, `tb_servicedata`, `tb_patientrecord` | Finance tables are populated and can be classified by invoice/service/payment types. The exact frontend buckets such as BHYT, viện phí, dịch vụ, and BH tư nhân will require source-to-business mapping rules. |
| `Xe cấp cứu 115` | `Unconfirmed` | Candidate only: `tb_treatment` (`iscapcuu`), `tb_medicalrecord_capcuu`, `tb_crm_dieuphoi_*` | Emergency-related data exists, including `173,767` treatment rows with `iscapcuu = 1`, but no populated, clearly named ambulance fleet / vehicle activity fact table has been confirmed yet. |
| `Xe 115 hoạt động` | `Unconfirmed` | Candidate only: `tb_treatment` (`iscapcuu`), `tb_medicalrecord_capcuu`, `tb_crm_dieuphoi_*` | Same gap as the widget above. Current evidence supports emergency-care activity, not exact vehicle fleet status. |
| `Clinical Pathway` | `Derivable` | `tb_phacdodieutri`, `tb_phacdodieutri_phieudieutri`, `tb_treatment`, `tb_patientrecord` | HDOS contains populated pathway definition and execution tables: `tb_phacdodieutri` (`23` rows) and `tb_phacdodieutri_phieudieutri` (`127` rows). The frontend percentages should be sourceable after joining pathway definitions to execution and encounter context. |
| `Population Health` | `Partial` | `tb_patientrecord` diagnosis fields, optional chronic-disease tables if later populated | The dedicated chronic-disease tables found so far are currently empty in this demo database: `tb_patient_benhmantinh`, `tb_medicalrecord_daithaoduong`, `tb_medicalrecord_tanghuyetap`, `tb_medicalrecord_khambenh_daithaoduong`, `tb_medicalrecord_khambenh_tanghuyetap`, and `tb_medicalrecord_khambenh_hen` all returned `0` rows. We can still approximate disease cohorts from diagnosis fields in `tb_patientrecord`, but the exact registry-style frontend widget is not fully supported by populated source tables yet. |

### Current Conclusion

- The operational core of the dashboard is sourceable now:
  - encounter volume
  - patient flow
  - finance / revenue
  - inpatient counts
  - bed occupancy / BOR
- The pathway domain is also sourceable with additional joining and business logic.
- The alert domain is only partially confirmed:
  - alert rules/configs exist
  - exact active-alert event sourcing is not fully confirmed yet
- The population-health widget is only partially supported in the current demo data:
  - diagnosis-based cohorts are possible
  - dedicated chronic registry tables are empty
- The `Xe cấp cứu 115` / `Xe 115 hoạt động` widgets are the only major widgets not yet confirmed from an exact populated source table.

### Recommended Next Dashboard Pipeline Order

1. Build gold marts first for:
   - encounter activity
   - finance and revenue classification
   - inpatient counts
   - BOR / department bed occupancy
2. Add a second pass for:
   - clinical pathway compliance/progress
   - alert logic
3. Treat these as separate discovery tasks before promising parity with the frontend:
   - ambulance / 115 vehicle activity
   - population health registry percentages
