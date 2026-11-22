package core

import (
	"net/url"
	"path"
)

func DefaultEngineCatalog() *EngineCatalogRegistry {
	return NewEngineCatalogRegistry(
		polystrataProvider(),
		upstreamLlamaCppProvider(),
		prismLlamaCppProvider(),
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
			Summary:        "",
			HomepageURL:    "https://github.com/ggml-org/llama.cpp",
			ReleasesURL:    "https://github.com/PrismML-Eng/llama.cpp/releases",
			SupportsWizard: true,
			Releases: []EngineRelease{
				{
					ID:           "llama-cpp-prism-b10743",
					Version:      "prism-b10743-adfffbe",
					Revision:     "prism-b10743-adfffbe",
					Distribution: "PrismML",
					Channel:      ReleaseStable,
					Recommended:  false,
					ReleaseURL:   "https://github.com/PrismML-Eng/llama.cpp/releases/tag/prism-b10743-adfffbe",
					Builds: []EnginePlatformBuild{
						{
							ID:          "engine-macos-arm64",
							Platform:    "darwin/arm64",
							Variant:     "Metal / CPU",
							AcquireMode: EngineAcquireDirect,
							Artifacts: []EngineArtifact{
								artifactFromCatalog("engine-macos-arm64", "engine", "llama-server", "prism-b10743-adfffbe"),
							},
						},
						{
							ID:          "engine-macos-x64",
							Platform:    "darwin/amd64",
							Variant:     "CPU",
							AcquireMode: EngineAcquireDirect,
							Artifacts: []EngineArtifact{
								artifactFromCatalog("engine-macos-x64", "engine", "llama-server", "prism-b10743-adfffbe"),
							},
						},
						{
							ID:          "engine-windows-x64",
							Platform:    "windows/amd64",
							Variant:     "CPU",
							AcquireMode: EngineAcquireDirect,
							Profile:     catalogEntryOrPanic("engine-windows-x64").Profile,
							Artifacts: []EngineArtifact{
								artifactFromCatalog("engine-windows-x64", "engine", "llama-server", "prism-b10743-adfffbe"),
							},
						},
						{
							ID:          "engine-windows-cuda-x64",
							Platform:    "windows/amd64",
							Variant:     "CUDA 12.4",
							AcquireMode: EngineAcquireDirect,
							Profile:     winCUDA.Profile,
							Artifacts: []EngineArtifact{
								artifactFromCatalog("engine-windows-cuda-x64", "engine", "llama-server (CUDA 12.4)", "prism-b10743-adfffbe"),
								{
									Role:     "runtime",
									Label:    "CUDA 12.4 (cudart)",
									Version:  "12.4",
									Filename: urlFilename(winCUDA.RuntimeURL),
									URL:      winCUDA.RuntimeURL,
									Bytes:    winCUDA.RuntimeBytes,
									SHA256:   winCUDA.RuntimeSHA256,
								},
							},
							Note: "包含 llama-server 与 CUDA 12.4 运行时。",
						},
						{
							ID:          "engine-linux-x64",
							Platform:    "linux/amd64",
							Variant:     "CPU",
							AcquireMode: EngineAcquireManualArchive,
							Artifacts: []EngineArtifact{
								artifactFromCatalog("engine-linux-x64", "engine", "llama-server", "prism-b10743-adfffbe"),
							},
							Note: "使用 inferyard config assets 与 linux.example.toml 准备配置。",
						},
						{
							ID:          "engine-linux-arm64",
							Platform:    "linux/arm64",
							Variant:     "CPU",
							AcquireMode: EngineAcquireManualArchive,
							Artifacts: []EngineArtifact{
								artifactFromCatalog("engine-linux-arm64", "engine", "llama-server", "prism-b10743-adfffbe"),
							},
							Note: "使用 inferyard config assets 与 linux.example.toml 准备配置。",
						},
					},
				},
			},
		},
	}
}

