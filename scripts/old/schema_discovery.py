import argparse
import json
import os
import sys
from collections import defaultdict

try:
    import pyarrow.parquet as pq
except ImportError:
    print("ERROR: pyarrow is not installed.")
    print("Install it with: python3 -m pip install pyarrow")
    sys.exit(1)


def classify_file_name(file_name: str) -> str:
    name = file_name.lower()
    for prefix in [
        "yellow_tripdata",
        "green_tripdata",
        "fhv_tripdata",
        "fhvhv_tripdata",
    ]:
        if name.startswith(prefix):
            return prefix
    return "other"


def schema_to_lines(schema) -> list[str]:
    lines = []
    for field in schema:
        lines.append(f"- {field.name}: {field.type}")
    return lines


def schema_signature(schema) -> str:
    payload = [{"name": field.name, "type": str(field.type)} for field in schema]
    return json.dumps(payload, sort_keys=True)


def main():
    parser = argparse.ArgumentParser(description="Discover and classify parquet schemas by filename family.")
    parser.add_argument("--base-path", default="/home/ubuntu/data_70gb/", help="Local folder containing parquet files")
    parser.add_argument("--sample-per-family", type=int, default=20, help="Max files to inspect per family")
    args = parser.parse_args()

    all_files = []
    for root, _, files in os.walk(args.base_path):
        for file_name in files:
            if file_name.lower().endswith(".parquet"):
                all_files.append(os.path.join(root, file_name))

    all_files.sort()

    if not all_files:
        print(f"No parquet files found under: {args.base_path}")
        return

    families = defaultdict(list)
    for path in all_files:
        families[classify_file_name(os.path.basename(path))].append(path)

    print("# SCHEMA DISCOVERY REPORT")
    print(f"Base path: {args.base_path}")
    print(f"Total parquet files: {len(all_files)}")
    print()

    for family in sorted(families.keys()):
        files = families[family]
        print(f"## FAMILY: {family}")
        print(f"File count: {len(files)}")

        signatures = defaultdict(lambda: {"count": 0, "sample_file": None, "schema_lines": None})

        for path in files[: args.sample_per_family]:
            try:
                schema = pq.read_schema(path)
                sig = schema_signature(schema)
                signatures[sig]["count"] += 1
                if signatures[sig]["sample_file"] is None:
                    signatures[sig]["sample_file"] = path
                    signatures[sig]["schema_lines"] = schema_to_lines(schema)
            except Exception as e:
                err_sig = json.dumps({"read_error": str(e)})
                signatures[err_sig]["count"] += 1
                if signatures[err_sig]["sample_file"] is None:
                    signatures[err_sig]["sample_file"] = path
                    signatures[err_sig]["schema_lines"] = [f"- READ_ERROR: {e}"]

        print(f"Sampled files: {min(len(files), args.sample_per_family)}")
        print(f"Distinct schema types in sample: {len(signatures)}")
        print()

        for idx, (_, info) in enumerate(signatures.items(), start=1):
            print(f"### Schema type {idx}")
            print(f"Sample file: {info['sample_file']}")
            print(f"Occurrences in sample: {info['count']}")
            print("Schema:")
            for line in info["schema_lines"]:
                print(line)
            print()


if __name__ == "__main__":
    main()
