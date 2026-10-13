# 旧版本迁移夹具

`synthetic-v1/`、`synthetic-v2/` 从基线 `c0fd763` 的
`validation/windows/portable-requests-20261001/` 同名目录逐字节复制。
共 244589 bytes，仅含每版三个正式请求的合成服务运行，不是真机测量资格。
保留全部 manifest 条目（包括旧派生报告），以覆盖完整封存、显式迁移、评分和
离线报告回放；测试不需要原 validation 目录或旧 Git 历史。

原 manifest 的 SHA-256：

| 包 | SHA-256 |
| --- | --- |
| synthetic-v1 | `317c5be8c87ce9a35487c463cd92f449b17d837e89f0bf01cb2e654fb3e3c0e5` |
| synthetic-v2 | `f4fecdc6a5f81e1470dffb09c6858f1333276cccde3a9183a36b2c2b32ae19c3` |

包内路径、源码身份和时间是历史合成数据，不作为当前机器配置读取。
`.gitattributes` 对夹具禁用换行转换；不要格式化或重封存这些文件。
拒绝路径测试只修改临时副本。Windows 复制测试把这些文件的原始字节写入临时
Git 对象库，并仅在临时 manifest/索引恢复带冒号的请求名，以覆盖真实别名复制路径。
