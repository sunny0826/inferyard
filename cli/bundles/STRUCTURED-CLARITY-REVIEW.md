# 嵌套 JSON 措辞修订 · 待人工审核

只审核下面 20 道题的措辞变化。其余 100 题、全部参考答案、评分规则及答案政策不变。
统一明确最外层为对象，以及必须保留的顶层字段；没有按模型失分挑选修订题。

候选为 `zh-core-clarified.json`，内容版本 `2.0.1-draft.1`，数据契约仍为 v3。
旧题包、原批准和已有 120 题结果保留；新题包不会自动取得原来的人工批准。

本次审核内容 SHA-256：`38897c71c1bc40a1ee1ffb56081e788557863c9c4788677ba30e0b15284c0844`。
原题包文件 SHA-256：`5296dd8c59613e1142a0e9ec887509ab8d7266f49f88a614d343188640fc15c6`。
未修改 100 题内容 SHA-256：`2f90fc8330eabffac1eaab878cddf0fa2e7a534f5b47fdc29b847f37f9183453`。

审核要点：新题意是否与参考答案和规则一致；顶层/内部字段是否明确；是否引入新事实。
标准答案仅用于审核，不会作为提示词或 JSON 强制约束发给模型。

<details><summary>structured-01 · 顶层字段 name / address</summary>

修改前：

```text
联系人叫林青，城市为杭州，邮编未知。输出name字符串和address对象；address含city字符串、zip空值。
只输出合法JSON，不加Markdown或解释；字段不可增删，数组保持规定顺序。
```

修改后：

```text
联系人叫林青，城市为杭州，邮编未知。输出一个JSON对象，顶层字段恰为name和address。name是字符串；address是对象，含city字符串和zip空值。
最外层必须保留上述字段名称，不能直接输出顶层字段的内部对象或数组。
只输出合法JSON，不加Markdown或解释；字段不可增删，数组保持规定顺序。
```

参考答案（不变）：

```json
{"name": "林青", "address": {"city": "杭州", "zip": null}}
```

评分仍严格检查 JSON、原 Schema 和全部字段；不允许省略外层字段。
完整规则位于候选题包及 `zh-core-clarified.review.json` 的哈希绑定中。

</details>

<details><summary>structured-02 · 顶层字段 id / status</summary>

修改前：

```text
订单编号A17，已付款，未发货。输出id字符串和status对象；status含paid、shipped两个布尔值。
只输出合法JSON，不加Markdown或解释；字段不可增删，数组保持规定顺序。
```

修改后：

```text
订单编号A17，已付款，未发货。输出一个JSON对象，顶层字段恰为id和status。id是字符串；status是对象，含paid、shipped两个布尔值。
最外层必须保留上述字段名称，不能直接输出顶层字段的内部对象或数组。
只输出合法JSON，不加Markdown或解释；字段不可增删，数组保持规定顺序。
```

参考答案（不变）：

```json
{"id": "A17", "status": {"paid": true, "shipped": false}}
```

评分仍严格检查 JSON、原 Schema 和全部字段；不允许省略外层字段。
完整规则位于候选题包及 `zh-core-clarified.review.json` 的哈希绑定中。

</details>

<details><summary>structured-03 · 顶层字段 items</summary>

修改前：

```text
购物清单按顺序为苹果2个、梨3个。输出items数组，每项含name字符串、count整数。
只输出合法JSON，不加Markdown或解释；字段不可增删，数组保持规定顺序。
```

修改后：

```text
购物清单按顺序为苹果2个、梨3个。输出一个JSON对象，顶层字段仅为items。items是数组，每项是含name字符串、count整数的对象。
最外层必须保留上述字段名称，不能直接输出顶层字段的内部对象或数组。
只输出合法JSON，不加Markdown或解释；字段不可增删，数组保持规定顺序。
```

参考答案（不变）：

```json
{"items": [{"name": "苹果", "count": 2}, {"name": "梨", "count": 3}]}
```

评分仍严格检查 JSON、原 Schema 和全部字段；不允许省略外层字段。
完整规则位于候选题包及 `zh-core-clarified.review.json` 的哈希绑定中。

</details>

<details><summary>structured-04 · 顶层字段 readings / unit</summary>

