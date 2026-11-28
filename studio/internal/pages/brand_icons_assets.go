package pages

import (
	_ "embed"

	"github.com/egoist/mygo/ui"
)

// Engine brand icons.

//go:embed brand_icons/llamacpp.svg
var brandLlamaCppSVG []byte

//go:embed brand_icons/polystrata-color.svg
var brandPolyStrataSVG []byte

//go:embed brand_icons/mlx.svg
var brandMLXSVG []byte

//go:embed brand_icons/vllm-color.svg
var brandVLLMSVG []byte

//go:embed brand_icons/ollama.svg
var brandOllamaSVG []byte

//go:embed brand_icons/lmstudio.svg
var brandLMStudioSVG []byte

// Model family brand icons.

//go:embed brand_icons/qwen-color.svg
var brandQwenSVG []byte

//go:embed brand_icons/gemma-color.svg
var brandGemmaSVG []byte

//go:embed brand_icons/meta-color.svg
var brandMetaSVG []byte

//go:embed brand_icons/deepseek-color.svg
var brandDeepSeekSVG []byte

//go:embed brand_icons/huggingface-color.svg
var brandHuggingFaceSVG []byte

//go:embed brand_icons/zai.svg
var brandZaiSVG []byte

//go:embed brand_icons/liquid.svg
var brandLiquidSVG []byte

//go:embed brand_icons/mistral-color.svg
var brandMistralSVG []byte

//go:embed brand_icons/openai.svg
var brandOpenAISVG []byte

//go:embed brand_icons/microsoft-color.svg
var brandMicrosoftSVG []byte

//go:embed brand_icons/kimi-color.svg
var brandKimiSVG []byte

//go:embed brand_icons/minimax-color.svg
var brandMiniMaxSVG []byte

//go:embed brand_icons/hunyuan-color.svg
var brandHunyuanSVG []byte

//go:embed brand_icons/nvidia-color.svg
var brandNvidiaSVG []byte

//go:embed brand_icons/yi-color.svg
var brandYiSVG []byte

//go:embed brand_icons/openbmb-color.svg
var brandOpenBMBSVG []byte

//go:embed brand_icons/internlm-color.svg
var brandInternLMSVG []byte

//go:embed brand_icons/cohere-color.svg
var brandCohereSVG []byte

//go:embed brand_icons/doubao-color.svg
var brandDoubaoSVG []byte

//go:embed brand_icons/baidu-color.svg
var brandBaiduSVG []byte

//go:embed brand_icons/stepfun-color.svg
var brandStepFunSVG []byte

//go:embed brand_icons/baichuan-color.svg
var brandBaichuanSVG []byte

//go:embed brand_icons/ibm.svg
var brandIBMSVG []byte

//go:embed brand_icons/tii-color.svg
var brandTIISVG []byte

//go:embed brand_icons/xiaomimimo.svg
var brandXiaomiMiMoSVG []byte

//go:embed brand_icons/ling-color.svg
var brandLingSVG []byte

//go:embed brand_icons/longcat-color.svg
var brandLongCatSVG []byte

var (
	svgBrandLlamaCpp   = ui.MustParseSVG(brandLlamaCppSVG)
	svgBrandPolyStrata = ui.MustParseSVG(brandPolyStrataSVG)
	svgBrandMLX        = ui.MustParseSVG(brandMLXSVG)
	svgBrandVLLM       = ui.MustParseSVG(brandVLLMSVG)
	svgBrandOllama     = ui.MustParseSVG(brandOllamaSVG)
	svgBrandLMStudio   = ui.MustParseSVG(brandLMStudioSVG)

	svgBrandQwen        = ui.MustParseSVG(brandQwenSVG)
	svgBrandGemma       = ui.MustParseSVG(brandGemmaSVG)
	svgBrandMeta        = ui.MustParseSVG(brandMetaSVG)
	svgBrandDeepSeek    = ui.MustParseSVG(brandDeepSeekSVG)
	svgBrandHuggingFace = ui.MustParseSVG(brandHuggingFaceSVG)
	svgBrandZai         = ui.MustParseSVG(brandZaiSVG)
	svgBrandLiquid      = ui.MustParseSVG(brandLiquidSVG)
	svgBrandMistral     = ui.MustParseSVG(brandMistralSVG)
	svgBrandOpenAI      = ui.MustParseSVG(brandOpenAISVG)
	svgBrandMicrosoft   = ui.MustParseSVG(brandMicrosoftSVG)
	svgBrandKimi        = ui.MustParseSVG(brandKimiSVG)
	svgBrandMiniMax     = ui.MustParseSVG(brandMiniMaxSVG)
	svgBrandHunyuan     = ui.MustParseSVG(brandHunyuanSVG)
	svgBrandNvidia      = ui.MustParseSVG(brandNvidiaSVG)
	svgBrandYi          = ui.MustParseSVG(brandYiSVG)
	svgBrandOpenBMB     = ui.MustParseSVG(brandOpenBMBSVG)
	svgBrandInternLM    = ui.MustParseSVG(brandInternLMSVG)
	svgBrandCohere      = ui.MustParseSVG(brandCohereSVG)
	svgBrandDoubao      = ui.MustParseSVG(brandDoubaoSVG)
	svgBrandBaidu       = ui.MustParseSVG(brandBaiduSVG)
	svgBrandStepFun     = ui.MustParseSVG(brandStepFunSVG)
	svgBrandBaichuan    = ui.MustParseSVG(brandBaichuanSVG)
	svgBrandIBM         = ui.MustParseSVG(brandIBMSVG)
	svgBrandTII         = ui.MustParseSVG(brandTIISVG)
	svgBrandXiaomiMiMo  = ui.MustParseSVG(brandXiaomiMiMoSVG)
	svgBrandLing        = ui.MustParseSVG(brandLingSVG)
	svgBrandLongCat     = ui.MustParseSVG(brandLongCatSVG)
)
