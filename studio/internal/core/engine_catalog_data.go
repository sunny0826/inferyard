package core

import (
	"net/url"
	"path"
)

func DefaultEngineCatalog() *EngineCatalogRegistry {
	return NewEngineCatalogRegistry(
		prismLlamaCppProvider(),
		upstreamLlamaCppProvider(),
		mlxLMProvider(),
		vllmProvider(),
		sglangProvider(),
		lmStudioProvider(),
	)
}

func catalogEntryOrPanic(id string) CatalogEntry {
	entry, ok := CatalogEntryByID(id)
	if !ok {
		panic("missing catalog entry: " + id)
	}
	return entry
}

func urlFilename(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Path == "" {
		return ""
	}
	base := path.Base(u.Path)
	if base == "." || base == "/" {
		return ""
	}
	return base
}

func artifactFromCatalog(id, role, label, version string) EngineArtifact {
	entry := catalogEntryOrPanic(id)
	if version == "" {
		version = entry.Revision
	}
	return EngineArtifact{
		Role:     role,
		Label:    label,
		Version:  version,
		Filename: urlFilename(entry.URL),
		URL:      entry.URL,
		Bytes:    entry.Bytes,
		SHA256:   entry.SHA256,
	}
}

func prismLlamaCppProvider() EngineCatalogProvider {
	winCUDA := catalogEntryOrPanic("engine-windows-cuda-x64")
	return StaticEngineProvider{
		Family: EngineFamilyCatalog{
			ID:             "llama-cpp",
			Name:           "llama.cpp",
			ModelFormat:    "gguf",
			Summary:        "GGUF 推理核心（llama-server），包含 PrismML 核验版与 ggml-org 上游发行版。",
			HomepageURL:    "https://github.com/ggml-org/llama.cpp",
			ReleasesURL:    "https://github.com/PrismML-Eng/llama.cpp/releases",
			SupportsWizard: true,
			Releases: []EngineRelease{
				{
					ID:           "llama-cpp-prism-b10743",
					Version:      "prism-b10743-adfffbe",
					Revision:     "prism-b10743-adfffbe",
					Distribution: "PrismML 核验版",
					Channel:      ReleaseStable,
					Recommended:  true,
					ReleaseURL:   "https://github.com/PrismML-Eng/llama.cpp/releases/tag/prism-b10743-adfffbe",
					Builds: []EnginePlatformBuild{
						{
							ID:          "engine-macos-arm64",
							Platform:    "darwin/arm64",
							Variant:     "Metal / CPU",
							AcquireMode: EngineAcquireDirect,
							Artifacts: []EngineArtifact{
								artifactFromCatalog("engine-macos-arm64", "engine", "llama-server 主程序", "prism-b10743-adfffbe"),
							},
						},
						{
							ID:          "engine-macos-x64",
							Platform:    "darwin/amd64",
							Variant:     "CPU",
							AcquireMode: EngineAcquireDirect,
							Artifacts: []EngineArtifact{
								artifactFromCatalog("engine-macos-x64", "engine", "llama-server 主程序", "prism-b10743-adfffbe"),
							},
						},
						{
							ID:          "engine-windows-x64",
							Platform:    "windows/amd64",
							Variant:     "CPU",
							AcquireMode: EngineAcquireDirect,
							Profile:     catalogEntryOrPanic("engine-windows-x64").Profile,
							Artifacts: []EngineArtifact{
								artifactFromCatalog("engine-windows-x64", "engine", "llama-server 主程序", "prism-b10743-adfffbe"),
							},
						},
						{
							ID:          "engine-windows-cuda-x64",
							Platform:    "windows/amd64",
							Variant:     "CUDA 12.4",
							AcquireMode: EngineAcquireDirect,
							Profile:     winCUDA.Profile,
							Artifacts: []EngineArtifact{
								artifactFromCatalog("engine-windows-cuda-x64", "engine", "llama-server 主程序 (CUDA 12.4)", "prism-b10743-adfffbe"),
								{
									Role:     "runtime",
									Label:    "CUDA 12.4 运行时 (cudart)",
									Version:  "12.4",
									Filename: urlFilename(winCUDA.RuntimeURL),
									URL:      winCUDA.RuntimeURL,
									Bytes:    winCUDA.RuntimeBytes,
									SHA256:   winCUDA.RuntimeSHA256,
								},
							},
							Note: winCUDA.Note,
						},
						{
							ID:          "engine-linux-x64",
							Platform:    "linux/amd64",
							Variant:     "CPU",
							AcquireMode: EngineAcquireManualArchive,
							Artifacts: []EngineArtifact{
								artifactFromCatalog("engine-linux-x64", "engine", "llama-server 归档", "prism-b10743-adfffbe"),
							},
							Note: catalogEntryOrPanic("engine-linux-x64").Reason,
						},
						{
							ID:          "engine-linux-arm64",
							Platform:    "linux/arm64",
							Variant:     "CPU",
							AcquireMode: EngineAcquireManualArchive,
							Artifacts: []EngineArtifact{
								artifactFromCatalog("engine-linux-arm64", "engine", "llama-server 归档", "prism-b10743-adfffbe"),
							},
							Note: catalogEntryOrPanic("engine-linux-arm64").Reason,
						},
					},
				},
			},
		},
	}
}

