"""Apply the frozen benchmark recipe after platform recommendation refresh."""

from copy import deepcopy

from inferyard.config.context_speed_profile import CASES, GENERATION
from inferyard.config.preparation_io import PreparationError
from inferyard.config.startup_arguments import replace_arguments


def apply_recipe(config, metadata, context):
    maximum = metadata.get(str(metadata.get("general.architecture")) + ".context_length")
    if type(context) is not int or context not in CASES:
        raise PreparationError("context_unsupported")
    if type(maximum) is not int or maximum < context:
        raise PreparationError("model_context_unsupported")
    config["conditions"].update(context_size=context, slots=1, cache_policy="disabled")
    config["generation"] = deepcopy(GENERATION)
    config["execution"].update(
        repeats=1, concurrency=1, warmup_count=1, order="fixed", require_fresh_process=False
    )
    config["engine"]["startup_args"] = replace_arguments(
        config["engine"]["startup_args"], {"-c": str(context), "--ctx-size": str(context)}
    )
