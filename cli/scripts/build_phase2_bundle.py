"""Build the original synthetic corpus and its unapproved review material."""

import argparse
import copy
import hashlib
import html
import json
from collections import Counter
from pathlib import Path

from inferyard.analysis.scoring import score_case
from inferyard.config.bundle import content_hash
from inferyard.contracts.validation import validate_document

ROOT = Path(__file__).resolve().parents[1]
QA = [
    ("青禾站的失物招领处在二层。售票处在一层。失物招领处在哪一层？", "二层"),
    ("松果小组周一由林悦值班，周二由周宁值班。周二谁值班？", "周宁"),
    ("小站只在周三和周六接收旧报纸，其余日期不接收。周四是否接收？只答“是”或“否”。", "否"),
    ("原计划在南门集合；通知改为在西门集合，时间不变。最终在哪个门集合？", "西门"),
    ("红盒编号K7，蓝盒编号K9，绿盒编号K3。蓝盒的编号是什么？", "K9"),
    ("甲车先到，丙车最后到，乙车在两者之间到达。第二个到达的是哪辆车？", "乙车"),
    ("短桥因维修关闭，行人须绕行长桥。短桥关闭的原因是什么？", "维修"),
    (
        "报名截止于5月12日，作品提交截止于5月19日。报名截止日期是什么？按“5月12日”格式回答。",
        "5月12日",
    ),
    (
        "活动每队4人。蓝队有林、王、赵、陈；赵临时退出，孙替补。最终蓝队是否包含赵？只答“是”或“否”。",
        "否",
    ),
    ("东库只存纸箱，西库只存木箱。需要领取木箱，应去哪座库？", "西库"),
    ("任务A依赖任务B完成，任务B依赖任务C完成。三个任务中必须最先完成哪个？", "任务C"),
    ("寄件单：收件人韩雪；寄件人杜林；经办人叶青。收件人是谁？", "韩雪"),
    ("小树在2024年加入小组，翌年转入绘画组。小树哪年转入绘画组？只答四位年份。", "2025"),
    (
        "一号柜钥匙交给了阿青；阿青随后将这把钥匙交给阿宁。按最后一条记录，谁持有一号柜钥匙？",
        "阿宁",
    ),
    ("预约记录只有房间号和使用日期，没有填写人数。能否从记录确定人数？只答“能”或“不能”。", "不能"),
    ("白卡可进入阅览室，黑卡可进入设备室；两者均不可进入仓库。哪种卡可进入设备室？", "黑卡"),
    ("雪芽茶每盒30元，山泉水每瓶3元。材料中哪件商品按盒出售？", "雪芽茶"),
    ("灯亮表示机器待机，灯闪表示运行，灯灭表示断电。目前灯闪。机器处于什么状态？", "运行"),
    ("会议原定14:00，后推迟30分钟。最终开始时间是什么？用24小时制HH:MM回答。", "14:30"),
    ("名册中小周负责摄影，小吴负责剪辑，小郑负责配音。谁负责剪辑？", "小吴"),
]
# Prompt, exact numerical reference, required unit, absolute tolerance.
MATH = [
    ("仓库有17盒彩笔，收到8盒又发出9盒，剩几盒？", 16, "盒", 0),
    ("每袋6颗糖，4袋共几颗？", 24, "颗", 0),
    ("45张卡片平均分给5人，每人几张？", 9, "张", 0),
    ("一件商品原价80元，按原价的75%出售，售价多少元？", 60, "元", 0),
    ("走完3千米用了30分钟，平均每分钟多少米？", 100, "米/分钟", 0),
    ("长方形长8厘米、宽5厘米，周长多少厘米？", 26, "厘米", 0),
    ("长方形长8厘米、宽5厘米，面积多少平方厘米？", 40, "平方厘米", 0),
    ("温度从零下3度升高8度，最终温度是多少度？", 5, "度", 0),
    ("2.5升水倒出0.75升，剩余多少升？", 1.75, "升", 0),
    ("三个数4、7、10的平均数是多少？", 7, "", 0),
    ("某等差数列前三项为2、5、8，第六项是多少？", 17, "", 0),
    ("甲比乙多4张票，两人共有20张票，甲有几张票？", 12, "张", 0),
    ("今天是星期二，10天后是星期几？只用数字1至7表示周一至周日。", 5, "", 0),
    ("时钟从09:40走到11:05，经过多少分钟？", 85, "分钟", 0),
    (
        "有红球3个、蓝球2个，每个球被等概率抽到。抽到蓝球的概率是多少？用0到1的小数回答。",
        0.4,
        "",
        0,
    ),
    ("一个数乘3再加2得到23，这个数是多少？", 7, "", 0),
    ("七个人每两个人握手一次，共握手多少次？", 21, "次", 0),
    ("17个零件每盒最多装5个，装完至少需要几个盒子？", 4, "个", 0),
    ("计算10除以3，结果保留两位小数。", 3.33, "", 0.005),
    ("数列1、1、2、3、5中，从第三项起每项等于前两项之和。第八项是多少？", 21, "", 0),
]
LABELS = ["物流", "账务", "技术", "其他"]
CLASSIFICATION = {
    "物流": [
        "包裹三天没有更新运输位置，请查询。",
        "快递显示签收，但收件处没有包裹。",
        "货物预计什么时候送到？",
        "寄出的包裹一直停留在分拣中心。",
        "快递员把包裹送到了隔壁楼，请协助找回。",
    ],
    "账务": [
        "同一笔订单被扣款两次，请核对。",
        "请为这笔已付款订单开具发票。",
        "退款已申请七天，金额还没有到账。",
        "付款账单上的总金额与订单不一致。",
        "请修改发票上的购买方抬头。",
    ],
    "技术": [
        "应用打开后立即闪退，无法操作。",
        "点击保存按钮后界面报错。",
        "网页图片无法加载，刷新也无效。",
        "软件登录后一直白屏。",
        "新版本安装时提示文件损坏。",
    ],
    "其他": [
        "你们周末的营业时间是什么？",
        "我想了解线下门店的位置。",
        "感谢客服耐心解答，服务很好。",
        "请问是否正在招聘客服人员？",
        "我希望下次推出绿色款式。",
    ],
}
# Each scenario specifies its entire output shape; no keys or facts are left implicit.
STRUCTURED = [
    (
        "联系人叫林青，城市为杭州，邮编未知。输出name字符串和address对象；address含city字符串、zip空值。",
        {"name": "林青", "address": {"city": "杭州", "zip": None}},
    ),
    (
        "订单编号A17，已付款，未发货。输出id字符串和status对象；status含paid、shipped两个布尔值。",
        {"id": "A17", "status": {"paid": True, "shipped": False}},
    ),
    (
        "购物清单按顺序为苹果2个、梨3个。输出items数组，每项含name字符串、count整数。",
        {"items": [{"name": "苹果", "count": 2}, {"name": "梨", "count": 3}]},
    ),
    (
        "两次温度读数按顺序为零下2度、5度。输出readings数值数组和unit字符串“度”。",
        {"readings": [-2, 5], "unit": "度"},
    ),
    (
        "会议标题为周会，没有参会者。输出meeting对象，含title字符串和attendees空数组。",
        {"meeting": {"title": "周会", "attendees": []}},
    ),
    (
        "会员编号U03，有效会员，积分为0。输出user对象，含id字符串、active布尔值、points整数。",
        {"user": {"id": "U03", "active": True, "points": 0}},
    ),
    (
        "任务名为备份，状态为进行中。输出task对象，含name和state；state使用枚举：待办todo、进行中doing、完成done。",
        {"task": {"name": "备份", "state": "doing"}},
    ),
    (
        "矩阵第一行为1、2，第二行为3、4。输出matrix，值为按行排列的二维整数数组。",
        {"matrix": [[1, 2], [3, 4]]},
    ),
    (
        "两名队员按顺序为林甲、陈乙；林甲是队长，陈乙不是。输出members数组，每项含name字符串和captain布尔值。",
        {"members": [{"name": "林甲", "captain": True}, {"name": "陈乙", "captain": False}]},
    ),
    (
        "文件夹名为资料，包含文件a.txt和b.txt，顺序不变。输出folder对象，含name字符串、files字符串数组。",
        {"folder": {"name": "资料", "files": ["a.txt", "b.txt"]}},
    ),
    (
        "坐标横向为1.5，纵向为零下2.5。输出point对象，含x、y两个数值。",
        {"point": {"x": 1.5, "y": -2.5}},
    ),
    (
        "库存中“笔”为12，“本”为0。输出stock对象，其键恰为“笔”和“本”，值为整数。",
        {"stock": {"笔": 12, "本": 0}},
    ),
    (
        "错误记录：代码E2，信息为超时，可以重试。输出error对象，含code、message两个字符串和retryable布尔值。",
        {"error": {"code": "E2", "message": "超时", "retryable": True}},
    ),
    (
        "课程名为绘画，教师未知，教室为B2。输出course对象，含name字符串、teacher空值、room字符串。",
        {"course": {"name": "绘画", "teacher": None, "room": "B2"}},
    ),
    (
        "两个标签按原顺序为“重要”“待办”，保留顺序。输出meta对象，含tags字符串数组和archived布尔值false。",
        {"meta": {"tags": ["重要", "待办"], "archived": False}},
    ),
    (
        "商品编号P8，价格12.5元，数量2。输出product对象，含id字符串、price数值、quantity整数。",
        {"product": {"id": "P8", "price": 12.5, "quantity": 2}},
    ),
    (
        "路线依次经过东站、南站、西站。输出route数组，每项含stop字符串和从1开始的order整数。",
        {
            "route": [
                {"stop": "东站", "order": 1},
                {"stop": "南站", "order": 2},
                {"stop": "西站", "order": 3},
            ]
        },
    ),
    (
        "原始字段名为a/b，值为启用；原始字段名为x~y，值为false。输出fields对象，保留这两个原始字段名。",
        {"fields": {"a/b": "启用", "x~y": False}},
    ),
    (
        "区间下界为0，上界为10；包含下界，不包含上界。输出range对象，含min、max数值和includeMin、includeMax布尔值。",
        {"range": {"min": 0, "max": 10, "includeMin": True, "includeMax": False}},
    ),
    (
        "调查有两条回答，顺序为：题号1回答“是”；题号2未作答。输出answers数组，每项含question整数和value，未作答用null。",
        {"answers": [{"question": 1, "value": "是"}, {"question": 2, "value": None}]},
    ),
]