func upstreamLlamaCppProvider() EngineCatalogProvider {
	tagURL := "https://github.com/ggml-org/llama.cpp/releases/tag/b11146"
	return StaticEngineProvider{
		Family: EngineFamilyCatalog{
			ID:             "llama-cpp",
			Name:           "llama.cpp",
			ModelFormat:    "gguf",
			Summary:        "GGUF 推理核心（llama-server），包含 PrismML 核验版与 ggml-org 上游发行版。",
			HomepageURL:    "https://github.com/ggml-org/llama.cpp",
			ReleasesURL:    "https://github.com/ggml-org/llama.cpp/releases",
			SupportsWizard: true,
			Releases: []EngineRelease{
				{
					ID:           "llama-cpp-upstream-b11146",
					Version:      "b11146",
					Revision:     "b11146-7fe450e19",
					Distribution: "ggml-org 上游",
					Channel:      ReleaseStable,
					Recommended:  false,
					ReleaseURL:   tagURL,
					Builds: []EnginePlatformBuild{
						{
							ID:          "llama-upstream-b11146-macos-arm64",
							Platform:    "darwin/arm64",
							Variant:     "Metal / CPU",
							AcquireMode: EngineAcquireExternalPage,
							Artifacts: []EngineArtifact{
								{Role: "release_page", Label: "ggml-org b11146 macOS arm64 发布页", Version: "b11146", URL: tagURL},
							},
							InstallCommand: "brew install llama.cpp",
							Note:           "从上游 Release 页面下载对应归档（或通过 Homebrew 安装），再用向导注册本地引擎。",
						},
						{
							ID:          "llama-upstream-b11146-macos-x64",
							Platform:    "darwin/amd64",
							Variant:     "CPU",
							AcquireMode: EngineAcquireExternalPage,
							Artifacts: []EngineArtifact{
								{Role: "release_page", Label: "ggml-org b11146 macOS x64 发布页", Version: "b11146", URL: tagURL},
							},
							InstallCommand: "brew install llama.cpp",
							Note:           "从上游 Release 页面下载对应归档后，通过向导注册本地引擎。",
						},
						{
							ID:          "llama-upstream-b11146-win-x64",
							Platform:    "windows/amd64",
							Variant:     "CPU / CUDA",
							AcquireMode: EngineAcquireExternalPage,
							Artifacts: []EngineArtifact{
								{Role: "release_page", Label: "ggml-org b11146 Windows 发布页", Version: "b11146", URL: tagURL},
							},
							Note: "从上游 Release 页面按显卡选择对应压缩包并注册本地引擎。",
						},
						{
							ID:          "llama-upstream-b11146-linux-x64",
							Platform:    "linux/amd64",
							Variant:     "CPU (b11146-7fe450e19)",
							AcquireMode: EngineAcquireExternalPage,
							Artifacts: []EngineArtifact{
								{Role: "release_page", Label: "ggml-org b11146 Linux x64 发布页", Version: "b11146", URL: tagURL},
							},
							Note: "对应 CLI llama_cpp_b11146_v1 适配器，从上游 Release 下载并配合 linux.example.toml 准备。",
						},
						{
							ID:          "llama-upstream-b11146-linux-arm64",
							Platform:    "linux/arm64",
							Variant:     "CPU",
							AcquireMode: EngineAcquireExternalPage,
							Artifacts: []EngineArtifact{
								{Role: "release_page", Label: "ggml-org b11146 Linux arm64 发布页", Version: "b11146", URL: tagURL},
							},
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
			Summary:        "Apple Silicon 原生 MLX 目录模型推理引擎，需在独立 Python 虚拟环境中安装 wheel。",
			HomepageURL:    "https://github.com/ml-explore/mlx-lm",
			ReleasesURL:    "https://pypi.org/project/mlx-lm/#history",
			SupportsWizard: true,
			Releases: []EngineRelease{
				{
					ID:           "mlx-lm-0.32.2",
					Version:      "0.32.2",
					Revision:     "0.32.2",
					Distribution: "PyPI 官方",
					Channel:      ReleaseStable,
					Recommended:  true,
					ReleaseURL:   "https://pypi.org/project/mlx-lm/0.32.2/",
					Builds: []EnginePlatformBuild{
						{
							ID:          "engine-mlx-lm-macos-arm64",
							Platform:    "darwin/arm64",
							Variant:     "Apple Silicon (Python venv)",
							AcquireMode: EngineAcquireManualArchive,
							Artifacts: []EngineArtifact{
								artifactFromCatalog("engine-mlx-lm", "engine", "mlx-lm 主包 (wheel)", "0.32.2"),
								artifactFromCatalog("runtime-mlx", "runtime", "mlx 核心运行时 (wheel)", "0.32.4"),
							},
							InstallCommand: "uv venv ~/.venvs/mlx-lm --python 3.12 && uv pip install --python ~/.venvs/mlx-lm/bin/python mlx-lm==0.32.2 mlx==0.32.4",
							Note:           catalogEntryOrPanic("engine-mlx-lm").Reason,
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
			Summary:        "高吞吐目录模型推理服务；macOS arm64 需配合 vllm-metal 加速插件安装。",
			HomepageURL:    "https://github.com/vllm-project/vllm",
			ReleasesURL:    "https://github.com/vllm-project/vllm/releases",
			SupportsWizard: true,
			Releases: []EngineRelease{
				{
					ID:           "vllm-0.31.0",
					Version:      "v0.31.0",
					Revision:     "v0.31.0",
					Distribution: "GitHub 官方",
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
								artifactFromCatalog("engine-vllm", "engine", "vllm (CPU wheel)", "v0.31.0"),
								artifactFromCatalog("engine-vllm-metal", "plugin", "vllm-metal 加速插件 (wheel)", "0.31.0"),
							},
							InstallCommand: "uv venv ~/.venvs/vllm --python 3.12 && uv pip install --python ~/.venvs/vllm/bin/python vllm-0.31.0+cpu-cp312-cp312-macosx_11_0_arm64.whl vllm_metal-0.31.0-cp312-cp312-macosx_15_0_arm64.whl",
							Note:           catalogEntryOrPanic("engine-vllm").Reason,
						},
						{
							ID:          "engine-vllm-linux-x64",
							Platform:    "linux/amd64",
							Variant:     "CUDA / CPU",
							AcquireMode: EngineAcquireExternalPage,
							Artifacts: []EngineArtifact{
								{Role: "release_page", Label: "vLLM v0.31.0 GitHub Release", Version: "v0.31.0", URL: tagURL},
							},
							InstallCommand: "uv venv ~/.venvs/vllm --python 3.12 && uv pip install --python ~/.venvs/vllm/bin/python vllm==0.31.0",
							Note:           "Linux 需按 CUDA/CPU 环境从官方安装后在向导中注册虚拟环境 Python 路径。",
						},
						{
							ID:          "engine-vllm-linux-arm64",
							Platform:    "linux/arm64",
							Variant:     "CUDA / CPU",
							AcquireMode: EngineAcquireExternalPage,
							Artifacts: []EngineArtifact{
								{Role: "release_page", Label: "vLLM v0.31.0 GitHub Release", Version: "v0.31.0", URL: tagURL},
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
			Summary:        "结构化生成与目录模型推理引擎；支持官方正式发行版及固定源码提交快照。",
			HomepageURL:    "https://github.com/sgl-project/sglang",
			ReleasesURL:    releasesURL,
			SupportsWizard: true,
			Releases: []EngineRelease{
				{
					ID:           "sglang-releases",
					Version:      "官方正式版",
					Revision:     "releases",
					Distribution: "GitHub Releases",
					Channel:      ReleaseLatest,
					Recommended:  false,
					ReleaseURL:   releasesURL,
					Builds: []EnginePlatformBuild{
						{
							ID:          "engine-sglang-latest-any",
							Platform:    "any",
							Variant:     "Releases 页面",
							AcquireMode: EngineAcquireExternalPage,
							Artifacts: []EngineArtifact{
								{Role: "release_page", Label: "SGLang GitHub Releases", Version: "官方正式版", URL: releasesURL},
							},
							Note: "打开 GitHub Releases 查看并安装最新正式版本，安装完成后通过向导注册。",
						},
					},
				},
				{
					ID:           "sglang-e262b44",
					Version:      "e262b44",
					Revision:     snapshot.Revision,
					Distribution: "固定源码提交",
					Channel:      ReleaseSnapshot,
					Recommended:  true,
					ReleaseURL:   "https://github.com/sgl-project/sglang/tree/" + snapshot.Revision,
					Builds: []EnginePlatformBuild{
						{
							ID:          "engine-sglang-snapshot-any",
							Platform:    "any",
							Variant:     "源码归档",
							AcquireMode: EngineAcquireManualArchive,
							Artifacts: []EngineArtifact{
								artifactFromCatalog("engine-sglang", "source", "SGLang 固定源码归档", "e262b44"),
							},
							InstallCommand: "uv venv ~/.venvs/sglang --python 3.12 && uv pip install --python ~/.venvs/sglang/bin/python ./sglang-" + snapshot.Revision + ".tar.gz",
							Note:           snapshot.Reason,
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
			ModelFormat:    "gguf",
			Summary:        "独立桌面推理应用；安装并加载模型后通过 lms CLI 接入本地服务。",
			HomepageURL:    "https://lmstudio.ai",
			ReleasesURL:    entry.URL,
			SupportsWizard: true,
			Releases: []EngineRelease{
				{
					ID:           "lmstudio-official",
					Version:      "官方正式版",
					Revision:     "official",
					Distribution: "LM Studio 官方",
					Channel:      ReleaseStable,
					Recommended:  true,
					ReleaseURL:   entry.URL,
					Builds: []EnginePlatformBuild{
						{
							ID:          "engine-lmstudio-any",
							Platform:    "any",
							Variant:     "桌面应用 + lms CLI",
							AcquireMode: EngineAcquireExternalPage,
							Artifacts: []EngineArtifact{
								{Role: "release_page", Label: "LM Studio 官方下载页", Version: "官方正式版", URL: entry.URL},
							},
							Note: entry.Reason,
						},
					},
				},
			},
		},
	}
}
