# 中文40题包人工审核稿

状态：当前用户已明确“审核通过”；T06人工项完成。绑定哈希及来源见 本机原件 `validation/mvp/G1/human-review.md`（不随公开仓库分发）。T12真实完整测评与新服务重跑已通过，见本机 G2 原件 `validation/mvp/G2/README.md`（不随公开仓库分发）。

内容SHA-256（不含review_records）：`ca338348af5bee1e77a5be649ee11d7a6d51ddfaf1ae9d95f09d48c22f4ff8fc`。

请检查每道题的要求、参考输出和机器规则是否一致，以及字段类型/数值是否正确。
审核通过后记录审核人标签、UTC时间和上述内容hash；不要把自动评分自检当成人工审核。

## instruction-01

写一行中文，包含“星光”，不要包含“月亮”。

参考输出：

```text
星光照亮夜空。
```

评分规则：`{"literal_contains":["星光"],"literal_forbidden":["月亮"],"nonempty_line_count":1}`

## instruction-02

写一行中文，包含“苹果”和“香蕉”，不要包含“梨”。

参考输出：

```text
苹果和香蕉都在桌上。
```

评分规则：`{"literal_contains":["苹果","香蕉"],"literal_forbidden":["梨"],"nonempty_line_count":1}`

## instruction-03

写两行，每行都非空；全文包含“春天”和“秋天”，不要包含“冬天”。

参考输出：

```text
春天花开。
秋天叶落。
```

评分规则：`{"literal_contains":["春天","秋天"],"literal_forbidden":["冬天"],"nonempty_line_count":2}`

## instruction-04

写三行，每行都非空；全文包含“红色”“蓝色”“绿色”。

参考输出：

```text
红色。
蓝色。
绿色。
```

评分规则：`{"literal_contains":["红色","蓝色","绿色"],"literal_forbidden":[],"nonempty_line_count":3}`

## instruction-05

写一行，包含字母串“ABC”，不要包含小写字母串“abc”。

参考输出：

```text
ABC
```

评分规则：`{"literal_contains":["ABC"],"literal_forbidden":["abc"],"nonempty_line_count":1}`

## instruction-06

写一行，包含数字串“2026”，不要包含“2025”。

参考输出：

```text
年份为2026。
```

评分规则：`{"literal_contains":["2026"],"literal_forbidden":["2025"],"nonempty_line_count":1}`

## instruction-07

写两行非空文本，全文包含“早安”和“晚安”，不要包含“午安”。

参考输出：

```text
早安。
晚安。
```

评分规则：`{"literal_contains":["早安","晚安"],"literal_forbidden":["午安"],"nonempty_line_count":2}`

## instruction-08

写一行中文，包含“安静”，不要包含“喧闹”和“吵闹”。

参考输出：

```text
这里很安静。
```

评分规则：`{"literal_contains":["安静"],"literal_forbidden":["喧闹","吵闹"],"nonempty_line_count":1}`

## instruction-09

写一行，包含“北京”和“中国”，不要包含“上海”。

参考输出：

```text
北京位于中国。
```

评分规则：`{"literal_contains":["北京","中国"],"literal_forbidden":["上海"],"nonempty_line_count":1}`

## instruction-10

写两行非空文本，全文包含“阅读”和“写作”。

参考输出：

```text
阅读书籍。
练习写作。
```

评分规则：`{"literal_contains":["阅读","写作"],"literal_forbidden":[],"nonempty_line_count":2}`

## instruction-11

写一行，包含完整短语“保持耐心”，不要包含“放弃”。

参考输出：

```text
请保持耐心。
```

评分规则：`{"literal_contains":["保持耐心"],"literal_forbidden":["放弃"],"nonempty_line_count":1}`

## instruction-12

写三行非空文本，全文包含“一”“二”“三”，不要包含“四”。

参考输出：

```text
一。
二。
三。
```