def dumps(value):
    return json.dumps(value, ensure_ascii=False, indent=2) + "\n"


def shape(value):
    """Schema checks shape and types; scored fields check exact semantics separately."""
    if isinstance(value, dict):
        return dict(
            type="object",
            properties={k: shape(v) for k, v in value.items()},
            required=list(value),
            additionalProperties=False,
        )
    if isinstance(value, list):
        if not value:
            return dict(type="array", items=False, maxItems=0)
        return dict(
            type="array",
            prefixItems=[shape(v) for v in value],
            items=False,
            minItems=len(value),
            maxItems=len(value),
        )
    return {
        "type": {
            str: "string",
            int: "integer",
            float: "number",
            bool: "boolean",
            type(None): "null",
        }[type(value)]
    }


def fields(value, path=""):
    if isinstance(value, dict) and value:
        return [
            p
            for k, v in value.items()
            for p in fields(v, path + "/" + k.replace("~", "~0").replace("/", "~1"))
        ]
    if isinstance(value, list) and value:
        return [p for i, v in enumerate(value) for p in fields(v, path + "/" + str(i))]
    return [path]


def new_cases():
    result = []

    def add(category, prompt, answer, rules):
        index = sum(c["category"] == category for c in result) + 1
        result.append(
            dict(
                case_id=f"{category}-{index:02}",
                category=category,
                prompt=prompt,
                reference_answer=answer,
                rules=rules,
            )
        )

    for prompt, answer in QA:
        add(
            "qa",
            "仅根据材料回答，只输出答案，不加解释。\n" + prompt,
            answer,
            dict(answers=[answer], normalization="strip"),
        )
    for prompt, expected, unit, tolerance in MATH:
        instruction = (
            f"只输出数值及单位“{unit}”，不加解释。" if unit else "只输出数值，不加单位和解释。"
        )
        add(
            "math",
            prompt + "\n" + instruction,
            str(expected) + unit,
            dict(expected=expected, unit=unit, absolute_tolerance=tolerance, relative_tolerance=0),
        )
    # Interleave labels to avoid a contiguous category-answer pattern.
    for index in range(5):
        for label in LABELS:
            add(
                "classification",
                "将以下客服消息归类，只输出一个标签。\n"
                "物流：运输、派送、收货问题；账务：扣款、退款、发票问题；"
                "技术：软件故障；其他：不属于前三类的消息。\n消息：" + CLASSIFICATION[label][index],
                label,
                dict(labels=LABELS, expected=label),
            )
    for prompt, expected in STRUCTURED:
        schema = shape(expected)
        if "task" in expected:
            schema["properties"]["task"]["properties"]["state"]["enum"] = ["todo", "doing", "done"]
        add(
            "structured",
            prompt + "\n只输出合法JSON，不加Markdown或解释；字段不可增删，数组保持规定顺序。",
            json.dumps(expected, ensure_ascii=False),
            dict(
                json_schema=schema,
                expected=expected,
                fields=fields(expected),
                array_matching="ordered",
            ),
        )
    return result


