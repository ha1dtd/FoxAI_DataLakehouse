import json
import os
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Final

CONFIG_FILE_ENV = "FOXAI_CONFIG_FILE"
DEFAULT_CONFIG_FILE = Path(__file__).with_suffix(".json")


@dataclass(frozen=True)
class SourceConfig:
    name: str
    schema: str
    primary_key: str
    query: str


def _load_config() -> dict[str, Any]:
    config_path = Path(os.environ.get(CONFIG_FILE_ENV, str(DEFAULT_CONFIG_FILE)))
    with config_path.open("r", encoding="utf-8") as f:
        config = json.load(f)
    if not isinstance(config, dict):
        raise ValueError(f"Config root must be an object: {config_path}")
    return config


_CONFIG = _load_config()


def _resolve_placeholders(value: str) -> str:
    if "{SCRIPT_BASE}" in value:
        value = value.replace("{SCRIPT_BASE}", str(_CONFIG.get("SCRIPT_BASE", "")))
    return value


def get_config(key: str, env_name: str | None = None) -> str:
    env_names = [f"FOXAI_{key}"]
    if env_name:
        env_names.append(env_name)
    else:
        env_names.append(key)
    for name in env_names:
        value = os.environ.get(name)
        if value is not None and value.strip():
            return value.strip()
    value = _CONFIG.get(key, "")
    if isinstance(value, str):
        return _resolve_placeholders(value)
    return str(value)


def get_sources() -> list[SourceConfig]:
    raw_sources = _CONFIG.get("SOURCES")
    if not isinstance(raw_sources, list) or not raw_sources:
        raise ValueError("Config SOURCES must be a non-empty list")

    sources: list[SourceConfig] = []
    for raw_source in raw_sources:
        if not isinstance(raw_source, dict):
            raise ValueError(f"Source config must be an object: {raw_source!r}")
        name = str(raw_source.get("name", "")).strip()
        schema = str(raw_source.get("schema", "")).strip()
        primary_key = str(raw_source.get("primary_key", "")).strip()
        query = str(raw_source.get("query", "")).strip()
        if not name or not schema or not primary_key or not query:
            raise ValueError(f"Source config is missing name/schema/primary_key/query: {raw_source!r}")
        sources.append(SourceConfig(name=name, schema=schema, primary_key=primary_key, query=query))
    return sources


MINIO_ENDPOINT: Final[str] = get_config("MINIO_ENDPOINT")
MINIO_ACCESS_KEY: Final[str] = get_config("MINIO_ACCESS_KEY")
MINIO_SECRET_KEY: Final[str] = get_config("MINIO_SECRET_KEY")
SPARK_SUBMIT_BIN: Final[str] = get_config("SPARK_SUBMIT_BIN")
SCRIPT_BASE: Final[str] = get_config("SCRIPT_BASE")
RAW_WAREHOUSE: Final[str] = get_config("RAW_WAREHOUSE")
BRONZE_WAREHOUSE: Final[str] = get_config("BRONZE_WAREHOUSE")
SILVER_WAREHOUSE: Final[str] = get_config("SILVER_WAREHOUSE")
GOLD_WAREHOUSE: Final[str] = get_config("GOLD_WAREHOUSE")
PG_HOST: Final[str] = get_config("PG_HOST")
PG_PORT: Final[str] = get_config("PG_PORT")
PG_DATABASE: Final[str] = get_config("PG_DATABASE")
PG_USER: Final[str] = get_config("PG_USER")
PG_PASSWORD: Final[str] = get_config("PG_PASSWORD")
RAW_NAMESPACE: Final[str] = get_config("RAW_NAMESPACE")
BRONZE_NAMESPACE: Final[str] = get_config("BRONZE_NAMESPACE")
SILVER_NAMESPACE: Final[str] = get_config("SILVER_NAMESPACE")
GOLD_NAMESPACE: Final[str] = get_config("GOLD_NAMESPACE")
SOURCES: Final[list[SourceConfig]] = get_sources()
