package core

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
					Builds: []EnginePlatformBuild{
						func() EnginePlatformBuild {
							archiveName := "sglang-" + snapshot.Revision + ".tar.gz"
							sourceArtifact := artifactFromCatalog("engine-sglang", "source", "SGLang 源码包", "e262b44")
							sourceArtifact.Filename = archiveName
							return EnginePlatformBuild{
								ID:             "engine-sglang-snapshot-any",
								Platform:       "any",
								Variant:        "源码包",
								AcquireMode:    EngineAcquireManualArchive,
								Artifacts:      []EngineArtifact{sourceArtifact},
								InstallCommand: "tar -xzf " + archiveName + " && uv venv ~/.venvs/sglang --python 3.12 && uv pip install --python ~/.venvs/sglang/bin/python ./sglang-" + snapshot.Revision + "/python",
								Note:           "下载并解压源码归档后，按目标硬件从解压目录的 python/ 子目录在独立虚拟环境中安装，再注册本地引擎。",
							}
						}(),
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