def boundary_examples(task):
    answer, category = task["reference_answer"], task["category"]
    accepted = " \n" + answer + "\n "
    if category == "qa":
        rejected, reason = answer + "。", "仅去首尾空白；添加标点不视为精确匹配"
    elif category == "math":
        r = task["rules"]
        rejected = str(r["expected"] + max(1, r["absolute_tolerance"] * 10)) + r["unit"]
        reason = "数值超出冻结容差；单位必须完整匹配"
    elif category == "classification":
        rejected, reason = "类别：" + answer, "只接受冻结标签；额外前缀属于无效标签"
    else:
        rejected, reason = (
            "```json\n" + answer + "\n```",
            "Markdown围栏不属于JSON；解析失败时所有字段保留分母",
        )
    return [
        dict(answer=accepted, expected_state="pass", reason="首尾空白允许"),
        dict(answer=rejected, expected_state="fail", reason=reason),
    ]


def build():
    original = json.loads((ROOT / "bundles/zh-smoke.json").read_text())
    bundle = copy.deepcopy(original)
    bundle.pop("review_provenance", None)
    bundle.update(
        schema_version=3,
        bundle_id="zh-core-v2",
        version="2.0.0-draft.1",
        review_records=[],
        task_protocol="quality",
    )
    bundle["cases"].extend(new_cases())
    validate_document("bundle", bundle)
    boundaries = {c["case_id"]: boundary_examples(c) for c in bundle["cases"][40:]}
    for task in bundle["cases"]:
        assert (
            score_case(task, task["reference_answer"], bundle["answer_policy"])["quality_state"]
            == "pass"
        )
        for example in boundaries.get(task["case_id"], []):
            assert (
                score_case(task, example["answer"], bundle["answer_policy"])["quality_state"]
                == example["expected_state"]
            )
    return bundle, boundaries


