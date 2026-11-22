package core

func polystrataProvider() EngineCatalogProvider {
	releaseURL := "https://github.com/simplexai-labs/PolyStrata/releases/tag/v0.1.45-poly.10"
	sourceArtifact := artifactFromCatalog("engine-polystrata", "source", "PolyStrata v0.1.45-poly.10", "v0.1.45-poly.10")
	return StaticEngineProvider{
		Family: EngineFamilyCatalog{
			ID:             "polystrata",
			Name:           "PolyStrata",
			ModelFormat:    "gguf",
			Summary:        "高性能跨平台 MoE 推理引擎，支持 Apple Silicon (Metal)、NVIDIA (CUDA) 与 AMD (HIP)。",
			HomepageURL:    "https://github.com/simplexai-labs/PolyStrata",
			ReleasesURL:    "https://github.com/simplexai-labs/PolyStrata/releases",
			SupportsWizard: true,
			ExternalApp:    false,
			Releases: []EngineRelease{
				{
					ID:           "polystrata-v0.1.45-poly.10",
					Version:      "v0.1.45-poly.10",
					Revision:     "v0.1.45-poly.10",
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
							Note:           "需 macOS 15+ 与 Xcode Command Line Tools，应用内自动配置并注册。",
						},
						{
							ID:             "engine-polystrata-win-x64",
							Platform:       "windows/amd64",
							Variant:        "NVIDIA CUDA / AMD HIP",
							AcquireMode:    EngineAcquireManagedSetup,
							Artifacts:      []EngineArtifact{sourceArtifact},
							InstallCommand: `inferyard engine prepare-polystrata --out .\polystrata --backend auto`,
							Note:           "支持 NVIDIA (驱动 580+) 与 AMD Radeon (HIP)，应用内自动配置并注册。",
						},
						{
							ID:             "engine-polystrata-linux-x64",
							Platform:       "linux/amd64",
							Variant:        "NVIDIA CUDA / AMD HIP",
							AcquireMode:    EngineAcquireManagedSetup,
							Artifacts:      []EngineArtifact{sourceArtifact},
							InstallCommand: "inferyard engine prepare-polystrata --out ./polystrata --backend auto",
							Note:           "需 python3-venv 与对应 GPU 驱动/工具链，应用内自动配置并注册。",
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
			Summary:        "",
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
							Note:           "在独立 Python 虚拟环境中安装后，注册本地引擎。",
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
			Summary:        "",
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
							Note:           "在独立 Python 虚拟环境中安装后，注册本地引擎。",
						},
						{
							ID:          "engine-vllm-linux-x64",
							Platform:    "linux/amd64",
							Variant:     "CUDA / CPU",
							AcquireMode: EngineAcquireExternalPage,
							Artifacts: []EngineArtifact{
								{Role: "release_page", Label: "vLLM v0.31.0 发布页", Version: "v0.31.0", URL: tagURL},
							},
							InstallCommand: "uv venv ~/.venvs/vllm --python 3.12 && uv pip install --python ~/.venvs/vllm/bin/python vllm==0.31.0",
							Note:           "在独立 Python 虚拟环境中安装后，注册本地引擎。",
						},
						{
							ID:          "engine-vllm-linux-arm64",
							Platform:    "linux/arm64",
							Variant:     "CUDA / CPU",
							AcquireMode: EngineAcquireExternalPage,
							Artifacts: []EngineArtifact{
								{Role: "release_page", Label: "vLLM v0.31.0 发布页", Version: "v0.31.0", URL: tagURL},
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
			Summary:        "",
			HomepageURL:    "https://github.com/sgl-project/sglang",
			ReleasesURL:    releasesURL,
			SupportsWizard: true,
			Releases: []EngineRelease{
				{
					ID:           "sglang-releases",
					Version:      "最新版",
					Revision:     "releases",
					Distribution: "GitHub",
					Channel:      ReleaseLatest,
					Recommended:  false,
					ReleaseURL:   releasesURL,
					Builds: []EnginePlatformBuild{
						{
							ID:          "engine-sglang-latest-any",
							Platform:    "any",
							Variant:     "发布页",
							AcquireMode: EngineAcquireExternalPage,
							Artifacts: []EngineArtifact{
								{Role: "release_page", Label: "SGLang GitHub Releases", Version: "最新版", URL: releasesURL},
							},
							Note: "从 GitHub Releases 安装对应版本后，注册本地引擎。",
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
						sourceArtifact := artifactFromCatalog("engine-sglang", "source", "SGLang 源码包", "e262b44")
						sourceArtifact.Filename = archiveName
						linuxCmd := "tar -xzf " + archiveName + " && uv venv ~/.venvs/sglang --python 3.12 && uv pip install --python ~/.venvs/sglang/bin/python --prerelease=allow ./sglang-" + snapshot.Revision + "/python"
						appleMetalDoc := EngineArtifact{
							Role:    "release_page",
							Label:   "Apple Silicon 安装说明",
							Version: "e262b44",
							URL:     "https://github.com/sgl-project/sglang/blob/" + snapshot.Revision + "/docs/docs/hardware-platforms/apple_metal.mdx",
						}
						return []EnginePlatformBuild{
							{
								ID:             "engine-sglang-snapshot-linux-x64",
								Platform:       "linux/amd64",
								Variant:        "源码包 (CUDA 13)",
								AcquireMode:    EngineAcquireManualArchive,
								Artifacts:      []EngineArtifact{sourceArtifact},
								InstallCommand: linuxCmd,
								Note:           "下载并解压源码归档后，在 Linux CUDA 13 环境的独立虚拟环境中从 python/ 目录安装，再注册本地引擎。",
							},
							{
								ID:             "engine-sglang-snapshot-linux-arm64",
								Platform:       "linux/arm64",
								Variant:        "源码包 (CUDA 13)",
								AcquireMode:    EngineAcquireManualArchive,
								Artifacts:      []EngineArtifact{sourceArtifact},
								InstallCommand: linuxCmd,
								Note:           "下载并解压源码归档后，在 Linux CUDA 13 环境的独立虚拟环境中从 python/ 目录安装，再注册本地引擎。",
							},
							{
								ID:          "engine-sglang-snapshot-macos-arm64",
								Platform:    "darwin/arm64",
								Variant:     "源码包 (Apple Silicon / MLX)",
								AcquireMode: EngineAcquireManualArchive,
								Artifacts:   []EngineArtifact{sourceArtifact, appleMetalDoc},
								Note:        "Apple Silicon 请解压源码归档后按上游 Apple Silicon (Metal / MLX) 安装说明使用 pyproject_other.toml 在独立虚拟环境中安装，再注册本地引擎。",
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
			Summary:        "",
			HomepageURL:    "https://lmstudio.ai",
			ReleasesURL:    entry.URL,
			SupportsWizard: true,
			ExternalApp:    true,
			Releases: []EngineRelease{
				{
					ID:           "lmstudio-official",
					Version:      "最新版",
					Revision:     "official",
					Distribution: "LM Studio",
					Channel:      ReleaseStable,
					Recommended:  true,
					ReleaseURL:   entry.URL,
					Builds: []EnginePlatformBuild{
						{
							ID:          "engine-lmstudio-any",
							Platform:    "any",
							Variant:     "桌面应用",
							AcquireMode: EngineAcquireExternalPage,
							Artifacts: []EngineArtifact{
								{Role: "release_page", Label: "LM Studio 下载页", Version: "最新版", URL: entry.URL},
							},
							Note: "安装桌面应用并启用 lms CLI，加载本地模型后注册本地引擎。",
						},
					},
				},
			},
		},
	}
}