评分规则：`{"literal_contains":["一","二","三"],"literal_forbidden":["四"],"nonempty_line_count":3}`

## instruction-13

写一行，包含“猫”，不要包含“狗”。

参考输出：

```text
猫正在休息。
```

评分规则：`{"literal_contains":["猫"],"literal_forbidden":["狗"],"nonempty_line_count":1}`

## instruction-14

写两行非空文本，全文包含“开始”和“结束”，不要包含“中断”。

参考输出：

```text
开始工作。
结束工作。
```

评分规则：`{"literal_contains":["开始","结束"],"literal_forbidden":["中断"],"nonempty_line_count":2}`

## instruction-15

写一行，包含“山”和“海”，不要包含“河”。

参考输出：

```text
山与海相望。
```

评分规则：`{"literal_contains":["山","海"],"literal_forbidden":["河"],"nonempty_line_count":1}`

## instruction-16

写一行，包含“谢谢”，不要包含“抱歉”。

参考输出：

```text
谢谢你的帮助。
```

评分规则：`{"literal_contains":["谢谢"],"literal_forbidden":["抱歉"],"nonempty_line_count":1}`

## instruction-17

写三行非空文本，全文包含“计划”“执行”“复盘”。

参考输出：

```text
制定计划。
认真执行。
及时复盘。
```

评分规则：`{"literal_contains":["计划","执行","复盘"],"literal_forbidden":[],"nonempty_line_count":3}`

## instruction-18

写一行，包含大小写完全一致的“Local AI”，不要包含“Cloud AI”。

参考输出：

```text
Local AI
```

评分规则：`{"literal_contains":["Local AI"],"literal_forbidden":["Cloud AI"],"nonempty_line_count":1}`

## instruction-19

写两行非空文本，全文包含“输入”和“输出”，不要包含“错误”。

参考输出：

```text
准备输入。
检查输出。
```

评分规则：`{"literal_contains":["输入","输出"],"literal_forbidden":["错误"],"nonempty_line_count":2}`

## instruction-20

写一行，包含“完成”，不要包含“失败”和“重试”。

参考输出：

```text
任务完成。
```

评分规则：`{"literal_contains":["完成"],"literal_forbidden":["失败","重试"],"nonempty_line_count":1}`

## extraction-01

从以下文本提取信息，只返回一个JSON对象，不要代码围栏或解释。必须且仅包含字段：name、age。类型要求：name为string；age为integer。文本：姓名小明，年龄12岁。

参考输出：

```text
{"name":"小明","age":12}
```

评分规则：`{"fields":{"name":{"type":"string","value":"小明"},"age":{"type":"integer","value":12}},"allow_extra_fields":false}`

## extraction-02

从以下文本提取信息，只返回一个JSON对象，不要代码围栏或解释。必须且仅包含字段：city、country。类型要求：city为string；country为string。文本：城市北京，国家中国。

参考输出：

```text
{"city":"北京","country":"中国"}
```

评分规则：`{"fields":{"city":{"type":"string","value":"北京"},"country":{"type":"string","value":"中国"}},"allow_extra_fields":false}`

## extraction-03

从以下文本提取信息，只返回一个JSON对象，不要代码围栏或解释。必须且仅包含字段：product、quantity。类型要求：product为string；quantity为integer。文本：商品名称铅笔，数量3支。

参考输出：

```text
{"product":"铅笔","quantity":3}
```

评分规则：`{"fields":{"product":{"type":"string","value":"铅笔"},"quantity":{"type":"integer","value":3}},"allow_extra_fields":false}`

## extraction-04

从以下文本提取信息，只返回一个JSON对象，不要代码围栏或解释。必须且仅包含字段：order_id、paid。类型要求：order_id为string；paid为boolean。文本：订单编号A-102，已经付款。

参考输出：

```text
{"order_id":"A-102","paid":true}
```

