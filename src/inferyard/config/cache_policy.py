"""Reject ambiguous startup cache settings before claiming disabled caches."""


def disabled_startup(args):
    switches = {"--no-cache-prompt": 0, "--no-cache-idle-slots": 0}
    values = []
    for index, argument in enumerate(args):
        name, separator, value = argument.partition("=")
        if name in ("--cache-prompt", "--cache-idle-slots"):
            return False
        if name in switches:
            if separator:
                return False
            switches[name] += 1
        if name in ("--cache-ram", "-cram"):
            if not separator:
                value = args[index + 1] if index + 1 < len(args) else None
            values.append(value)
    return all(count == 1 for count in switches.values()) and values == ["0"]
