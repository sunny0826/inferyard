"""Shared schema primitives; wire revision is independent of algorithm definitions."""

from inferyard import SCHEMA_VERSION


def obj(properties, required=None):
    return {
        "type": "object",
        "properties": properties,
        "required": list(properties) if required is None else required,
        "additionalProperties": False,
    }


def array(items, minimum=0):
    return {"type": "array", "items": items, "minItems": minimum}


def enum(*values):
    return {"type": "string", "enum": list(values)}


def nullable(spec):
    return {"anyOf": [spec, {"type": "null"}]}


TEXT = {"type": "string"}
LABEL = {"type": "string", "minLength": 1}
IDENTIFIER = {"type": "string", "pattern": r"^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$"}
HASH = {"type": "string", "pattern": r"^[a-f0-9]{64}$"}
NAT = {"type": "integer", "minimum": 0}
POS = {"type": "integer", "minimum": 1}
NUMBER = {"type": "number"}
BOOL = {"type": "boolean"}
VERSION = {"type": "integer", "const": SCHEMA_VERSION}
STRING_LIST = array(LABEL)
EXECUTION = enum("completed", "failed", "cancelled", "invalid", "not_executed")
QUALITY = enum("pass", "fail", "unscorable", "not_scored")
PHASE = enum("probe", "warmup", "baseline", "formal", "scoring", "residual", "finalizing")
PROVENANCE = enum("declared", "observed", "verified", "unknown")