评分规则：`{"fields":{"order_id":{"type":"string","value":"A-102"},"paid":{"type":"boolean","value":true}},"allow_extra_fields":false}`

## extraction-05

从以下文本提取信息，只返回一个JSON对象，不要代码围栏或解释。必须且仅包含字段：nickname、email。类型要求：nickname为string；email为null。文本：用户昵称小雨，没有提供电子邮箱。邮箱字段用null。

参考输出：

```text
{"nickname":"小雨","email":null}
```

评分规则：`{"fields":{"nickname":{"type":"string","value":"小雨"},"email":{"type":"null","value":null}},"allow_extra_fields":false}`

## extraction-06

从以下文本提取信息，只返回一个JSON对象，不要代码围栏或解释。必须且仅包含字段：title、pages。类型要求：title为string；pages为integer。文本：书名《春日》，页数128。书名不要包含书名号。

参考输出：

```text
{"title":"春日","pages":128}
```

评分规则：`{"fields":{"title":{"type":"string","value":"春日"},"pages":{"type":"integer","value":128}},"allow_extra_fields":false}`

## extraction-07

从以下文本提取信息，只返回一个JSON对象，不要代码围栏或解释。必须且仅包含字段：room、occupied。类型要求：room为string；occupied为boolean。文本：房间号B203，当前空闲。occupied字段表示是否已被占用。

参考输出：

```text
{"room":"B203","occupied":false}
```

评分规则：`{"fields":{"room":{"type":"string","value":"B203"},"occupied":{"type":"boolean","value":false}},"allow_extra_fields":false}`

## extraction-08

从以下文本提取信息，只返回一个JSON对象，不要代码围栏或解释。必须且仅包含字段：temperature、location。类型要求：temperature为number；location为string。文本：温度为12.5摄氏度，测量地点为室内。

参考输出：

```text
{"temperature":12.5,"location":"室内"}
```

评分规则：`{"fields":{"temperature":{"type":"number","value":12.5},"location":{"type":"string","value":"室内"}},"allow_extra_fields":false}`

## extraction-09

从以下文本提取信息，只返回一个JSON对象，不要代码围栏或解释。必须且仅包含字段：filename、size_bytes。类型要求：filename为string；size_bytes为integer。文本：文件名notes.txt，文件大小为0字节。

参考输出：

```text
{"filename":"notes.txt","size_bytes":0}
```

评分规则：`{"fields":{"filename":{"type":"string","value":"notes.txt"},"size_bytes":{"type":"integer","value":0}},"allow_extra_fields":false}`

## extraction-10

从以下文本提取信息，只返回一个JSON对象，不要代码围栏或解释。必须且仅包含字段：date、time。类型要求：date为string；time为string。文本：会议日期2026-09-29，开始时间09:30。

参考输出：

```text
{"date":"2026-09-29","time":"09:30"}
```

评分规则：`{"fields":{"date":{"type":"string","value":"2026-09-29"},"time":{"type":"string","value":"09:30"}},"allow_extra_fields":false}`

## extraction-11

从以下文本提取信息，只返回一个JSON对象，不要代码围栏或解释。必须且仅包含字段：recipient、packages、received。类型要求：recipient为string；packages为integer；received为boolean。文本：收件人李华，包裹共2件，尚未签收。

参考输出：

```text
{"recipient":"李华","packages":2,"received":false}
```

评分规则：`{"fields":{"recipient":{"type":"string","value":"李华"},"packages":{"type":"integer","value":2},"received":{"type":"boolean","value":false}},"allow_extra_fields":false}`

## extraction-12

从以下文本提取信息，只返回一个JSON对象，不要代码围栏或解释。必须且仅包含字段：course、score。类型要求：course为string；score为integer。文本：课程名称数学，成绩95分。

参考输出：

```text
{"course":"数学","score":95}
```

评分规则：`{"fields":{"course":{"type":"string","value":"数学"},"score":{"type":"integer","value":95}},"allow_extra_fields":false}`

