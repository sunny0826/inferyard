"""Shared JSON value shapes for comparison plan, history and v2 records."""

type JSONValue = str | int | float | bool | None | list[JSONValue] | dict[str, JSONValue]
type JSONRecord = dict[str, JSONValue]
type JSONRows = list[JSONRecord]