def render(bundle, boundaries):
    escape = html.escape
    digest = content_hash(bundle)
    sections = []
    names = dict(qa="材料问答", math="数学与逻辑", classification="文本分类", structured="嵌套JSON")
    for task in bundle["cases"][40:]:
        cid = task["case_id"]
        sections.append(
            f'<details data-category="{task["category"]}">'
            f"<summary>{cid} · {names[task['category']]}</summary>"
            f"<h3>题目</h3><pre>{escape(task['prompt'])}</pre>"
            f"<h3>标准答案</h3><pre>{escape(task['reference_answer'])}</pre>"
            f"<h3>边界样例</h3><pre>{escape(dumps(boundaries[cid]))}</pre>"
            f"<details><summary>完整评分规则</summary><pre>{escape(dumps(task['rules']))}</pre></details></details>"
        )
    template = (ROOT / "scripts/phase2_review.html").read_text()
    values = {
        "DIGEST": digest,
        "BOUNDARIES": hashlib.sha256(dumps(boundaries).encode()).hexdigest(),
        "OPTIONS": "".join(f'<option value="{k}">{v} · 20题</option>' for k, v in names.items()),
        "SECTIONS": "".join(sections),
    }
    for key, value in values.items():
        template = template.replace("@@" + key + "@@", value)
    return template


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    bundle, boundaries = build()
    outputs = {
        "bundles/zh-core.json": dumps(bundle),
        "bundles/zh-core-v2.boundaries.json": dumps(boundaries),
        "bundles/zh-core-review.html": render(bundle, boundaries),
    }
    for name, text in outputs.items():
        path = ROOT / name
        if args.check:
            # Review records are human-owned; regeneration cannot remove or invent them.
            if name.endswith("zh-core.json"):
                existing = json.loads(path.read_text())
                existing["review_records"] = []
                assert existing == bundle, name
            else:
                assert path.read_text() == text, name
        else:
            if name.endswith("zh-core.json") and path.exists():
                existing = json.loads(path.read_text())
                if existing.get("review_records"):
                    raise SystemExit(
                        "Refusing to overwrite a reviewed bundle; create a new version."
                    )
            path.write_text(text)
    print(
        dumps(
            dict(
                content_sha256=content_hash(bundle),
                categories=Counter(c["category"] for c in bundle["cases"]),
                new_questions=80,
                human_review="pending",
            )
        )
    )


if __name__ == "__main__":
    main()
