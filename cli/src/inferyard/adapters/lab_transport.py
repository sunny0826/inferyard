"""Bounded lab observation HTTP; no redirects, proxy environment or implicit retries."""

import asyncio
import time

import httpx

from inferyard.adapters.lab_observation import (
    ObservationTracker,
    parse_identity,
    parse_lifecycle,
    parse_request,
)
from inferyard.adapters.lab_observation_json import MAX_RESPONSE_BYTES, LabProtocolError
from inferyard.platforms.identity import PreflightError


class LabTransport:
    async def read(self, path, body=None, *, deadline, headers=None):
        try:
            async with asyncio.timeout_at(deadline):
                async with self.client.stream(
                    "GET" if body is None else "POST", path, json=body, headers=headers
                ) as response:
                    if response.status_code != 200:
                        raise PreflightError("lab_contract_http_error")
                    raw = bytearray()
                    async for chunk in response.aiter_bytes():
                        raw.extend(chunk)
                        if len(raw) > MAX_RESPONSE_BYTES:
                            raise PreflightError("lab_contract_response_too_large")
                    if time.monotonic() >= deadline:
                        raise TimeoutError
                    return bytes(raw)
        except (httpx.HTTPError, TimeoutError) as exc:
            raise PreflightError("lab_contract_unavailable") from exc

    async def identity(self, *, deadline):
        raw = await self.read("/lab/v1/identity", deadline=deadline)
        try:
            value = parse_identity(raw, expected_engine=self.engine_id)
        except LabProtocolError as exc:
            raise PreflightError(str(exc)) from exc
        if self.binding is not None and value != self.binding:
            raise PreflightError("lab_identity_changed")
        if self.binding is None:
            if not all(value["capabilities"].values()):
                raise PreflightError("lab_required_capability_missing")
            self.binding = value
            self.tracker = ObservationTracker(value["server_instance_id"])
        if time.monotonic() >= deadline:
            raise PreflightError("lab_contract_deadline_exceeded")
        return value

    def headers(self, request_id=None):
        result = {"X-Lab-Instance-ID": self.binding["server_instance_id"]}
        if request_id is not None:
            result["X-Lab-Request-ID"] = request_id
        return result

    async def lifecycle(self, *, deadline):
        raw = await self.read("/lab/v1/lifecycle", deadline=deadline, headers=self.headers())
        try:
            value = parse_lifecycle(raw, expected_instance_id=self.binding["server_instance_id"])
            self.tracker.accept_lifecycle(value)
        except LabProtocolError as exc:
            raise PreflightError(str(exc)) from exc
        return value

    async def request_status(self, request_id, *, deadline):
        raw = await self.read(
            "/lab/v1/requests/" + request_id, deadline=deadline, headers=self.headers()
        )
        try:
            value = parse_request(
                raw,
                expected_instance_id=self.binding["server_instance_id"],
                expected_request_id=request_id,
            )
            self.tracker.accept_request(value)
        except LabProtocolError as exc:
            raise PreflightError(str(exc)) from exc
        return value

    async def management(self, path, body=None, *, timeout=5):
        # Kept for the common adapter interface; lab adapters never guess legacy routes.
        from inferyard.adapters.lab_observation_json import load_strict

        return load_strict(
            await self.read(
                path, body, deadline=time.monotonic() + timeout, headers=self.headers()
            ),
            "lab_invalid_management",
        )