修改前：

```text
两次温度读数按顺序为零下2度、5度。输出readings数值数组和unit字符串“度”。
只输出合法JSON，不加Markdown或解释；字段不可增删，数组保持规定顺序。
```

修改后：

```text
两次温度读数按顺序为零下2度、5度。输出一个JSON对象，顶层字段恰为readings和unit。readings是数值数组；unit是字符串“度”。
最外层必须保留上述字段名称，不能直接输出顶层字段的内部对象或数组。
只输出合法JSON，不加Markdown或解释；字段不可增删，数组保持规定顺序。
```

参考答案（不变）：

```json
{"readings": [-2, 5], "unit": "度"}
```

评分仍严格检查 JSON、原 Schema 和全部字段；不允许省略外层字段。
完整规则位于候选题包及 `zh-core-clarified.review.json` 的哈希绑定中。

</details>

<details><summary>structured-05 · 顶层字段 meeting</summary>

修改前：

```text
会议标题为周会，没有参会者。输出meeting对象，含title字符串和attendees空数组。
只输出合法JSON，不加Markdown或解释；字段不可增删，数组保持规定顺序。
```

修改后：

```text
会议标题为周会，没有参会者。输出一个JSON对象，顶层字段仅为meeting。meeting是对象，含title字符串和attendees空数组。
最外层必须保留上述字段名称，不能直接输出顶层字段的内部对象或数组。
只输出合法JSON，不加Markdown或解释；字段不可增删，数组保持规定顺序。
```

参考答案（不变）：

```json
{"meeting": {"title": "周会", "attendees": []}}
```

评分仍严格检查 JSON、原 Schema 和全部字段；不允许省略外层字段。
完整规则位于候选题包及 `zh-core-clarified.review.json` 的哈希绑定中。

</details>

<details><summary>structured-06 · 顶层字段 user</summary>

修改前：

```text
会员编号U03，有效会员，积分为0。输出user对象，含id字符串、active布尔值、points整数。
只输出合法JSON，不加Markdown或解释；字段不可增删，数组保持规定顺序。
```

修改后：

```text
会员编号U03，有效会员，积分为0。输出一个JSON对象，顶层字段仅为user。user是对象，含id字符串、active布尔值、points整数。
最外层必须保留上述字段名称，不能直接输出顶层字段的内部对象或数组。
只输出合法JSON，不加Markdown或解释；字段不可增删，数组保持规定顺序。
```

参考答案（不变）：

```json
{"user": {"id": "U03", "active": true, "points": 0}}
```

评分仍严格检查 JSON、原 Schema 和全部字段；不允许省略外层字段。
完整规则位于候选题包及 `zh-core-clarified.review.json` 的哈希绑定中。

</details>

<details><summary>structured-07 · 顶层字段 task</summary>

修改前：

```text
任务名为备份，状态为进行中。输出task对象，含name和state；state使用枚举：待办todo、进行中doing、完成done。
只输出合法JSON，不加Markdown或解释；字段不可增删，数组保持规定顺序。
```

修改后：

```text
任务名为备份，状态为进行中。输出一个JSON对象，顶层字段仅为task。task是对象，含name和state字符串；state使用枚举：待办todo、进行中doing、完成done。
最外层必须保留上述字段名称，不能直接输出顶层字段的内部对象或数组。
只输出合法JSON，不加Markdown或解释；字段不可增删，数组保持规定顺序。
```

参考答案（不变）：

```json
{"task": {"name": "备份", "state": "doing"}}
```

评分仍严格检查 JSON、原 Schema 和全部字段；不允许省略外层字段。
完整规则位于候选题包及 `zh-core-clarified.review.json` 的哈希绑定中。

</details>

<details><summary>structured-08 · 顶层字段 matrix</summary>

修改前：

```text
矩阵第一行为1、2，第二行为3、4。输出matrix，值为按行排列的二维整数数组。
只输出合法JSON，不加Markdown或解释；字段不可增删，数组保持规定顺序。
```

修改后：

```text
矩阵第一行为1、2，第二行为3、4。输出一个JSON对象，顶层字段仅为matrix。matrix是按行排列的二维整数数组。
最外层必须保留上述字段名称，不能直接输出顶层字段的内部对象或数组。
只输出合法JSON，不加Markdown或解释；字段不可增删，数组保持规定顺序。
```

