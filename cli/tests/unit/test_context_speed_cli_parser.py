"""Public command surface regression for the context speed profile."""

from inferyard.cli.arguments import parser


def test_speed_results_public_parser():
    args = parser().parse_args(["speed-results", "--batch", "/tmp/example"])
    assert args.command == "speed-results"
    assert str(args.batch) == "/tmp/example"
