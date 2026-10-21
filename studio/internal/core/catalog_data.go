package core

// The built-in address data. Each entry pins one asset with its measured
// size and digest; the downloadability of an entry is derived from the
// validators in catalog.go rather than declared here, so the data cannot
// claim a download the transfer path would refuse.

// catalogEntries is the built-in address list. Engine archives carry the
// measured size and digest of the pinned release assets; model files carry
// the measured digest of the pinned repository revisions.
var catalogEntries = []CatalogEntry{
	{
		ID: "engine-macos-arm64", Kind: CatalogEngine, Title: "llama-server · macOS arm64",
		Family: "llama-cpp", URL: catalogEngineBase + "llama-prism-b10743-adfffbe-bin-macos-arm64.tar.gz",
		Bytes: 11517506, SHA256: "596d257973080ca5011a4be50477c5f93ed1d231fcccfc2afb44bb573bb9629a",
		Revision: "prism-b10743-adfffbe", Platform: "darwin/arm64",
	},
	{
		ID: "engine-macos-x64", Kind: CatalogEngine, Title: "llama-server · macOS x64",
		Family: "llama-cpp", URL: catalogEngineBase + "llama-prism-b10743-adfffbe-bin-macos-x64.tar.gz",
		Bytes: 11559738, SHA256: "3527598651d783b66d525afbcdd0cbbba12a8d59c388bb0e13fa85715381969d",
		Revision: "prism-b10743-adfffbe", Platform: "darwin/amd64",
	},
	{
		ID: "engine-linux-x64", Kind: CatalogEngine, Title: "llama-server · Linux x64",
		Family: "llama-cpp", URL: catalogEngineBase + "llama-prism-b10743-adfffbe-bin-ubuntu-x64.tar.gz",
		Bytes: 17779725, SHA256: "1bb340929fddae8667c97ec6d4064a5768fe1dd08eeee0074b8d5e08f07dfc31",
		Revision: "prism-b10743-adfffbe", Platform: "linux/amd64",
	},
	{
		ID: "engine-linux-arm64", Kind: CatalogEngine, Title: "llama-server · Linux arm64",
		Family: "llama-cpp", URL: catalogEngineBase + "llama-prism-b10743-adfffbe-bin-ubuntu-arm64.tar.gz",
		Bytes: 13800344, SHA256: "ee4cb83a2985572459a5686ef175503d4e526b46b97c7fcc7152e868169b3060",
		Revision: "prism-b10743-adfffbe", Platform: "linux/arm64",
	},
	{
		ID: "engine-windows-x64", Kind: CatalogEngine, Title: "llama-server · Windows CPU x64",
		Family: "llama-cpp", URL: catalogEngineBase + "llama-prism-b10743-adfffbe-bin-win-cpu-x64.zip",
		Bytes: 19441568, SHA256: "d0b3016c9cc4bc1385de68be034adee570277ba952dd94292ba3888b7f18cc44",
		Revision: "prism-b10743-adfffbe", Platform: "windows/amd64",
	},
	{
		ID: "model-smollm2-135m", Kind: CatalogModel, Title: "SmolLM2-135M-Instruct-Q4_K_M（GGUF）",
		Family: "llama-cpp", URL: "https://huggingface.co/unsloth/SmolLM2-135M-Instruct-GGUF/blob/9e6855bc4be717fca1ef21360a1db4b29d5c559a/SmolLM2-135M-Instruct-Q4_K_M.gguf",
		Bytes: 105454144, SHA256: "ed5fa30c487b282ec156c29062f1222e5c20875a944ac98289dbd242e947f747",
		Revision: "9e6855bc4be717fca1ef21360a1db4b29d5c559a", Platform: "any",
	},
	{
		ID: "model-qwen3-0.6b-q8", Kind: CatalogModel, Title: "Qwen3-0.6B-Q8_0（GGUF）",
		Family: "llama-cpp", URL: "https://huggingface.co/Qwen/Qwen3-0.6B-GGUF/blob/23749fefcc72300e3a2ad315e1317431b06b590a/Qwen3-0.6B-Q8_0.gguf",
		Bytes: 639446688, SHA256: "9465e63a22add5354d9bb4b99e90117043c7124007664907259bd16d043bb031",
		Revision: "23749fefcc72300e3a2ad315e1317431b06b590a", Platform: "any",
	},
	{
		ID: "engine-windows-cuda-x64", Kind: CatalogEngine, Title: "llama-server · Windows CUDA 12.4 x64",
		Family: "llama-cpp", URL: catalogEngineBase + "llama-prism-b10743-adfffbe-bin-win-cuda-12.4-x64.zip",
		Bytes: 257322810, SHA256: "1b849f713bee42fda258de83770cd422e8f48dd631ce370eb0641f6458c69d87",
		Revision: "prism-b10743-adfffbe", Platform: "windows/amd64",
		Reason: "归档 257 MB 超过 64 MiB 下载上限，Studio 引擎通道取不到。",
		Doc:    "cli/docs/contracts/windows-prism-batch-contract.md",
	},
	{
		ID: "runtime-windows-cuda-cudart", Kind: CatalogEngine, Title: "CUDA 运行时 · Windows CUDA 12.4 x64",
		Family: "llama-cpp", URL: catalogEngineBase + "cudart-llama-bin-win-cuda-12.4-x64.zip",
		Bytes: 391443627, SHA256: "8c79a9b226de4b3cacfd1f83d24f962d0773be79f1e7b75c6af4ded7e32ae1d6",
		Revision: "prism-b10743-adfffbe", Platform: "windows/amd64",
		Reason: "运行时归档 391 MB 超过 64 MiB 下载上限，且不是引擎可执行文件。",
		Doc:    "cli/docs/contracts/windows-prism-batch-contract.md",
	},
	{
		ID: "engine-mlx-lm", Kind: CatalogEngine, Title: "mlx-lm 0.32.2",
		Family: "mlx-lm", URL: "https://files.pythonhosted.org/packages/d1/4f/000eefdeb41388782b47e7f39904cf2d9e7e49a60df5e172097c358dba8e/mlx_lm-0.32.2-py3-none-any.whl",
		Bytes: 546040, SHA256: "5c0991248cfff5872503d74f466d2532dd8cb7fb3670cb9be7a06d7305dadc04",
		Revision: "0.32.2", Platform: "darwin/arm64",
		Reason: "Python 轮子是安装依赖，不是引擎归档；Studio 引擎通道只解包 llama.cpp 归档。",
		Doc:    "cli/docs/local-engine-native-preparation.md",
	},
	{
		ID: "runtime-mlx", Kind: CatalogEngine, Title: "mlx 0.32.4",
		Family: "mlx-lm", URL: "https://files.pythonhosted.org/packages/6d/92/7a999dcf6460525a31e555905cdca2f835ec0d786c528e969e04cd00d158/mlx-0.32.4-cp312-cp312-macosx_15_0_arm64.whl",
		Bytes: 667599, SHA256: "f0604cb3015091f285c79c7b7dbfcabcfa9948248728c7bab6e0263513c3d3fe",
		Revision: "0.32.4", Platform: "darwin/arm64",
		Reason: "Python 轮子是安装依赖，不是引擎归档；Studio 引擎通道只解包 llama.cpp 归档。",
		Doc:    "cli/docs/local-engine-native-preparation.md",
	},
	{
		ID: "engine-vllm", Kind: CatalogEngine, Title: "vllm 0.31.0 (CPU)",
		Family: "vllm", URL: "https://github.com/vllm-project/vllm/releases/download/v0.31.0/vllm-0.31.0%2Bcpu-cp312-cp312-macosx_11_0_arm64.whl",
		Bytes: 29500084, SHA256: "21378516a51ff15029f71168e827b94660e21b39484eb52277166f01ac7dd686",
		Revision: "v0.31.0", Platform: "darwin/arm64",
		Reason: "Python 轮子是安装依赖，不是引擎归档；Studio 引擎通道只解包 llama.cpp 归档。",
		Doc:    "cli/docs/local-engine-native-preparation.md",
	},
	{
		ID: "engine-vllm-metal", Kind: CatalogEngine, Title: "vllm-metal 0.31.0",
		Family: "vllm", URL: "https://github.com/vllm-project/vllm-metal/releases/download/v0.31.0/vllm_metal-0.31.0-cp312-cp312-macosx_15_0_arm64.whl",
		Bytes: 4290205, SHA256: "e2ab31296f8c42d8b65c8d5f5490f160087d1070178444870a5672e44aaacf6e",
		Revision: "v0.31.0", Platform: "darwin/arm64",
		Reason: "Python 轮子是安装依赖，不是引擎归档；Studio 引擎通道只解包 llama.cpp 归档。",
		Doc:    "cli/docs/local-engine-native-preparation.md",
	},
	{
		ID: "engine-sglang", Kind: CatalogEngine, Title: "SGLang 源码",
		Family: "sglang", URL: "https://github.com/sgl-project/sglang/archive/e262b44eef1f2bf83e34345276e2bb91a086587e.tar.gz",
		Bytes: 39545342, SHA256: "75da22de24091dfb7b8758deae145bc62f9b569d88b216c5899d01d61c032290",
		Revision: "e262b44eef1f2bf83e34345276e2bb91a086587e", Platform: "any",
		Reason: "源码归档不是引擎可执行文件；GitHub 生成的归档字节会随生成方式变化，摘要只对应本次快照。",
		Doc:    "cli/docs/local-engine-native-preparation.md",
	},
	{
		ID: "engine-lmstudio", Kind: CatalogEngine, Title: "LM Studio",
		Family: "lmstudio", URL: "https://lmstudio.ai/download",
		Platform: "any",
		Reason:   "LM Studio 由操作者自行安装并加载模型，Studio 不下载也不接管它的后台进程。",
		Doc:      "cli/docs/contracts/local-engines.md",
	},
	{
		ID: "model-qwen3-0.6b-mlx", Kind: CatalogModel, Title: "Qwen3-0.6B-4bit（目录模型）",
		Family: "mlx-lm", URL: "https://huggingface.co/mlx-community/Qwen3-0.6B-4bit/blob/73e3e38d981303bc594367cd910ea6eb48349da8/model.safetensors",
		Bytes: 335450584, SHA256: "392e8d466d56100ada00eb82031fb854297fc9e389b7d303eba3af114e87bce2",
		Revision: "73e3e38d981303bc594367cd910ea6eb48349da8", Platform: "darwin/arm64",
		Reason: "目录模型要按目录获取单个文件放错位置；模型通道只接受固定 revision 的单个 GGUF。",
		Doc:    "cli/docs/contracts/local-engines.md",
	},
}