参考答案（不变）：

```json
{"matrix": [[1, 2], [3, 4]]}
```

评分仍严格检查 JSON、原 Schema 和全部字段；不允许省略外层字段。
完整规则位于候选题包及 `zh-core-clarified.review.json` 的哈希绑定中。

</details>

<details><summary>structured-09 · 顶层字段 members</summary>

修改前：

```text
两名队员按顺序为林甲、陈乙；林甲是队长，陈乙不是。输出members数组，每项含name字符串和captain布尔值。
只输出合法JSON，不加Markdown或解释；字段不可增删，数组保持规定顺序。
```

修改后：

```text
两名队员按顺序为林甲、陈乙；林甲是队长，陈乙不是。输出一个JSON对象，顶层字段仅为members。members是数组，每项是含name字符串和captain布尔值的对象。
最外层必须保留上述字段名称，不能直接输出顶层字段的内部对象或数组。
只输出合法JSON，不加Markdown或解释；字段不可增删，数组保持规定顺序。
```

参考答案（不变）：

```json
{"members": [{"name": "林甲", "captain": true}, {"name": "陈乙", "captain": false}]}
```

评分仍严格检查 JSON、原 Schema 和全部字段；不允许省略外层字段。
完整规则位于候选题包及 `zh-core-clarified.review.json` 的哈希绑定中。

</details>

<details><summary>structured-10 · 顶层字段 folder</summary>

修改前：

```text
文件夹名为资料，包含文件a.txt和b.txt，顺序不变。输出folder对象，含name字符串、files字符串数组。
只输出合法JSON，不加Markdown或解释；字段不可增删，数组保持规定顺序。
```

修改后：

```text
文件夹名为资料，包含文件a.txt和b.txt，顺序不变。输出一个JSON对象，顶层字段仅为folder。folder是对象，含name字符串和files字符串数组。
最外层必须保留上述字段名称，不能直接输出顶层字段的内部对象或数组。
只输出合法JSON，不加Markdown或解释；字段不可增删，数组保持规定顺序。
```

参考答案（不变）：

```json
{"folder": {"name": "资料", "files": ["a.txt", "b.txt"]}}
```

评分仍严格检查 JSON、原 Schema 和全部字段；不允许省略外层字段。
完整规则位于候选题包及 `zh-core-clarified.review.json` 的哈希绑定中。

</details>

<details><summary>structured-11 · 顶层字段 point</summary>

修改前：

```text
坐标横向为1.5，纵向为零下2.5。输出point对象，含x、y两个数值。
只输出合法JSON，不加Markdown或解释；字段不可增删，数组保持规定顺序。
```

修改后：

```text
坐标横向为1.5，纵向为零下2.5。输出一个JSON对象，顶层字段仅为point。point是对象，含x、y两个数值。
最外层必须保留上述字段名称，不能直接输出顶层字段的内部对象或数组。
只输出合法JSON，不加Markdown或解释；字段不可增删，数组保持规定顺序。
```

参考答案（不变）：

```json
{"point": {"x": 1.5, "y": -2.5}}
```

评分仍严格检查 JSON、原 Schema 和全部字段；不允许省略外层字段。
完整规则位于候选题包及 `zh-core-clarified.review.json` 的哈希绑定中。

</details>

<details><summary>structured-12 · 顶层字段 stock</summary>

修改前：

```text
库存中“笔”为12，“本”为0。输出stock对象，其键恰为“笔”和“本”，值为整数。
只输出合法JSON，不加Markdown或解释；字段不可增删，数组保持规定顺序。
```

修改后：

```text
库存中“笔”为12，“本”为0。输出一个JSON对象，顶层字段仅为stock。stock是对象，其键恰为“笔”和“本”，值为整数。
最外层必须保留上述字段名称，不能直接输出顶层字段的内部对象或数组。
只输出合法JSON，不加Markdown或解释；字段不可增删，数组保持规定顺序。
```

参考答案（不变）：

```json
{"stock": {"笔": 12, "本": 0}}
```

