//go:build ui_debug && !release

package pages

import (
	"strings"

	"inferyard/studio/internal/core"
	"inferyard/studio/internal/inferyard"
)

type downloadsProtoState struct {
	activeTab        int
	hostPlatform     string
	selectedRelease  map[string]string
	selectedPlatform map[string]string
	selectedBuild    map[string]string
	drawerStableOnly bool
	drawerOpen       bool
	drawerFamilyID   string
	customEngineURL  string

	modelSource      string
	modelDrawerOpen  bool
	modelDrawerID    string
	customModelURL   string
	customTokenEnv   string
	customResolveURL string
	customResolved   *inferyard.ModelResolve
	customResolveErr string
}

func newDownloadsProtoState() *downloadsProtoState {
	return &downloadsProtoState{
		activeTab:        0,
		selectedRelease:  map[string]string{},
		selectedPlatform: map[string]string{},
		selectedBuild:    map[string]string{},
		modelSource:      "huggingface",
	}
}

func (st *downloadsProtoState) effectiveHostPlatform() string {
	if st != nil && strings.TrimSpace(st.hostPlatform) != "" {
		return st.hostPlatform
	}
	return core.CurrentPlatform()
}

func (st *downloadsProtoState) openDrawer(familyID, releaseID, platform, buildID string) {
	st.modelDrawerOpen = false
	st.drawerOpen = true
	st.drawerFamilyID = familyID
	if releaseID != "" {
		st.selectedRelease[familyID] = releaseID
	}
	if platform != "" {
		st.selectedPlatform[familyID] = platform
	}
	if buildID != "" {
		st.selectedBuild[familyID] = buildID
	}
}

func (st *downloadsProtoState) openModelDrawer(entryID string) {
	st.drawerOpen = false
	st.modelDrawerOpen = true
	st.modelDrawerID = entryID
}

func seedPrototypeAssets(s *viewState) {
	const (
		engineName = "v1.0.0 · llama.cpp"
		modelName  = "Qwen3-0.6B-Q8_0.gguf"
	)
	s.registry.Engines = []core.EngineAsset{{
		Name:   engineName,
		Family: "llama-cpp",
		Source: "本地内置安装",
	}}
	s.registry.DefaultEngine = engineName
	s.registry.Models = []core.ModelAsset{
		{Name: modelName, Format: "gguf", Available: true, Architecture: "qwen3", SizeBytes: previewValue(int64(639446688))},
		{Name: "Preview-Small.gguf", Format: "gguf", Available: true},
	}
	s.checks = core.EngineCheckOverview{
		Supported: true,
		Engines: map[string]core.EngineEligibility{
			engineName: {State: core.EngineCheckEligible, Family: "llama-cpp", Version: "v1.0.0"},
		},
	}
	s.engine = engineName
	s.model = modelName
}

func previewDownloadsPrototype(s *viewState) {
	s.selected = "downloads_prototype"
	s.downloads = nil
	s.assetsPreset = true
	s.modelURL = core.DefaultModelURL()
	s.engineURL = core.DefaultEngineURL()
	seedPrototypeAssets(s)
}

