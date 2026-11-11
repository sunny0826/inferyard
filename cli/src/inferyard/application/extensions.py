"""Command results for the versioned extension freeze/run/replay lifecycle."""

import asyncio

from inferyard.application.types import CommandResult


def _freeze(request):
    from inferyard.evidence.storage import read_json
    from inferyard.extensions.workflow import freeze

    plan = freeze(read_json(request.extension_spec), request.config, request.out)
    return 0, CommandResult(
        request.command, "frozen", "complete", evidence_dir=str(request.out), details=plan
    )


def _replay(request):
    from inferyard.errors import EvidenceError
    from inferyard.evidence.storage import read_json
    from inferyard.extensions.workflow import save_packet

    packet = read_json(request.extension_spec)
    if packet.get("evidence_kind") != "fixture":
        raise EvidenceError("offline_import_cannot_grant_live_qualification")
    return save_packet(request.out, packet), "software_verified"


def _run(request):
    from inferyard.extensions.workflow import live, read_plan
    from inferyard.implementation_identity import IdentityContext
    from inferyard.provenance import tool_source_hash

    identity_context = IdentityContext(source_hash=tool_source_hash)
    plan = read_plan(
        request.extension_spec, request.config, source_identity=identity_context.source
    )
    return (
        asyncio.run(live(plan, request.config, request.out, identity_context=identity_context)),
        "executed",
    )


def execute(request):
    if request.command == "extension-check":
        from inferyard.extensions.extension_evidence import verify_packet

        path, summary, status = request.run, verify_packet(request.run), "verified"
    elif request.command == "extension-freeze":
        return _freeze(request)
    elif request.command == "extension-replay":
        (path, summary), status = _replay(request)
    else:
        (path, summary), status = _run(request)
    return 0, CommandResult(
        request.command, status, "complete", evidence_dir=str(path), details=summary
    )