评分仍严格检查 JSON、原 Schema 和全部字段；不允许省略外层字段。
完整规则位于候选题包及 `zh-core-clarified.review.json` 的哈希绑定中。

</details>

<details><summary>structured-13 · 顶层字段 error</summary>

修改前：

```text
错误记录：代码E2，信息为超时，可以重试。输出error对象，含code、message两个字符串和retryable布尔值。
只输出合法JSON，不加Markdown或解释；字段不可增删，数组保持规定顺序。
```

修改后：

```text
错误记录：代码E2，信息为超时，可以重试。输出一个JSON对象，顶层字段仅为error。error是对象，含code、message两个字符串和retryable布尔值。
最外层必须保留上述字段名称，不能直接输出顶层字段的内部对象或数组。
只输出合法JSON，不加Markdown或解释；字段不可增删，数组保持规定顺序。
```

参考答案（不变）：

```json
{"error": {"code": "E2", "message": "超时", "retryable": true}}
```

评分仍严格检查 JSON、原 Schema 和全部字段；不允许省略外层字段。
完整规则位于候选题包及 `zh-core-clarified.review.json` 的哈希绑定中。

</details>

<details><summary>structured-14 · 顶层字段 course</summary>

修改前：

```text
课程名为绘画，教师未知，教室为B2。输出course对象，含name字符串、teacher空值、room字符串。
只输出合法JSON，不加Markdown或解释；字段不可增删，数组保持规定顺序。
```

修改后：

```text
课程名为绘画，教师未知，教室为B2。输出一个JSON对象，顶层字段仅为course。course是对象，含name字符串、teacher空值、room字符串。
最外层必须保留上述字段名称，不能直接输出顶层字段的内部对象或数组。
只输出合法JSON，不加Markdown或解释；字段不可增删，数组保持规定顺序。
```

参考答案（不变）：

```json
{"course": {"name": "绘画", "teacher": null, "room": "B2"}}
```

评分仍严格检查 JSON、原 Schema 和全部字段；不允许省略外层字段。
完整规则位于候选题包及 `zh-core-clarified.review.json` 的哈希绑定中。

</details>

<details><summary>structured-15 · 顶层字段 meta</summary>

修改前：

```text
两个标签按原顺序为“重要”“待办”，保留顺序。输出meta对象，含tags字符串数组和archived布尔值false。
只输出合法JSON，不加Markdown或解释；字段不可增删，数组保持规定顺序。
```

修改后：

```text
两个标签按原顺序为“重要”“待办”，保留顺序。输出一个JSON对象，顶层字段仅为meta。meta是对象，含tags字符串数组和archived布尔值false。
最外层必须保留上述字段名称，不能直接输出顶层字段的内部对象或数组。
只输出合法JSON，不加Markdown或解释；字段不可增删，数组保持规定顺序。
```

参考答案（不变）：

```json
{"meta": {"tags": ["重要", "待办"], "archived": false}}
```

评分仍严格检查 JSON、原 Schema 和全部字段；不允许省略外层字段。
完整规则位于候选题包及 `zh-core-clarified.review.json` 的哈希绑定中。

</details>

<details><summary>structured-16 · 顶层字段 product</summary>

修改前：

```text
商品编号P8，价格12.5元，数量2。输出product对象，含id字符串、price数值、quantity整数。
只输出合法JSON，不加Markdown或解释；字段不可增删，数组保持规定顺序。
```

修改后：

```text
商品编号P8，价格12.5元，数量2。输出一个JSON对象，顶层字段仅为product。product是对象，含id字符串、price数值、quantity整数。
最外层必须保留上述字段名称，不能直接输出顶层字段的内部对象或数组。
只输出合法JSON，不加Markdown或解释；字段不可增删，数组保持规定顺序。
```

参考答案（不变）：

```json
{"product": {"id": "P8", "price": 12.5, "quantity": 2}}
```

评分仍严格检查 JSON、原 Schema 和全部字段；不允许省略外层字段。
完整规则位于候选题包及 `zh-core-clarified.review.json` 的哈希绑定中。

</details>

<details><summary>structured-17 · 顶层字段 route</summary>

修改前：