func previewPrototypeCatalog(includeCustom bool) *core.EngineCatalogRegistry {
	reg := core.DefaultEngineCatalog()

	reg.Register(core.StaticEngineProvider{
		Family: core.EngineFamilyCatalog{
			ID: "llama-cpp",
			Releases: []core.EngineRelease{
				{
					ID:           "llama-cpp-upstream-v1.1.0-rc1",
					Version:      "v1.1.0-rc1",
					Revision:     "v1.1.0-rc1",
					Distribution: "ggml-org",
					Channel:      core.ReleasePreRelease,
					ReleaseURL:   "https://github.com/ggml-org/llama.cpp/releases",
					Builds:       upstreamLlamaTagBuilds("v1.1.0-rc1", "https://github.com/ggml-org/llama.cpp/releases", ""),
				},
			},
		},
	})

	reg.Register(core.StaticEngineProvider{
		Family: core.EngineFamilyCatalog{
			ID: "mlx-lm",
			Releases: []core.EngineRelease{
				{
					ID:           "mlx-lm-0.31.1",
					Version:      "0.31.1",
					Revision:     "0.31.1",
					Distribution: "PyPI",
					Channel:      core.ReleaseStable,
					ReleaseURL:   "https://pypi.org/project/mlx-lm/0.31.1/",
					Builds: []core.EnginePlatformBuild{{
						ID:             "engine-mlx-lm-0311-macos-arm64",
						Platform:       "darwin/arm64",
						Variant:        "Python venv",
						AcquireMode:    core.EngineAcquireExternalPage,
						Artifacts:      []core.EngineArtifact{{Role: "release_page", Label: "mlx-lm 0.31.1 发布页", Version: "0.31.1", URL: "https://pypi.org/project/mlx-lm/0.31.1/"}},
						InstallCommand: "uv venv ~/.venvs/mlx-lm-031 --python 3.12 && uv pip install --python ~/.venvs/mlx-lm-031/bin/python mlx-lm==0.31.1",
					}},
				},
				{
					ID:           "mlx-lm-0.33.0a1",
					Version:      "0.33.0a1",
					Revision:     "0.33.0a1",
					Distribution: "PyPI",
					Channel:      core.ReleasePreRelease,
					ReleaseURL:   "https://pypi.org/project/mlx-lm/#history",
					Builds: []core.EnginePlatformBuild{{
						ID:             "engine-mlx-lm-pre-macos-arm64",
						Platform:       "darwin/arm64",
						Variant:        "Python venv",
						AcquireMode:    core.EngineAcquireExternalPage,
						Artifacts:      []core.EngineArtifact{{Role: "release_page", Label: "mlx-lm 预发布历史", Version: "0.33.0a1", URL: "https://pypi.org/project/mlx-lm/#history"}},
						InstallCommand: "uv venv ~/.venvs/mlx-lm-pre --python 3.12 && uv pip install --python ~/.venvs/mlx-lm-pre/bin/python --pre mlx-lm==0.33.0a1",
					}},
				},
			},
		},
	})

	reg.Register(core.StaticEngineProvider{
		Family: core.EngineFamilyCatalog{
			ID: "vllm",
			Releases: []core.EngineRelease{
				{
					ID:           "vllm-0.30.2",
					Version:      "v0.30.2",
					Revision:     "v0.30.2",
					Distribution: "GitHub",
					Channel:      core.ReleaseStable,
					ReleaseURL:   "https://github.com/vllm-project/vllm/releases/tag/v0.30.2",
					Builds:       vllmTagBuilds("v0.30.2", "https://github.com/vllm-project/vllm/releases/tag/v0.30.2", ""),
				},
				{
					ID:           "vllm-0.32.0-rc1",
					Version:      "v0.32.0-rc1",
					Revision:     "v0.32.0-rc1",
					Distribution: "GitHub",
					Channel:      core.ReleasePreRelease,
					ReleaseURL:   "https://github.com/vllm-project/vllm/releases",
					Builds:       vllmTagBuilds("v0.32.0-rc1", "https://github.com/vllm-project/vllm/releases", ""),
				},
			},
		},
	})

	reg.Register(core.StaticEngineProvider{
		Family: core.EngineFamilyCatalog{
			ID: "sglang",
			Releases: []core.EngineRelease{
				{
					ID:           "sglang-v0.4.6",
					Version:      "v0.4.6",
					Revision:     "v0.4.6",
					Distribution: "GitHub",
					Channel:      core.ReleaseStable,
					Recommended:  true,
					ReleaseURL:   "https://github.com/sgl-project/sglang/releases/tag/v0.4.6",
					Builds: []core.EnginePlatformBuild{{
						ID:             "engine-sglang-v046-any",
						Platform:       "any",
						Variant:        "发布页",
						AcquireMode:    core.EngineAcquireExternalPage,
						Artifacts:      []core.EngineArtifact{{Role: "release_page", Label: "SGLang v0.4.6 发布页", Version: "v0.4.6", URL: "https://github.com/sgl-project/sglang/releases/tag/v0.4.6"}},
						InstallCommand: "uv venv ~/.venvs/sglang --python 3.12 && uv pip install --python ~/.venvs/sglang/bin/python sglang==0.4.6",
						Note:           "在独立 Python 虚拟环境中安装后，注册本地引擎。",
					}},
				},
			},
		},
	})

	if includeCustom {
		reg.Register(core.StaticEngineProvider{
			Family: core.EngineFamilyCatalog{
				ID:             "koboldcpp",
				Name:           "KoboldCpp",
				ModelFormat:    "gguf",
				HomepageURL:    "https://github.com/LostRuins/koboldcpp",
				ReleasesURL:    "https://github.com/LostRuins/koboldcpp/releases",
				SupportsWizard: false,
				Releases: []core.EngineRelease{
					{
						ID:           "koboldcpp-v1.92-pre",
						Version:      "v1.92-pre",
						Revision:     "v1.92-pre",
						Distribution: "GitHub",
						Channel:      core.ReleasePreRelease,
						ReleaseURL:   "https://github.com/LostRuins/koboldcpp/releases",
						Builds: []core.EnginePlatformBuild{{
							ID:          "koboldcpp-pre-any",
							Platform:    "any",
							Variant:     "预发布",
							AcquireMode: core.EngineAcquireExternalPage,
							Artifacts:   []core.EngineArtifact{{Role: "release_page", Label: "KoboldCpp 预发布页", Version: "v1.92-pre", URL: "https://github.com/LostRuins/koboldcpp/releases"}},
						}},
					},
					{
						ID:           "koboldcpp-v1.91",
						Version:      "v1.91",
						Revision:     "v1.91",
						Distribution: "GitHub",
						Channel:      core.ReleaseStable,
						Recommended:  true,
						ReleaseURL:   "https://github.com/LostRuins/koboldcpp/releases/tag/v1.91",
						Builds: []core.EnginePlatformBuild{{
							ID:          "koboldcpp-v191-any",
							Platform:    "any",
							Variant:     "发布页",
							AcquireMode: core.EngineAcquireExternalPage,
							Artifacts:   []core.EngineArtifact{{Role: "release_page", Label: "KoboldCpp v1.91 发布页", Version: "v1.91", URL: "https://github.com/LostRuins/koboldcpp/releases/tag/v1.91"}},
						}},
					},
				},
			},
		})
	}
	return reg
}

