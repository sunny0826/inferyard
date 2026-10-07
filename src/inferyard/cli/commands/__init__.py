"""Lightweight command argument registration; no workflow imports."""


def compatibility_parser(commands, name, description):
    """Keep an entry callable and document its own help without listing it as primary."""
    return commands.add_parser(name, description=description)
