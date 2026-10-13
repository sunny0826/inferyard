"""Read or replace explicitly selected startup options without IO or normalization."""


def argument_values(arguments, options):
    """Yield each selected option's position, name and value; missing values are None."""
    for index, argument in enumerate(arguments):
        option, separator, inline = argument.partition("=")
        if option in options:
            value = (
                inline
                if separator
                else (arguments[index + 1] if index + 1 < len(arguments) else None)
            )
            yield index, option, value


def model_argument(arguments):
    """Return a single unambiguous model value, or None for missing/invalid bindings."""
    values = [value for _, _, value in argument_values(arguments, ("-m", "--model"))]
    if len(values) != 1 or not values[0] or values[0].startswith("-"):
        return None
    return values[0]


def replace_arguments(arguments, replacements, *, expected_values=None):
    """Keep argv spelling/order and replace only existing selected option values."""
    result = list(arguments)
    for index, option, value in argument_values(arguments, replacements):
        if value is None:
            continue
        if expected_values and option in expected_values and value != expected_values[option]:
            continue
        if "=" in arguments[index]:
            result[index] = option + "=" + replacements[option]
        else:
            result[index + 1] = replacements[option]
    return result
