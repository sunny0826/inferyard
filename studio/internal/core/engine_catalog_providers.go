package core

import "inferyard/studio/internal/l10n"

func polystrataProvider() EngineCatalogProvider {
	releaseURL := "https://github.com/simplexai-labs/PolyStrata/releases/tag/v0.2.20-poly.5"
	sourceArtifact := artifactFromCatalog("engine-polystrata", "source", "PolyStrata v0.2.20-poly.5", "v0.2.20-poly.5")
	return StaticEngineProvider{
		Family: EngineFamilyCatalog{
			ID:             "polystrata",
			Name:           "PolyStrata",
			Description:    l10n.TR("core.engine_catalog.description.polystrata"),
			ModelFormat:    "gguf",
			HomepageURL:    "https://github.com/simplexai-labs/PolyStrata",
			ReleasesURL:    "https://github.com/simplexai-labs/PolyStrata/releases",
			SupportsWizard: true,
			ExternalApp:    false,
			Releases: []EngineRelease{
				{
					ID:           "polystrata-v0.2.20-poly.5",
					Version:      "v0.2.20-poly.5",
					Revision:     "v0.2.20-poly.5",
					Distribution: "simplexai-labs",
					Channel:      ReleaseStable,
					Recommended:  true,
					ReleaseURL:   releaseURL,
					Builds: []EnginePlatformBuild{
						{
							ID:             "engine-polystrata-macos-arm64",
							Platform:       "darwin/arm64",
							Variant:        "Apple Silicon (Metal)",
							AcquireMode:    EngineAcquireManagedSetup,
							Artifacts:      []EngineArtifact{sourceArtifact},
							InstallCommand: "inferyard engine prepare-polystrata --out ./polystrata --backend metal",
							Note:           l10n.TR("core.engine_catalog.note.polystrata_macos"),
						},
						{
							ID:             "engine-polystrata-win-x64",
							Platform:       "windows/amd64",
							Variant:        "NVIDIA CUDA / AMD HIP",
							AcquireMode:    EngineAcquireManagedSetup,
							Artifacts:      []EngineArtifact{sourceArtifact},
							InstallCommand: `inferyard engine prepare-polystrata --out .\polystrata --backend auto`,
							Note:           l10n.TR("core.engine_catalog.note.polystrata_win"),
						},
						{
							ID:             "engine-polystrata-linux-x64",
							Platform:       "linux/amd64",
							Variant:        "NVIDIA CUDA / AMD HIP",
							AcquireMode:    EngineAcquireManagedSetup,
							Artifacts:      []EngineArtifact{sourceArtifact},
							InstallCommand: "inferyard engine prepare-polystrata --out ./polystrata --backend auto",
							Note:           l10n.TR("core.engine_catalog.note.polystrata_linux"),
						},
					},
				},
			},
		},
	}
}

func mlxLMProvider() EngineCatalogProvider {
	mlxLMWheel := catalogEntryOrPanic("engine-mlx-lm").URL
	mlxWheel := catalogEntryOrPanic("runtime-mlx").URL
	return StaticEngineProvider{
		Family: EngineFamilyCatalog{
			ID:             "mlx-lm",
			Name:           "mlx-lm",
			Description:    l10n.TR("core.engine_catalog.description.mlx_lm"),
			ModelFormat:    "directory",
			HomepageURL:    "https://github.com/ml-explore/mlx-lm",
			ReleasesURL:    "https://pypi.org/project/mlx-lm/#history",
			SupportsWizard: true,
			Releases: []EngineRelease{
				{
					ID:           "mlx-lm-0.32.2",
					Version:      "0.32.2",
					Revision:     "0.32.2",
					Distribution: "PyPI",
					Channel:      ReleaseStable,
					Recommended:  true,
					ReleaseURL:   "https://pypi.org/project/mlx-lm/0.32.2/",
					Builds: []EnginePlatformBuild{
						{
							ID:          "engine-mlx-lm-macos-arm64",
							Platform:    "darwin/arm64",
							Variant:     "Python venv",
							AcquireMode: EngineAcquireManagedVenv,
							Artifacts: []EngineArtifact{
								artifactFromCatalog("engine-mlx-lm", "engine", "mlx-lm", "0.32.2"),
								artifactFromCatalog("runtime-mlx", "runtime", "mlx", "0.32.4"),
							},
							ManagedSpec:    &EngineManagedVenvSpec{Python: "3.12", WheelURLs: []string{mlxLMWheel, mlxWheel}},
							InstallCommand: "uv venv ~/.venvs/mlx-lm --python 3.12 && uv pip install --python ~/.venvs/mlx-lm/bin/python mlx-lm==0.32.2 mlx==0.32.4",
							Note:           l10n.TR("core.engine_catalog.note.python_venv_register"),
						},
					},
				},
			},
		},
	}
}

