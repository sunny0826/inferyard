"""Read-only discovery: diagnostics never confer permission to bind or run."""

import httpx

from inferyard.application.types import CommandResult
from inferyard.contracts.validation import ContractError, strict_json_loads
from inferyard.platforms.service_discovery import DiscoveryError, collect_listeners


def _get(client, url, *, models=False):
    try:
        client.cookies.clear()
        with client.stream("GET", url) as response:
            category = f"{response.status_code // 100}xx"
            if not models:
                return category, None, None
            if category != "2xx":
                return category, None, f"models_http_{category}"
            payload = bytearray()
            for chunk in response.iter_bytes():
                payload.extend(chunk)
                if len(payload) > 1024 * 1024:
                    return category, None, "models_response_too_large"
            try:
                value = strict_json_loads(payload.decode("utf-8"))
            except ContractError, ValueError, UnicodeError:
                return category, None, "models_invalid_response"
            if not isinstance(value, dict) or not isinstance(value.get("data"), list):
                return category, None, "models_invalid_response"
            entries = value["data"]
            if not entries:
                return category, None, "models_empty"
            if any(
                not isinstance(item, dict)
                or not isinstance(item.get("id"), str)
                or not item["id"].strip()
                for item in entries
            ):
                return category, None, "models_invalid_response"
            return category, list(dict.fromkeys(item["id"] for item in entries)), None
    except httpx.TimeoutException:
        return None, None, "models_timeout" if models else "health_timeout"
    except httpx.HTTPError:
        return None, None, "models_unreachable" if models else "health_unreachable"


def execute(request):
    details = {"services": [], "model_requests_sent": 0}
    try:
        processes, limitations = collect_listeners()
        owners = {}
        for process in processes:
            for _, port in process["listeners"]:
                owners.setdefault(port, set()).add(process["pid"])
        probes = {}
        if processes:
            with httpx.Client(timeout=2.0, trust_env=False, follow_redirects=False) as client:
                for process in sorted(processes, key=lambda item: item["pid"]):
                    endpoints = []
                    for host, port in process["listeners"]:
                        address = f"[{host}]" if ":" in host else host
                        url = f"http://{address}:{port}"
                        if url not in probes:
                            health, _, health_reason = _get(client, url + "/health")
                            _, models, reason = _get(client, url + "/v1/models", models=True)
                            probes[url] = {
                                "url": url,
                                "health": health,
                                "models": models,
                                "reason": reason or health_reason,
                                "ambiguous_listener": len(owners[port]) > 1,
                            }
                        endpoints.append(dict(probes[url]))
                    details["services"].append(
                        {key: value for key, value in process.items() if key != "listeners"}
                        | {"endpoints": endpoints}
                    )
        return (3 if limitations else 0), CommandResult(
            request.command,
            "partial" if limitations else "discovered",
            "incomplete" if limitations else "complete",
            limitations=tuple(sorted(limitations)),
            details=details,
        )
    except DiscoveryError as exc:
        return 4, CommandResult(request.command, "error", limitations=(str(exc),), details=details)
    except Exception:
        return 4, CommandResult(
            request.command,
            "error",
            limitations=("service_discovery_error",),
            details=details,
        )