```text
路线依次经过东站、南站、西站。输出route数组，每项含stop字符串和从1开始的order整数。
只输出合法JSON，不加Markdown或解释；字段不可增删，数组保持规定顺序。
```

修改后：

```text
路线依次经过东站、南站、西站。输出一个JSON对象，顶层字段仅为route。route是数组，每项是含stop字符串和从1开始的order整数的对象。
最外层必须保留上述字段名称，不能直接输出顶层字段的内部对象或数组。
只输出合法JSON，不加Markdown或解释；字段不可增删，数组保持规定顺序。
```

参考答案（不变）：

```json
{"route": [{"stop": "东站", "order": 1}, {"stop": "南站", "order": 2}, {"stop": "西站", "order": 3}]}
```

评分仍严格检查 JSON、原 Schema 和全部字段；不允许省略外层字段。
完整规则位于候选题包及 `zh-core-clarified.review.json` 的哈希绑定中。

</details>

<details><summary>structured-18 · 顶层字段 fields</summary>

修改前：

```text
原始字段名为a/b，值为启用；原始字段名为x~y，值为false。输出fields对象，保留这两个原始字段名。
只输出合法JSON，不加Markdown或解释；字段不可增删，数组保持规定顺序。
```

修改后：

```text
原始字段名为a/b，值为启用；原始字段名为x~y，值为false。输出一个JSON对象，顶层字段仅为fields。fields是对象，保留材料中的两个原始字段名。
最外层必须保留上述字段名称，不能直接输出顶层字段的内部对象或数组。
只输出合法JSON，不加Markdown或解释；字段不可增删，数组保持规定顺序。
```

参考答案（不变）：

```json
{"fields": {"a/b": "启用", "x~y": false}}
```

评分仍严格检查 JSON、原 Schema 和全部字段；不允许省略外层字段。
完整规则位于候选题包及 `zh-core-clarified.review.json` 的哈希绑定中。

</details>

<details><summary>structured-19 · 顶层字段 range</summary>

修改前：

```text
区间下界为0，上界为10；包含下界，不包含上界。输出range对象，含min、max数值和includeMin、includeMax布尔值。
只输出合法JSON，不加Markdown或解释；字段不可增删，数组保持规定顺序。
```

修改后：

```text
区间下界为0，上界为10；包含下界，不包含上界。输出一个JSON对象，顶层字段仅为range。range是对象，含min、max数值和includeMin、includeMax布尔值。
最外层必须保留上述字段名称，不能直接输出顶层字段的内部对象或数组。
只输出合法JSON，不加Markdown或解释；字段不可增删，数组保持规定顺序。
```

参考答案（不变）：

```json
{"range": {"min": 0, "max": 10, "includeMin": true, "includeMax": false}}
```

评分仍严格检查 JSON、原 Schema 和全部字段；不允许省略外层字段。
完整规则位于候选题包及 `zh-core-clarified.review.json` 的哈希绑定中。

</details>

<details><summary>structured-20 · 顶层字段 answers</summary>

修改前：

```text
调查有两条回答，顺序为：题号1回答“是”；题号2未作答。输出answers数组，每项含question整数和value，未作答用null。
只输出合法JSON，不加Markdown或解释；字段不可增删，数组保持规定顺序。
```

修改后：

```text
调查有两条回答，顺序为：题号1回答“是”；题号2未作答。输出一个JSON对象，顶层字段仅为answers。answers是数组，每项是含question整数和value的对象；未作答用null。
最外层必须保留上述字段名称，不能直接输出顶层字段的内部对象或数组。
只输出合法JSON，不加Markdown或解释；字段不可增删，数组保持规定顺序。
```

参考答案（不变）：

```json
{"answers": [{"question": 1, "value": "是"}, {"question": 2, "value": null}]}
```

评分仍严格检查 JSON、原 Schema 和全部字段；不允许省略外层字段。
完整规则位于候选题包及 `zh-core-clarified.review.json` 的哈希绑定中。

</details>

## 审核状态

状态为待审核。生成器没有创建批准记录，自动参考答案检查不代替人工审核。
审核通过后才记录当前用户的明确答复及上述内容哈希，再创建新的真实运行。
候选不会替换默认题包，旧分数也不会重算成新成绩。