func upstreamLlamaCppProvider() EngineCatalogProvider {
	v100URL := "https://github.com/ggml-org/llama.cpp/releases/tag/v1.0.0"
	b11146URL := "https://github.com/ggml-org/llama.cpp/releases/tag/b11146"
	return StaticEngineProvider{
		Family: EngineFamilyCatalog{
			ID:             "llama-cpp",
			Name:           "llama.cpp",
			ModelFormat:    "gguf",
			Summary:        "",
			HomepageURL:    "https://github.com/ggml-org/llama.cpp",
			ReleasesURL:    "https://github.com/ggml-org/llama.cpp/releases",
			SupportsWizard: true,
			Releases: []EngineRelease{
				{
					ID:           "llama-cpp-upstream-v1.0.0",
					Version:      "v1.0.0",
					Revision:     "v1.0.0",
					Distribution: "ggml-org",
					Channel:      ReleaseStable,
					Recommended:  true,
					ReleaseURL:   v100URL,
					Builds: []EnginePlatformBuild{
						{
							ID:          "llama-upstream-v1.0.0-macos-arm64",
							Platform:    "darwin/arm64",
							Variant:     "Metal / CPU",
							AcquireMode: EngineAcquireExternalPage,
							Artifacts: []EngineArtifact{
								{Role: "release_page", Label: "v1.0.0 发布页", Version: "v1.0.0", URL: v100URL},
							},
							InstallCommand: "brew install llama.cpp",
							Note:           "通过 Homebrew 安装或从 GitHub Releases 下载后，注册本地引擎。",
						},
						{
							ID:          "llama-upstream-v1.0.0-macos-x64",
							Platform:    "darwin/amd64",
							Variant:     "CPU",
							AcquireMode: EngineAcquireExternalPage,
							Artifacts: []EngineArtifact{
								{Role: "release_page", Label: "v1.0.0 发布页", Version: "v1.0.0", URL: v100URL},
							},
							InstallCommand: "brew install llama.cpp",
							Note:           "通过 Homebrew 安装或从 GitHub Releases 下载后，注册本地引擎。",
						},
						{
							ID:          "llama-upstream-v1.0.0-win-x64",
							Platform:    "windows/amd64",
							Variant:     "CPU / CUDA",
							AcquireMode: EngineAcquireExternalPage,
							Artifacts: []EngineArtifact{
								{Role: "release_page", Label: "v1.0.0 发布页", Version: "v1.0.0", URL: v100URL},
							},
							InstallCommand: "irm https://llama.app/install.ps1 | iex",
							Note:           "运行官方安装脚本或从 GitHub Releases 下载后，注册本地引擎。",
						},
						{
							ID:          "llama-upstream-v1.0.0-linux-x64",
							Platform:    "linux/amd64",
							Variant:     "CPU / CUDA",
							AcquireMode: EngineAcquireExternalPage,
							Artifacts: []EngineArtifact{
								{Role: "release_page", Label: "v1.0.0 发布页", Version: "v1.0.0", URL: v100URL},
							},
							InstallCommand: "curl -LsSf https://llama.app/install.sh | sh",
							Note:           "运行官方安装脚本或从 GitHub Releases 下载后，注册本地引擎。",
						},
						{
							ID:          "llama-upstream-v1.0.0-linux-arm64",
							Platform:    "linux/arm64",
							Variant:     "CPU",
							AcquireMode: EngineAcquireExternalPage,
							Artifacts: []EngineArtifact{
								{Role: "release_page", Label: "v1.0.0 发布页", Version: "v1.0.0", URL: v100URL},
							},
							InstallCommand: "curl -LsSf https://llama.app/install.sh | sh",
						},
					},
				},
				{
					ID:           "llama-cpp-upstream-b11146",
					Version:      "b11146",
					Revision:     "b11146-7fe450e19",
					Distribution: "ggml-org",
					Channel:      ReleaseSnapshot,
					Recommended:  false,
					ReleaseURL:   b11146URL,
					Builds: []EnginePlatformBuild{
						{
							ID:          "llama-upstream-b11146-macos-arm64",
							Platform:    "darwin/arm64",
							Variant:     "Metal / CPU",
							AcquireMode: EngineAcquireExternalPage,
							Artifacts: []EngineArtifact{
								{Role: "release_page", Label: "b11146 发布页", Version: "b11146", URL: b11146URL},
							},
							Note: "从 GitHub Releases 下载 b11146 对应压缩包并解压后，注册本地引擎。",
						},
						{
							ID:          "llama-upstream-b11146-macos-x64",
							Platform:    "darwin/amd64",
							Variant:     "CPU",
							AcquireMode: EngineAcquireExternalPage,
							Artifacts: []EngineArtifact{
								{Role: "release_page", Label: "b11146 发布页", Version: "b11146", URL: b11146URL},
							},
							Note: "从 GitHub Releases 下载 b11146 对应压缩包并解压后，注册本地引擎。",
						},
						{
							ID:          "llama-upstream-b11146-win-x64",
							Platform:    "windows/amd64",
							Variant:     "CPU / CUDA",
							AcquireMode: EngineAcquireExternalPage,
							Artifacts: []EngineArtifact{
								{Role: "release_page", Label: "b11146 发布页", Version: "b11146", URL: b11146URL},
							},
							Note: "从 GitHub Releases 按显卡选择对应压缩包后，注册本地引擎。",
						},
						{
							ID:          "llama-upstream-b11146-linux-x64",
							Platform:    "linux/amd64",
							Variant:     "CPU",
							AcquireMode: EngineAcquireExternalPage,
							Artifacts: []EngineArtifact{
								{Role: "release_page", Label: "b11146 发布页", Version: "b11146", URL: b11146URL},
							},
							Note: "从 GitHub Releases 下载后，配合 linux.example.toml 准备配置。",
						},
						{
							ID:          "llama-upstream-b11146-linux-arm64",
							Platform:    "linux/arm64",
							Variant:     "CPU",
							AcquireMode: EngineAcquireExternalPage,
							Artifacts: []EngineArtifact{
								{Role: "release_page", Label: "b11146 发布页", Version: "b11146", URL: b11146URL},
							},
						},
					},
				},
			},
		},
	}
}
