"""Pinned template/tokenizer management calls, without generation or context admission."""

import hashlib

from inferyard.platforms.identity import PreflightError


async def count_template(adapter, config, prompt):
    from inferyard.adapters.requests import request_body

    if hasattr(adapter, "count_template"):
        return await adapter.count_template(config, prompt)
    body = request_body(config, prompt, stream=False)
    template = await adapter.management(
        "/apply-template",
        {
            "messages": body["messages"],
            "add_generation_prompt": True,
            "chat_template_kwargs": body["chat_template_kwargs"],
        },
    )
    if not isinstance(template, dict) or not isinstance(template.get("prompt"), str):
        raise PreflightError("template_render_unverified")
    tokens = await adapter.management(
        "/tokenize",
        {
            "content": template["prompt"],
            "add_special": True,
            "parse_special": True,
        },
    )
    ids = tokens.get("tokens") if isinstance(tokens, dict) else None
    if not isinstance(ids, list) or any(type(token) is not int or token < 0 for token in ids):
        raise PreflightError("token_count_unverified")
    return {
        "input_tokens": len(ids),
        "output_budget": config["generation"]["max_tokens"],
        "template_prompt_sha256": hashlib.sha256(template["prompt"].encode()).hexdigest(),
        "source": "apply-template+tokenize:add_special,parse_special",
        "verification": "verified",
    }