func vllmProvider() EngineCatalogProvider {
	tagURL := "https://github.com/vllm-project/vllm/releases/tag/v0.31.0"
	vllmWheel := catalogEntryOrPanic("engine-vllm").URL
	metalWheel := catalogEntryOrPanic("engine-vllm-metal").URL
	return StaticEngineProvider{
		Family: EngineFamilyCatalog{
			ID:             "vllm",
			Name:           "vLLM",
			Description:    l10n.TR("core.engine_catalog.description.vllm"),
			ModelFormat:    "directory",
			HomepageURL:    "https://github.com/vllm-project/vllm",
			ReleasesURL:    "https://github.com/vllm-project/vllm/releases",
			SupportsWizard: true,
			Releases: []EngineRelease{
				{
					ID:           "vllm-0.31.0",
					Version:      "v0.31.0",
					Revision:     "v0.31.0",
					Distribution: "GitHub",
					Channel:      ReleaseStable,
					Recommended:  true,
					ReleaseURL:   tagURL,
					Builds: []EnginePlatformBuild{
						{
							ID:          "engine-vllm-macos-arm64",
							Platform:    "darwin/arm64",
							Variant:     "CPU + vllm-metal",
							AcquireMode: EngineAcquireManagedVenv,
							Artifacts: []EngineArtifact{
								artifactFromCatalog("engine-vllm", "engine", "vLLM (CPU)", "v0.31.0"),
								artifactFromCatalog("engine-vllm-metal", "plugin", "vllm-metal", "0.31.0"),
							},
							ManagedSpec:    &EngineManagedVenvSpec{Python: "3.12", WheelURLs: []string{vllmWheel, metalWheel}},
							InstallCommand: "uv venv ~/.venvs/vllm --python 3.12 && uv pip install --python ~/.venvs/vllm/bin/python vllm-0.31.0+cpu-cp312-cp312-macosx_11_0_arm64.whl vllm_metal-0.31.0-cp312-cp312-macosx_15_0_arm64.whl",
							Note:           l10n.TR("core.engine_catalog.note.python_venv_register"),
						},
						{
							ID:          "engine-vllm-linux-x64",
							Platform:    "linux/amd64",
							Variant:     "CUDA / CPU",
							AcquireMode: EngineAcquireExternalPage,
							Artifacts: []EngineArtifact{
								{Role: "release_page", Label: l10n.TRf("core.engine_catalog.artifact.release_page", "version", "vLLM v0.31.0"), Version: "v0.31.0", URL: tagURL},
							},
							InstallCommand: "uv venv ~/.venvs/vllm --python 3.12 && uv pip install --python ~/.venvs/vllm/bin/python vllm==0.31.0",
						},
						{
							ID:          "engine-vllm-linux-arm64",
							Platform:    "linux/arm64",
							Variant:     "CUDA / CPU",
							AcquireMode: EngineAcquireExternalPage,
							Artifacts: []EngineArtifact{
								{Role: "release_page", Label: l10n.TRf("core.engine_catalog.artifact.release_page", "version", "vLLM v0.31.0"), Version: "v0.31.0", URL: tagURL},
							},
							InstallCommand: "uv venv ~/.venvs/vllm --python 3.12 && uv pip install --python ~/.venvs/vllm/bin/python vllm==0.31.0",
						},
					},
				},
			},
		},
	}
}

func lmStudioProvider() EngineCatalogProvider {
	entry := catalogEntryOrPanic("engine-lmstudio")
	return StaticEngineProvider{
		Family: EngineFamilyCatalog{
			ID:             "lmstudio",
			Name:           "LM Studio",
			Description:    l10n.TR("core.engine_catalog.description.lmstudio"),
			ModelFormat:    "gguf",
			HomepageURL:    "https://lmstudio.ai",
			ReleasesURL:    entry.URL,
			SupportsWizard: true,
			ExternalApp:    true,
			Releases: []EngineRelease{
				{
					ID:           "lmstudio-official",
					Version:      l10n.TR("core.engine_catalog.channel.latest"),
					Revision:     "official",
					Distribution: "LM Studio",
					Channel:      ReleaseStable,
					Recommended:  true,
					ReleaseURL:   entry.URL,
					Builds: []EnginePlatformBuild{
						{
							ID:          "engine-lmstudio-any",
							Platform:    "any",
							Variant:     l10n.TR("core.engine_catalog.variant.desktop_app"),
							AcquireMode: EngineAcquireExternalPage,
							Artifacts: []EngineArtifact{
								{Role: "release_page", Label: l10n.TR("core.engine_catalog.artifact.lmstudio_download"), Version: l10n.TR("core.engine_catalog.channel.latest"), URL: entry.URL},
							},
							Note: l10n.TR("core.engine_catalog.note.lmstudio_register"),
						},
					},
				},
			},
		},
	}
}
