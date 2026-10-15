"""Register read-only service discovery without loading platform collectors."""


def add_commands(commands):
    service = commands.add_parser("service", help="inspect existing local services")
    actions = service.add_subparsers(required=True)
    discover = actions.add_parser(
        "discover", help="discover loopback listeners; GET /health and /v1/models (2s each)"
    )
    discover.set_defaults(command="service discover")
