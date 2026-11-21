//go:build ui_debug && !release

package pages

import (
	"strings"

	"inferyard/studio/internal/core"
)

type downloadsProtoState struct {
	activeTab       int
	platform        string
	formatFilter    string
	stableOnly      bool
	selectedRelease map[string]string
	drawerOpen      bool
	drawerFamilyID  string
	drawerReleaseID string
	drawerBuildID   string
	customEngineURL string
}

func newDownloadsProtoState() *downloadsProtoState {
	return &downloadsProtoState{
		activeTab:       0,
		platform:        "current",
		formatFilter:    "all",
		stableOnly:      false,
		selectedRelease: map[string]string{},
	}
}

func (st *downloadsProtoState) openDrawer(familyID, releaseID, buildID string) {
	st.drawerOpen = true
	st.drawerFamilyID = familyID
	st.drawerReleaseID = releaseID
	st.drawerBuildID = buildID
}

func seedPrototypeAssets(s *viewState) {
	const (
		engineName = "prism-b10743-adfffbe · llama-server"
		modelName  = "Preview-Model-Q4.gguf"
	)
	s.registry.Engines = []core.EngineAsset{{
		Name:   engineName,
		Family: "llama-cpp",
		Source: "模拟引擎目录",
	}}
	s.registry.DefaultEngine = engineName
	s.registry.Models = []core.ModelAsset{
		{Name: modelName, Format: "gguf", Available: true, Architecture: "llama", SizeBytes: previewValue(int64(4 << 30))},
		{Name: "Preview-Small.gguf", Format: "gguf", Available: true},
	}
	s.checks = core.EngineCheckOverview{
		Supported: true,
		Engines: map[string]core.EngineEligibility{
			engineName: {State: core.EngineCheckEligible, Family: "llama-cpp", Version: "prism-b10743-adfffbe"},
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
					ID:           "llama-cpp-upstream-b10980",
					Version:      "b10980",
					Revision:     "b10980",
					Distribution: "ggml-org 上游",
					Channel:      core.ReleaseStable,
					ReleaseURL:   "https://github.com/ggml-org/llama.cpp/releases/tag/b10980",
					Builds:       upstreamLlamaTagBuilds("b10980", "https://github.com/ggml-org/llama.cpp/releases/tag/b10980", ""),
				},
				{
					ID:           "llama-cpp-upstream-b11210-rc1",
					Version:      "b11210-rc1",
					Revision:     "b11210-rc1",
					Distribution: "ggml-org 预发布",
					Channel:      core.ReleasePreRelease,
					ReleaseURL:   "https://github.com/ggml-org/llama.cpp/releases",
					Builds:       upstreamLlamaTagBuilds("b11210-rc1", "https://github.com/ggml-org/llama.cpp/releases", "预发布示例版本；正式评测建议优先选用正式版。"),
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
					Distribution: "PyPI 官方",
					Channel:      core.ReleaseStable,
					ReleaseURL:   "https://pypi.org/project/mlx-lm/0.31.1/",
					Builds: []core.EnginePlatformBuild{{
						ID:             "engine-mlx-lm-0311-macos-arm64",
						Platform:       "darwin/arm64",
						Variant:        "Apple Silicon (Python venv)",
						AcquireMode:    core.EngineAcquireExternalPage,
						Artifacts:      []core.EngineArtifact{{Role: "release_page", Label: "PyPI mlx-lm 0.31.1", Version: "0.31.1", URL: "https://pypi.org/project/mlx-lm/0.31.1/"}},
						InstallCommand: "uv venv ~/.venvs/mlx-lm-031 --python 3.12 && uv pip install --python ~/.venvs/mlx-lm-031/bin/python mlx-lm==0.31.1",
						Note:           "PyPI 历史正式版本（不固定 wheel 校验和）。",
					}},
				},
				{
					ID:           "mlx-lm-0.33.0a1",
					Version:      "0.33.0a1",
					Revision:     "0.33.0a1",
					Distribution: "PyPI 预发布",
					Channel:      core.ReleasePreRelease,
					ReleaseURL:   "https://pypi.org/project/mlx-lm/#history",
					Builds: []core.EnginePlatformBuild{{
						ID:             "engine-mlx-lm-pre-macos-arm64",
						Platform:       "darwin/arm64",
						Variant:        "Apple Silicon (Python venv)",
						AcquireMode:    core.EngineAcquireExternalPage,
						Artifacts:      []core.EngineArtifact{{Role: "release_page", Label: "PyPI mlx-lm 预发布历史", Version: "0.33.0a1", URL: "https://pypi.org/project/mlx-lm/#history"}},
						InstallCommand: "uv venv ~/.venvs/mlx-lm-pre --python 3.12 && uv pip install --python ~/.venvs/mlx-lm-pre/bin/python --pre mlx-lm==0.33.0a1",
						Note:           "预发布示例版本，仅用于多版本筛选与排序演示。",
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
					Distribution: "GitHub 官方",
					Channel:      core.ReleaseStable,
					ReleaseURL:   "https://github.com/vllm-project/vllm/releases/tag/v0.30.2",
					Builds:       vllmTagBuilds("v0.30.2", "https://github.com/vllm-project/vllm/releases/tag/v0.30.2", ""),
				},
				{
					ID:           "vllm-0.32.0-rc1",
					Version:      "v0.32.0-rc1",
					Revision:     "v0.32.0-rc1",
					Distribution: "GitHub 预发布",
					Channel:      core.ReleasePreRelease,
					ReleaseURL:   "https://github.com/vllm-project/vllm/releases",
					Builds:       vllmTagBuilds("v0.32.0-rc1", "https://github.com/vllm-project/vllm/releases", "预发布示例版本；正式评测建议使用正式版。"),
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
					Distribution: "GitHub 官方",
					Channel:      core.ReleaseStable,
					Recommended:  true,
					ReleaseURL:   "https://github.com/sgl-project/sglang/releases/tag/v0.4.6",
					Builds: []core.EnginePlatformBuild{{
						ID:             "engine-sglang-v046-any",
						Platform:       "any",
						Variant:        "官方发行版",
						AcquireMode:    core.EngineAcquireExternalPage,
						Artifacts:      []core.EngineArtifact{{Role: "release_page", Label: "SGLang v0.4.6 GitHub Release", Version: "v0.4.6", URL: "https://github.com/sgl-project/sglang/releases/tag/v0.4.6"}},
						InstallCommand: "uv venv ~/.venvs/sglang --python 3.12 && uv pip install --python ~/.venvs/sglang/bin/python sglang==0.4.6",
						Note:           "优先展示 GitHub 官方正式发行版；如需固定源码归档可在版本中切换源码快照。",
					}},
				},
			},
		},
	})

	if includeCustom {
		reg.Register(core.StaticEngineProvider{
			Family: core.EngineFamilyCatalog{
				ID:             "koboldcpp",
				Name:           "KoboldCpp（扩展接口示例）",
				ModelFormat:    "gguf",
				Summary:        "通过 EngineCatalogProvider 接口动态注册的新引擎示例，验证目录扩展能力。",
				HomepageURL:    "https://github.com/LostRuins/koboldcpp",
				ReleasesURL:    "https://github.com/LostRuins/koboldcpp/releases",
				SupportsWizard: false,
				Releases: []core.EngineRelease{
					{
						ID:           "koboldcpp-v1.92-pre",
						Version:      "v1.92-pre",
						Revision:     "v1.92-pre",
						Distribution: "GitHub 预发布",
						Channel:      core.ReleasePreRelease,
						ReleaseURL:   "https://github.com/LostRuins/koboldcpp/releases",
						Builds: []core.EnginePlatformBuild{{
							ID:          "koboldcpp-pre-any",
							Platform:    "any",
							Variant:     "预发布页面",
							AcquireMode: core.EngineAcquireExternalPage,
							Artifacts:   []core.EngineArtifact{{Role: "release_page", Label: "KoboldCpp 预发布页", Version: "v1.92-pre", URL: "https://github.com/LostRuins/koboldcpp/releases"}},
						}},
					},
					{
						ID:           "koboldcpp-v1.91",
						Version:      "v1.91",
						Revision:     "v1.91",
						Distribution: "GitHub 官方",
						Channel:      core.ReleaseStable,
						Recommended:  true,
						ReleaseURL:   "https://github.com/LostRuins/koboldcpp/releases/tag/v1.91",
						Builds: []core.EnginePlatformBuild{{
							ID:          "koboldcpp-v191-any",
							Platform:    "any",
							Variant:     "正式发行版",
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
			Artifacts:   []core.EngineArtifact{{Role: "release_page", Label: "ggml-org " + version + " 发布页", Version: version, URL: url}},
			Note:        note,
		},
		{
			ID: "llama-" + slug + "-mac-x64", Platform: "darwin/amd64", Variant: "CPU",
			AcquireMode: core.EngineAcquireExternalPage,
			Artifacts:   []core.EngineArtifact{{Role: "release_page", Label: "ggml-org " + version + " 发布页", Version: version, URL: url}},
			Note:        note,
		},
		{
			ID: "llama-" + slug + "-win-x64", Platform: "windows/amd64", Variant: "CPU / CUDA",
			AcquireMode: core.EngineAcquireExternalPage,
			Artifacts:   []core.EngineArtifact{{Role: "release_page", Label: "ggml-org " + version + " 发布页", Version: version, URL: url}},
			Note:        note,
		},
		{
			ID: "llama-" + slug + "-linux-x64", Platform: "linux/amd64", Variant: "CPU",
			AcquireMode: core.EngineAcquireExternalPage,
			Artifacts:   []core.EngineArtifact{{Role: "release_page", Label: "ggml-org " + version + " 发布页", Version: version, URL: url}},
			Note:        note,
		},
		{
			ID: "llama-" + slug + "-linux-arm64", Platform: "linux/arm64", Variant: "CPU",
			AcquireMode: core.EngineAcquireExternalPage,
			Artifacts:   []core.EngineArtifact{{Role: "release_page", Label: "ggml-org " + version + " 发布页", Version: version, URL: url}},
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
