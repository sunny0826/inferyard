package core

import "inferyard/studio/internal/l10n"

func polystrataProvider() EngineCatalogProvider {
	releaseURL := "https://github.com/simplexai-labs/PolyStrata/releases/tag/v0.2.20-poly.5"
	sourceArtifact := artifactFromCatalog("engine-polystrata", "source", "PolyStrata v0.2.20-poly.5", "v0.2.20-poly.5")
	return StaticEngineProvider{
		Family: EngineFamilyCatalog{
			ID:             "polystrata",
			Name:           "PolyStrata",
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
	return StaticEngineProvider{
		Family: EngineFamilyCatalog{
			ID:             "mlx-lm",
			Name:           "mlx-lm",
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
							AcquireMode: EngineAcquireManualArchive,
							Artifacts: []EngineArtifact{
								artifactFromCatalog("engine-mlx-lm", "engine", "mlx-lm", "0.32.2"),
								artifactFromCatalog("runtime-mlx", "runtime", "mlx", "0.32.4"),
							},
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
	return StaticEngineProvider{
		Family: EngineFamilyCatalog{
			ID:             "vllm",
			Name:           "vLLM",
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
							AcquireMode: EngineAcquireManualArchive,
							Artifacts: []EngineArtifact{
								artifactFromCatalog("engine-vllm", "engine", "vLLM (CPU)", "v0.31.0"),
								artifactFromCatalog("engine-vllm-metal", "plugin", "vllm-metal", "0.31.0"),
							},
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
							Note:           l10n.TR("core.engine_catalog.note.python_venv_register"),
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

func sglangProvider() EngineCatalogProvider {
	releasesURL := "https://github.com/sgl-project/sglang/releases"
	snapshot := catalogEntryOrPanic("engine-sglang")
	return StaticEngineProvider{
		Family: EngineFamilyCatalog{
			ID:             "sglang",
			Name:           "SGLang",
			ModelFormat:    "directory",
			HomepageURL:    "https://github.com/sgl-project/sglang",
			ReleasesURL:    releasesURL,
			SupportsWizard: true,
			Releases: []EngineRelease{
				{
					ID:           "sglang-releases",
					Version:      l10n.TR("core.engine_catalog.channel.latest"),
					Revision:     "releases",
					Distribution: "GitHub",
					Channel:      ReleaseLatest,
					Recommended:  false,
					ReleaseURL:   releasesURL,
					Builds: []EnginePlatformBuild{
						{
							ID:          "engine-sglang-latest-any",
							Platform:    "any",
							Variant:     l10n.TR("core.engine_catalog.variant.release_page"),
							AcquireMode: EngineAcquireExternalPage,
							Artifacts: []EngineArtifact{
								{Role: "release_page", Label: "SGLang GitHub Releases", Version: l10n.TR("core.engine_catalog.channel.latest"), URL: releasesURL},
							},
							Note: l10n.TR("core.engine_catalog.note.release_page_register"),
						},
					},
				},
				{
					ID:           "sglang-e262b44",
					Version:      "e262b44",
					Revision:     snapshot.Revision,
					Distribution: "GitHub",
					Channel:      ReleaseSnapshot,
					Recommended:  true,
					ReleaseURL:   "https://github.com/sgl-project/sglang/tree/" + snapshot.Revision,
					Builds: func() []EnginePlatformBuild {
						archiveName := "sglang-" + snapshot.Revision + ".tar.gz"
						sourceArtifact := artifactFromCatalog("engine-sglang", "source", l10n.TR("core.engine_catalog.artifact.sglang_source"), "e262b44")
						sourceArtifact.Filename = archiveName
						linuxCmd := "tar -xzf " + archiveName + " && uv venv ~/.venvs/sglang --python 3.12 && uv pip install --python ~/.venvs/sglang/bin/python --prerelease=allow ./sglang-" + snapshot.Revision + "/python"
						appleMetalDoc := EngineArtifact{
							Role:    "release_page",
							Label:   l10n.TR("core.engine_catalog.artifact.apple_silicon_guide"),
							Version: "e262b44",
							URL:     "https://github.com/sgl-project/sglang/blob/" + snapshot.Revision + "/docs/docs/hardware-platforms/apple_metal.mdx",
						}
						return []EnginePlatformBuild{
							{
								ID:             "engine-sglang-snapshot-linux-x64",
								Platform:       "linux/amd64",
								Variant:        l10n.TR("core.engine_catalog.variant.source_cuda13"),
								AcquireMode:    EngineAcquireManualArchive,
								Artifacts:      []EngineArtifact{sourceArtifact},
								InstallCommand: linuxCmd,
								Note:           l10n.TR("core.engine_catalog.note.source_venv_register"),
							},
							{
								ID:             "engine-sglang-snapshot-linux-arm64",
								Platform:       "linux/arm64",
								Variant:        l10n.TR("core.engine_catalog.variant.source_cuda13"),
								AcquireMode:    EngineAcquireManualArchive,
								Artifacts:      []EngineArtifact{sourceArtifact},
								InstallCommand: linuxCmd,
								Note:           l10n.TR("core.engine_catalog.note.source_venv_register"),
							},
							{
								ID:          "engine-sglang-snapshot-macos-arm64",
								Platform:    "darwin/arm64",
								Variant:     l10n.TR("core.engine_catalog.variant.source_apple_mlx"),
								AcquireMode: EngineAcquireManualArchive,
								Artifacts:   []EngineArtifact{sourceArtifact, appleMetalDoc},
								Note:        l10n.TR("core.engine_catalog.note.source_apple_mlx_register"),
							},
						}
					}(),
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