## extraction-13

从以下文本提取信息，只返回一个JSON对象，不要代码围栏或解释。必须且仅包含字段：model、wifi_enabled。类型要求：model为string；wifi_enabled为boolean。文本：设备型号K1，已开启无线网络。

参考输出：

```text
{"model":"K1","wifi_enabled":true}
```

评分规则：`{"fields":{"model":{"type":"string","value":"K1"},"wifi_enabled":{"type":"boolean","value":true}},"allow_extra_fields":false}`

## extraction-14

从以下文本提取信息，只返回一个JSON对象，不要代码围栏或解释。必须且仅包含字段：author、year。类型要求：author为string；year为null。文本：作者名字为林，不知道出版年份。年份字段用null。

参考输出：

```text
{"author":"林","year":null}
```

评分规则：`{"fields":{"author":{"type":"string","value":"林"},"year":{"type":"null","value":null}},"allow_extra_fields":false}`

## extraction-15

从以下文本提取信息，只返回一个JSON对象，不要代码围栏或解释。必须且仅包含字段：price、currency。类型要求：price为number；currency为string。文本：商品价格19.5元，货币代码CNY。

参考输出：

```text
{"price":19.5,"currency":"CNY"}
```

评分规则：`{"fields":{"price":{"type":"number","value":19.5},"currency":{"type":"string","value":"CNY"}},"allow_extra_fields":false}`

## extraction-16

从以下文本提取信息，只返回一个JSON对象，不要代码围栏或解释。必须且仅包含字段：route、destination。类型要求：route为integer；destination为string。文本：路线编号7，终点站公园。

参考输出：

```text
{"route":7,"destination":"公园"}
```

评分规则：`{"fields":{"route":{"type":"integer","value":7},"destination":{"type":"string","value":"公园"}},"allow_extra_fields":false}`

## extraction-17

从以下文本提取信息，只返回一个JSON对象，不要代码围栏或解释。必须且仅包含字段：task_id、status。类型要求：task_id为string；status为string。文本：任务标识task-03，状态为完成。

参考输出：

```text
{"task_id":"task-03","status":"完成"}
```

评分规则：`{"fields":{"task_id":{"type":"string","value":"task-03"},"status":{"type":"string","value":"完成"}},"allow_extra_fields":false}`

## extraction-18

从以下文本提取信息，只返回一个JSON对象，不要代码围栏或解释。必须且仅包含字段：name、species、age。类型要求：name为string；species为string；age为integer。文本：宠物名字团子，品种为猫，年龄2岁。

参考输出：

```text
{"name":"团子","species":"猫","age":2}
```

评分规则：`{"fields":{"name":{"type":"string","value":"团子"},"species":{"type":"string","value":"猫"},"age":{"type":"integer","value":2}},"allow_extra_fields":false}`

## extraction-19

从以下文本提取信息，只返回一个JSON对象，不要代码围栏或解释。必须且仅包含字段：warehouse、stock。类型要求：warehouse为string；stock为integer。文本：仓库代号W2，可用库存为15件。

参考输出：

```text
{"warehouse":"W2","stock":15}
```

评分规则：`{"fields":{"warehouse":{"type":"string","value":"W2"},"stock":{"type":"integer","value":15}},"allow_extra_fields":false}`

## extraction-20

从以下文本提取信息，只返回一个JSON对象，不要代码围栏或解释。必须且仅包含字段：survey_id、consent、note。类型要求：survey_id为string；consent为boolean；note为null。文本：调查编号S9，同意参与，没有填写备注。备注字段用null。

参考输出：

```text
{"survey_id":"S9","consent":true,"note":null}
```

评分规则：`{"fields":{"survey_id":{"type":"string","value":"S9"},"consent":{"type":"boolean","value":true},"note":{"type":"null","value":null}},"allow_extra_fields":false}`
