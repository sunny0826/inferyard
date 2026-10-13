# Prism 协议解析样本

来源为基线 `c0fd763` 的
`validation/bonsai2-adapter/probe-20260929T102859Z/`。
这些是已保存的协议解析输入，测试不启动服务或发送请求，不代表当前引擎验收。

`stream.sse`、`slots-idle.json`、`tokenize.json`、`ordinary.json` 原样复制。
`slots-during.json` 只取原数组中第一个 `is_processing=true` 的完整采样对象
（原数组第 2 项）；对象字节保持原样，仅加独立 JSON 数组包裹和末尾换行。
未保留其余重复采样，总样本大小为 11122 bytes。
`.gitattributes` 禁用换行转换。

| 文件 | SHA-256 |
| --- | --- |
| stream.sse | `bc076eeb5186484790cad1fa2cd8711a77352824a5ec0ad2b66915f860a66b97` |
| slots-idle.json | `96626365e23ce0eb7cdb00b21c625e5c0e2521b5483ba1ff736be60629dc149c` |
| slots-during.json | `2c0315855ef3082777e06867bf4e5f09a6ec92eb8115753c5f229fdaca8ec7a6` |
| tokenize.json | `806f0dc581663a67c916d0801122bd4a438da37198342248328b0e993d3148ec` |
| ordinary.json | `d7e34ee1a82275e102eeecdd5cb48f8ffbf16ffeaf92d243ca584af9fb42f039` |