func upstreamLlamaTagBuilds(version, url, note string) []core.EnginePlatformBuild {
	slug := strings.ReplaceAll(version, ".", "")
	return []core.EnginePlatformBuild{
		{
			ID: "llama-" + slug + "-mac-arm64", Platform: "darwin/arm64", Variant: "Metal / CPU",
			AcquireMode: core.EngineAcquireExternalPage,
			Artifacts:   []core.EngineArtifact{{Role: "release_page", Label: version + " 发布页", Version: version, URL: url}},
			Note:        note,
		},
		{
			ID: "llama-" + slug + "-mac-x64", Platform: "darwin/amd64", Variant: "CPU",
			AcquireMode: core.EngineAcquireExternalPage,
			Artifacts:   []core.EngineArtifact{{Role: "release_page", Label: version + " 发布页", Version: version, URL: url}},
			Note:        note,
		},
		{
			ID: "llama-" + slug + "-win-x64", Platform: "windows/amd64", Variant: "CPU / CUDA",
			AcquireMode: core.EngineAcquireExternalPage,
			Artifacts:   []core.EngineArtifact{{Role: "release_page", Label: version + " 发布页", Version: version, URL: url}},
			Note:        note,
		},
		{
			ID: "llama-" + slug + "-linux-x64", Platform: "linux/amd64", Variant: "CPU",
			AcquireMode: core.EngineAcquireExternalPage,
			Artifacts:   []core.EngineArtifact{{Role: "release_page", Label: version + " 发布页", Version: version, URL: url}},
			Note:        note,
		},
		{
			ID: "llama-" + slug + "-linux-arm64", Platform: "linux/arm64", Variant: "CPU",
			AcquireMode: core.EngineAcquireExternalPage,
			Artifacts:   []core.EngineArtifact{{Role: "release_page", Label: version + " 发布页", Version: version, URL: url}},
			Note:        note,
		},
	}
}

func vllmTagBuilds(version, url, note string) []core.EnginePlatformBuild {
	slug := strings.ReplaceAll(version, ".", "")
	return []core.EnginePlatformBuild{
		{
			ID: "vllm-" + slug + "-mac-arm64", Platform: "darwin/arm64", Variant: "CPU + vllm-metal",
			AcquireMode: core.EngineAcquireExternalPage,
			Artifacts:   []core.EngineArtifact{{Role: "release_page", Label: "vLLM " + version + " 发布页", Version: version, URL: url}},
			Note:        note,
		},
		{
			ID: "vllm-" + slug + "-linux-x64", Platform: "linux/amd64", Variant: "CUDA / CPU",
			AcquireMode: core.EngineAcquireExternalPage,
			Artifacts:   []core.EngineArtifact{{Role: "release_page", Label: "vLLM " + version + " 发布页", Version: version, URL: url}},
			Note:        note,
		},
		{
			ID: "vllm-" + slug + "-linux-arm64", Platform: "linux/arm64", Variant: "CUDA / CPU",
			AcquireMode: core.EngineAcquireExternalPage,
			Artifacts:   []core.EngineArtifact{{Role: "release_page", Label: "vLLM " + version + " 发布页", Version: version, URL: url}},
			Note:        note,
		},
	}
}
