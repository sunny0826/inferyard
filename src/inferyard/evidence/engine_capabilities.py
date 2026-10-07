"""One read-bound interpretation of the historical Prism definition evidence."""

from dataclasses import dataclass

from inferyard.adapters.prism import BUILD
from inferyard.evidence.storage import local_file, read_json


@dataclass(frozen=True, slots=True)
class EngineCapabilities:
    prism_definition: bool
    sealed: bool

    @property
    def verified(self):
        return self.prism_definition and self.sealed


def bound_capabilities(root, manifest, *, reader=read_json):
    path = local_file(root, "service.props.json")
    props = reader(path) if path.exists() else {}
    return EngineCapabilities(
        type(props) is dict and props.get("build_info") == BUILD,
        manifest is not None and "service.props.json" in manifest,
    )
