# 首页接口实测与筛选审计（2026-11-06）

## 1. 结论与证据范围

- 已验证：2026-11-06 23:30:08–23:32:18（北京时间），68 次真实请求均 HTTP 200、业务 code=0；63 个去重筛选 ID 逐个请求，均有正确的选中项回显并返回 6–9 部剧。
- 已验证：本次接口返回 10 个快捷项、4 个内部分组（2/6/34/17 项），共 69 个展示条目，去重后 63 个 ID；客户端当前均有入口，没有唯一 ID 遗漏。
- 已验证：响应声明 `global_single=true`，当前客户端的全局单选符合该声明。两个逗号组合均回显两个 ID，旧文档“只生效第一个”不能继续作为当前结论。
- 未验证：组合结果是否严格取交集、每部返回剧是否都符合题材、所有版本/身份/网络的长期可用性；不得把选中项回显视为这些语义已获证明。
- 本轮仅遍历本次首页响应提供的值，不代表整个上游分类体系；未验证其它分类接口历史所称的 224 个类别能否直接用于本接口。

本报告基于真实接口，而非按旧文档推演。请求是 macOS Python urllib 3.14.10 发送，非手机 HTTP 栈；参数取当前工作区配方 v5 与 `RecipeHttp.commonParams`。使用独立随机测试设备身份，没有读取手机账号、Cookie 或用户设备 ID。手机生产 Kotlin 链路的此前抽样验收另见 [配方实测记录](2026-11-06-recipe-hardening.md#6-当前接口实测2026-11-06)。

## 2. 首个请求与完整归档

最早的探索请求发生于约 23:18:02，其临时脚本没有保存完整随机身份和请求头，因此无法还原全部入参。下面的 `001-default` 是**正式完整取证轮的首个请求**，不冒称是整段会话的第一次网络请求。探索结果只用于发现测试范围，本文数字和表格均以正式轮原始证据为准。

```text
GET https://api5-normal-sinfonlinec.fqnovel.com/reading/bookapi/bookmall/tab/v/
```

- 发送时间：`2026-11-06T23:30:08.228102+08:00`；响应完成：`2026-11-06T23:30:09.950113+08:00`，耗时 1722 ms。
- 返回：HTTP 200，`code=0` / `message=SUCCESS`，53104 字节，9 个去重剧集。
- 正文 SHA-256：`be6758555a84a2f80c40b09fdc86af131f58a41b850011b19c4ac42743e28155`。
- 签名：`none`；请求体：无；没有 Cookie 或 Authorization；首次请求省略 `session_id`、`selected_items`。
- 完整 [请求 URL、全部查询参数和请求头](api-evidence/home-20261106/001-default/request.json)、[原始响应字节](api-evidence/home-20261106/001-default/response.body)、[格式化完整响应](api-evidence/home-20261106/001-default/response.pretty.json)、[响应状态/头/耗时/摘要](api-evidence/home-20261106/001-default/response.json)。

### 2.1 首次请求全部查询参数

| 参数 | 实际值 |
| --- | --- |
| `iid` | `4597645888547073714` |
| `device_id` | `2624759645868322053` |
| `ac` | `wifi` |
| `channel` | `hongguo_baidu_pz_kaiping0105_android` |
| `aid` | `8662` |
| `app_name` | `novelread` |
| `version_code` | `65532` |
| `version_name` | `6.5.5.32` |
| `device_platform` | `android` |
| `os` | `android` |
| `ssmix` | `a` |
| `device_type` | `Redmi Note 8 Pro` |
| `device_brand` | `Redmi` |
| `language` | `zh` |
| `os_api` | `36` |
| `os_version` | `16` |
| `manifest_version_code` | `65532` |
| `resolution` | `1080*2198` |
| `dpi` | `440` |
| `update_version_code` | `65532` |
| `pv_player` | `65532` |
| `gender` | `2` |
| `need_personal_recommend` | `1` |
| `player_so_load` | `1` |
| `is_android_pad_screen` | `0` |
| `host_abi` | `arm64-v8a` |
| `dragon_device_type` | `phone` |
| `rom_version` | `BP4A.251205.006 release-keys` |
| `compliance_status` | `0` |
| `cdid` | `1f1d14b3-8c25-4733-9f6b-2fdc01640081` |
| `tab_type` | `8` |
| `offset` | `0` |

### 2.2 首次请求全部应用层请求头

| 头 | 实际值 |
| --- | --- |
| `User-Agent` | `Python-urllib/3.14` |
| `Accept-Encoding` | `identity` |
| `Connection` | `close` |
| `Host` | `api5-normal-sinfonlinec.fqnovel.com` |

以上为应用层记录，不是 TLS 抓包。Python 将空格编码为 `%20`、星号编码为 `%2A`；Android 当前公共参数保留 `+` 和 `*`，而配方参数另行编码。解码后的参数值一致，未宣称请求字节或默认 UA 与 Android 相同。本轮为无签名请求，不能把该编码方式直接移植到需要按原始 query 签名的接口。

### 2.3 每次请求的文件约定

- `request.json`：时间、方法、完整 URL、全部 query、请求头、请求体和签名档位。
- `response.body`：未经修改的响应正文原始字节；`response.json`：状态、响应头、长度、SHA-256、完成时间和耗时。响应头若存在 `Set-Cookie` 则仅该头值脱敏。
- `analysis.json`：业务码、去重剧集 ID/数量、选中项、游标、session 与筛选器。发送失败时另记 `error.json`，不隐藏失败。
- 根目录 [index.json](api-evidence/home-20261106/index.json) 为全量请求索引，[filter-catalog.json](api-evidence/home-20261106/filter-catalog.json) 为首屏原始枚举，[verification.json](api-evidence/home-20261106/verification.json) 为逐份正文长度/哈希核验结果。
- [run.json](api-evidence/home-20261106/run.json) 记录运行环境、配方版本和哈希；[配方快照](api-evidence/home-20261106/recipe.json) 与 [取证脚本快照](api-evidence/home-20261106/probe-home-api.py) 保留本轮实际基线。原始记录包含本轮测试 session 与推荐上下文，均为临时测试身份返回。
- 正式脚本不自动重试、不跟随重定向；每个编号对应一次发送，68 次没有隐藏补发。

## 3. 当前客户端如何调用

入口：`LibraryFragment.homePageLoader` → `DirectClient.homeTab(8, offset, sessionId, selectedFilter, deviceId, installId)` → `RecipeHttp.readJson("home", ...)`。普通 HTTP 请求直接连接上游，不经自建服务。

| 参数/行为 | 当前实现与本次证据 |
| --- | --- |
| `tab_type` | 固定 `8`，短剧信息流；旧项目文档的 `0=推荐` 已纠正。首屏同时返回 14/5/2/9/11/12 的空 tab，不能据此宣称这些 tab 数据都已验证可用。 |
| `offset` | 首次、切筛选、刷新为 `0`；后续使用上次返回的 `next_offset`，不自行当条目数量相加。 |
| `session_id` | 首屏省略；分页透传对应首屏的 session；换筛选/刷新时清空。 |
| `selected_items` | 默认推荐省略；选分类传返回的 `selector_item_id`。当前 App 只持有一个选中 ID。 |
| `count`/`page_size` | 当前配方未发送；本轮未验证这类参数是否支持。 |
| 关键字 | 本接口未做关键字搜索验证；App 搜索走独立 `/reading/bookapi/search/fetch-debug/v/`，不能混同分类筛选。 |
| 版本与签名 | `version_code=65532`，`version_name=6.5.5.32`，`sign=none`；其它版本或签名模式未在本轮展开。 |

响应数据位置：`data.tab_item[tab_type=8]`，该 tab 的 `cell_data[0].cell_selector` 提供筛选元数据，剧集位于 `cell_data[].cell_data[].video_data[]`（客户端也兼容外层直接 `video_data[]`）。正文中还含 session、推荐上下文、封面及剧集元数据，完整字段保留在原文中，不以节选代替原始响应。

### 3.1 分页、组合与无效值

- #001：`offset=0` → 9 部、`next_offset=1`、`has_more=false`。
- #065：沿用 #001 的 session，`offset=1` → 9 部、`next_offset=2`、`has_more=true`，与首屏剧集 ID 无重叠。故当前 `next_offset > offset` 的分页方式有本次证据支持；首屏 `has_more=false` 不能直接视为结束。没有验证最终页和长期游标稳定性。
- #066 / #067：`cate_4,cate_19` 与反序均回显 `cate_4`、`cate_19`；只能证明服务端识别两个选择，无法确定 AND/OR、优先级和内容排序。因此当前建议继续单选。
- #068：虚构值 `cate_nonexistent_probe_20261106` 仍返回 200/code=0/9 部，但回显选中 ID 为空。回归必须同时检查回显和业务数据，不能只检查 HTTP 200。

## 4. 本次完整筛选枚举

当前只有一个 selector，`global_single=true`、`show_style=0`、`inner_title=筛选`。外层 `selection_type=1` / `multi_selection_bound=10`，四个内组 `selection_type=1`。这些字段按原值记录；不把 `multi_selection_bound=10` 单独解释为可以跨组叠加 10 个条件。68 个响应中 ID 集合未变化，不能据此保证未来不变。

### 4.1 顶部快捷项（10 项）

| 名称 | selected_items | 单项返回部数 | 单项实测证据 |
| --- | --- | --- | --- |
| 推荐榜 | `ranklist_hot` | 7 | [入参](api-evidence/home-20261106/002-ranklist_hot/request.json) · [出参](api-evidence/home-20261106/002-ranklist_hot/response.body) |
| 新剧 | `firstonlinetime_new` | 8 | [入参](api-evidence/home-20261106/003-firstonlinetime_new/request.json) · [出参](api-evidence/home-20261106/003-firstonlinetime_new/response.body) |
| 现代 | `cate_757` | 9 | [入参](api-evidence/home-20261106/004-cate_757/request.json) · [出参](api-evidence/home-20261106/004-cate_757/response.body) |
| 逆袭 | `cate_739` | 9 | [入参](api-evidence/home-20261106/005-cate_739/request.json) · [出参](api-evidence/home-20261106/005-cate_739/response.body) |
| 都市 | `cate_1` | 8 | [入参](api-evidence/home-20261106/006-cate_1/request.json) · [出参](api-evidence/home-20261106/006-cate_1/response.body) |
| 打脸虐渣 | `cate_1051` | 9 | [入参](api-evidence/home-20261106/007-cate_1051/request.json) · [出参](api-evidence/home-20261106/007-cate_1051/response.body) |
| 都市日常 | `cate_261` | 8 | [入参](api-evidence/home-20261106/008-cate_261/request.json) · [出参](api-evidence/home-20261106/008-cate_261/response.body) |
| 剧情 | `cate_316` | 9 | [入参](api-evidence/home-20261106/009-cate_316/request.json) · [出参](api-evidence/home-20261106/009-cate_316/response.body) |
| 总裁 | `cate_29` | 9 | [入参](api-evidence/home-20261106/010-cate_29/request.json) · [出参](api-evidence/home-20261106/010-cate_29/response.body) |
| 都市爱情 | `cate_1021` | 9 | [入参](api-evidence/home-20261106/011-cate_1021/request.json) · [出参](api-evidence/home-20261106/011-cate_1021/response.body) |

### 4.2 综合（2 项）

| 名称 | selected_items | 单项返回部数 | 单项实测证据 |
| --- | --- | --- | --- |
| 推荐榜 | `ranklist_hot` | 7 | [入参](api-evidence/home-20261106/002-ranklist_hot/request.json) · [出参](api-evidence/home-20261106/002-ranklist_hot/response.body) |
| 新剧 | `firstonlinetime_new` | 8 | [入参](api-evidence/home-20261106/003-firstonlinetime_new/request.json) · [出参](api-evidence/home-20261106/003-firstonlinetime_new/response.body) |

### 4.3 时代背景（6 项）

| 名称 | selected_items | 单项返回部数 | 单项实测证据 |
| --- | --- | --- | --- |
| 校园 | `cate_4` | 9 | [入参](api-evidence/home-20261106/012-cate_4/request.json) · [出参](api-evidence/home-20261106/012-cate_4/response.body) |
| 乡村 | `cate_11` | 9 | [入参](api-evidence/home-20261106/013-cate_11/request.json) · [出参](api-evidence/home-20261106/013-cate_11/response.body) |
| 职场 | `cate_127` | 9 | [入参](api-evidence/home-20261106/014-cate_127/request.json) · [出参](api-evidence/home-20261106/014-cate_127/response.body) |
| 历史古代 | `cate_273` | 9 | [入参](api-evidence/home-20261106/015-cate_273/request.json) · [出参](api-evidence/home-20261106/015-cate_273/response.body) |
| 古装 | `cate_308` | 9 | [入参](api-evidence/home-20261106/016-cate_308/request.json) · [出参](api-evidence/home-20261106/016-cate_308/response.body) |
| 民国 | `cate_390` | 9 | [入参](api-evidence/home-20261106/017-cate_390/request.json) · [出参](api-evidence/home-20261106/017-cate_390/response.body) |

### 4.4 主题情节（34 项）

| 名称 | selected_items | 单项返回部数 | 单项实测证据 |
| --- | --- | --- | --- |
| 现代言情 | `cate_3` | 9 | [入参](api-evidence/home-20261106/018-cate_3/request.json) · [出参](api-evidence/home-20261106/018-cate_3/response.body) |
| 系统 | `cate_19` | 8 | [入参](api-evidence/home-20261106/019-cate_19/request.json) · [出参](api-evidence/home-20261106/019-cate_19/response.body) |
| 重生 | `cate_36` | 8 | [入参](api-evidence/home-20261106/020-cate_36/request.json) · [出参](api-evidence/home-20261106/020-cate_36/response.body) |
| 穿越 | `cate_37` | 7 | [入参](api-evidence/home-20261106/021-cate_37/request.json) · [出参](api-evidence/home-20261106/021-cate_37/response.body) |
| 娱乐圈 | `cate_43` | 8 | [入参](api-evidence/home-20261106/022-cate_43/request.json) · [出参](api-evidence/home-20261106/022-cate_43/response.body) |
| 悬疑推理 | `cate_165` | 9 | [入参](api-evidence/home-20261106/023-cate_165/request.json) · [出参](api-evidence/home-20261106/023-cate_165/response.body) |
| 都市日常 | `cate_261` | 8 | [入参](api-evidence/home-20261106/008-cate_261/request.json) · [出参](api-evidence/home-20261106/008-cate_261/response.body) |
| 马甲 | `cate_266` | 8 | [入参](api-evidence/home-20261106/024-cate_266/request.json) · [出参](api-evidence/home-20261106/024-cate_266/response.body) |
| 喜剧 | `cate_303` | 9 | [入参](api-evidence/home-20261106/025-cate_303/request.json) · [出参](api-evidence/home-20261106/025-cate_303/response.body) |
| 剧情 | `cate_316` | 9 | [入参](api-evidence/home-20261106/009-cate_316/request.json) · [出参](api-evidence/home-20261106/009-cate_316/response.body) |
| 穿书 | `cate_382` | 9 | [入参](api-evidence/home-20261106/026-cate_382/request.json) · [出参](api-evidence/home-20261106/026-cate_382/response.body) |
| 现言甜宠 | `cate_395` | 8 | [入参](api-evidence/home-20261106/027-cate_395/request.json) · [出参](api-evidence/home-20261106/027-cate_395/response.body) |
| 古风爱情 | `cate_439` | 9 | [入参](api-evidence/home-20261106/028-cate_439/request.json) · [出参](api-evidence/home-20261106/028-cate_439/response.body) |
| 闪婚 | `cate_466` | 9 | [入参](api-evidence/home-20261106/029-cate_466/request.json) · [出参](api-evidence/home-20261106/029-cate_466/response.body) |
| 破镜重圆 | `cate_475` | 9 | [入参](api-evidence/home-20261106/030-cate_475/request.json) · [出参](api-evidence/home-20261106/030-cate_475/response.body) |
| 异能 | `cate_598` | 6 | [入参](api-evidence/home-20261106/031-cate_598/request.json) · [出参](api-evidence/home-20261106/031-cate_598/response.body) |
| 逆袭 | `cate_739` | 9 | [入参](api-evidence/home-20261106/005-cate_739/request.json) · [出参](api-evidence/home-20261106/005-cate_739/response.body) |
| 追妻 | `cate_842` | 9 | [入参](api-evidence/home-20261106/032-cate_842/request.json) · [出参](api-evidence/home-20261106/032-cate_842/response.body) |
| 传承觉醒 | `cate_1007` | 9 | [入参](api-evidence/home-20261106/033-cate_1007/request.json) · [出参](api-evidence/home-20261106/033-cate_1007/response.body) |
| 虐恋 | `cate_1008` | 9 | [入参](api-evidence/home-20261106/034-cate_1008/request.json) · [出参](api-evidence/home-20261106/034-cate_1008/response.body) |
| 玄幻仙侠 | `cate_1013` | 9 | [入参](api-evidence/home-20261106/035-cate_1013/request.json) · [出参](api-evidence/home-20261106/035-cate_1013/response.body) |
| 奇幻爱情 | `cate_1020` | 9 | [入参](api-evidence/home-20261106/036-cate_1020/request.json) · [出参](api-evidence/home-20261106/036-cate_1020/response.body) |
| 年代爱情 | `cate_1022` | 9 | [入参](api-evidence/home-20261106/037-cate_1022/request.json) · [出参](api-evidence/home-20261106/037-cate_1022/response.body) |
| 家庭伦理 | `cate_1024` | 9 | [入参](api-evidence/home-20261106/038-cate_1024/request.json) · [出参](api-evidence/home-20261106/038-cate_1024/response.body) |
| 战神归来 | `cate_1038` | 9 | [入参](api-evidence/home-20261106/039-cate_1038/request.json) · [出参](api-evidence/home-20261106/039-cate_1038/response.body) |
| 亲情 | `cate_1040` | 8 | [入参](api-evidence/home-20261106/040-cate_1040/request.json) · [出参](api-evidence/home-20261106/040-cate_1040/response.body) |
| 奇幻脑洞 | `cate_1041` | 9 | [入参](api-evidence/home-20261106/041-cate_1041/request.json) · [出参](api-evidence/home-20261106/041-cate_1041/response.body) |
| 无敌神医 | `cate_1043` | 9 | [入参](api-evidence/home-20261106/042-cate_1043/request.json) · [出参](api-evidence/home-20261106/042-cate_1043/response.body) |
| 赘婿逆袭 | `cate_1044` | 9 | [入参](api-evidence/home-20261106/043-cate_1044/request.json) · [出参](api-evidence/home-20261106/043-cate_1044/response.body) |
| 古风权谋 | `cate_1047` | 8 | [入参](api-evidence/home-20261106/044-cate_1047/request.json) · [出参](api-evidence/home-20261106/044-cate_1047/response.body) |
| 女性成长 | `cate_1048` | 9 | [入参](api-evidence/home-20261106/045-cate_1048/request.json) · [出参](api-evidence/home-20261106/045-cate_1048/response.body) |
| 暗恋成真 | `cate_1050` | 9 | [入参](api-evidence/home-20261106/046-cate_1050/request.json) · [出参](api-evidence/home-20261106/046-cate_1050/response.body) |
| 打脸虐渣 | `cate_1051` | 9 | [入参](api-evidence/home-20261106/007-cate_1051/request.json) · [出参](api-evidence/home-20261106/007-cate_1051/response.body) |
| 豪门恩怨 | `cate_1053` | 9 | [入参](api-evidence/home-20261106/047-cate_1053/request.json) · [出参](api-evidence/home-20261106/047-cate_1053/response.body) |

### 4.5 角色设定（17 项）

| 名称 | selected_items | 单项返回部数 | 单项实测证据 |
| --- | --- | --- | --- |
| 神豪 | `cate_20` | 9 | [入参](api-evidence/home-20261106/048-cate_20/request.json) · [出参](api-evidence/home-20261106/048-cate_20/response.body) |
| 萌宝 | `cate_28` | 8 | [入参](api-evidence/home-20261106/049-cate_28/request.json) · [出参](api-evidence/home-20261106/049-cate_28/response.body) |
| 皇后 | `cate_84` | 9 | [入参](api-evidence/home-20261106/050-cate_84/request.json) · [出参](api-evidence/home-20261106/050-cate_84/response.body) |
| 王妃 | `cate_85` | 9 | [入参](api-evidence/home-20261106/051-cate_85/request.json) · [出参](api-evidence/home-20261106/051-cate_85/response.body) |
| 团宠 | `cate_94` | 9 | [入参](api-evidence/home-20261106/052-cate_94/request.json) · [出参](api-evidence/home-20261106/052-cate_94/response.body) |
| 女帝 | `cate_378` | 9 | [入参](api-evidence/home-20261106/053-cate_378/request.json) · [出参](api-evidence/home-20261106/053-cate_378/response.body) |
| 青梅竹马 | `cate_387` | 8 | [入参](api-evidence/home-20261106/054-cate_387/request.json) · [出参](api-evidence/home-20261106/054-cate_387/response.body) |
| 替身 | `cate_470` | 9 | [入参](api-evidence/home-20261106/055-cate_470/request.json) · [出参](api-evidence/home-20261106/055-cate_470/response.body) |
| 大叔 | `cate_737` | 9 | [入参](api-evidence/home-20261106/056-cate_737/request.json) · [出参](api-evidence/home-20261106/056-cate_737/response.body) |
| 大女主 | `cate_760` | 8 | [入参](api-evidence/home-20261106/057-cate_760/request.json) · [出参](api-evidence/home-20261106/057-cate_760/response.body) |
| 真假千金 | `cate_844` | 7 | [入参](api-evidence/home-20261106/058-cate_844/request.json) · [出参](api-evidence/home-20261106/058-cate_844/response.body) |
| 龙王 | `cate_901` | 8 | [入参](api-evidence/home-20261106/059-cate_901/request.json) · [出参](api-evidence/home-20261106/059-cate_901/response.body) |
| 小人物 | `cate_1010` | 9 | [入参](api-evidence/home-20261106/060-cate_1010/request.json) · [出参](api-evidence/home-20261106/060-cate_1010/response.body) |
| 强者回归 | `cate_1045` | 8 | [入参](api-evidence/home-20261106/061-cate_1045/request.json) · [出参](api-evidence/home-20261106/061-cate_1045/response.body) |
| 天下无敌 | `cate_1046` | 8 | [入参](api-evidence/home-20261106/062-cate_1046/request.json) · [出参](api-evidence/home-20261106/062-cate_1046/response.body) |
| 强强联合 | `cate_1049` | 9 | [入参](api-evidence/home-20261106/063-cate_1049/request.json) · [出参](api-evidence/home-20261106/063-cate_1049/response.body) |
| 欢喜冤家 | `cate_1052` | 9 | [入参](api-evidence/home-20261106/064-cate_1052/request.json) · [出参](api-evidence/home-20261106/064-cate_1052/response.body) |

## 5. 当前首页覆盖与交互建议

### 5.1 覆盖核对（交互已于 2026-11-07 更新）

| 位置 | 接口项数 | 当前客户端 |
| --- | --- | --- |
| 顶部快捷项 | 10 | 全部显示，横向滚动；右侧“筛选”固定，另有本地“推荐”。 |
| 综合 | 2 | 面板完整显示；顶部重复提供快捷入口。 |
| 时代背景 | 6 | 面板完整解析/展示。 |
| 主题情节 | 34 | 面板完整解析/展示，右侧可滚动。 |
| 角色设定 | 17 | 面板完整解析/展示。 |

顶部与内组有 6 个重复 ID；顶部独有 4 个为“现代 `cate_757`、都市 `cate_1`、总裁 `cate_29`、都市爱情 `cate_1021`”。所以只保留四个内组、删除顶部快捷项会漏掉这 4 个。App 的“推荐”是清空筛选，不等同于上游“推荐榜”。

### 5.2 取证时的适配隐患与修复状态

- 已修复：原 `LibraryFragment.renderFilters` 在 outer filters 为空时隐藏入口；现在仅内组也可打开面板，已选项暂时消失时仍可清除。
- 已修复：面板不再按组名过滤“综合”，新增项可正常展示。
- 已修复：`DirectClient.homeTab` 合并多份 selector 的快捷项与分组，遍历嵌套 cell，同组按 ID 去重；离线回归覆盖多个 selector 及新增综合项。
- `HomeFilterGroup` 没有承载 `global_single` / `selection_type` 等元数据。当前固定单选符合本次响应；未来上游策略改变时不能自动适配。
- 页面在切筛选后可能刷新分组列表；本轮接口所有响应的 ID 集合稳定，没观察到因此缩小分类的现象。

### 5.3 已采用的交互（2026-11-07 落地）

1. 首页继续显示“推荐 + 上游快捷标签”，将“筛选”固定在行末；用户横向浏览快捷标签时仍能找到完整面板入口。
2. 点击后打开底部面板，左侧提供“快捷分类 + 综合 + 时代背景 + 主题情节 + 角色设定”；右侧展示对应选项。组名和选项来自接口，快捷分类包含 outer_row，保证顶部独有的 4 项在面板中也能找到。
3. 面板顶部显示“当前：校园”与“清除筛选”；关闭后首页显示“筛选 · 校园”，重新打开定位到已选项所属组。现有客户端已有一部分已选展示和分组定位，可在此基础上调整。
4. 继续全局单选，点选即生效并关闭；选另一个组会替换已有条件。避免把分组界面表达成可组合条件，等验证 AND/OR 和内容一致性后再评估多选。
5. 所有可点击筛选项至少提供 48dp 触摸区域；顶部背景约 32dp、面板背景约 36dp；长名称允许完整阅读；即使快捷项为空，只要内组有内容，固定入口仍显示。

这比把 63 项全部平铺到首页更适合手机：常用项一步触达，低频项在完整面板中可发现，并保留上游动态增减分类的能力。该方案已实现，具体真机与回归证据见第 9 节。

## 6. 全部 68 次请求索引

所有请求均使用第 2 节相同的公共参数和测试身份；每行入参文件仍完整保存，不能只用本表重建请求。结果数量按 `series_id`（缺失时 vid）去重。

| # | case / selected_items | offset | HTTP / code | 部数 | 选中回显 | 耗时 ms | 完整入参 | 原始出参 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 001 | `default` | 0 | 200 / 0 | 9 | `无` | 1722 | [request](api-evidence/home-20261106/001-default/request.json) | [body](api-evidence/home-20261106/001-default/response.body) · [meta](api-evidence/home-20261106/001-default/response.json) |
| 002 | `ranklist_hot` | 0 | 200 / 0 | 7 | `ranklist_hot` | 1327 | [request](api-evidence/home-20261106/002-ranklist_hot/request.json) | [body](api-evidence/home-20261106/002-ranklist_hot/response.body) · [meta](api-evidence/home-20261106/002-ranklist_hot/response.json) |
| 003 | `firstonlinetime_new` | 0 | 200 / 0 | 8 | `firstonlinetime_new` | 1525 | [request](api-evidence/home-20261106/003-firstonlinetime_new/request.json) | [body](api-evidence/home-20261106/003-firstonlinetime_new/response.body) · [meta](api-evidence/home-20261106/003-firstonlinetime_new/response.json) |
| 004 | `cate_757` | 0 | 200 / 0 | 9 | `cate_757` | 1517 | [request](api-evidence/home-20261106/004-cate_757/request.json) | [body](api-evidence/home-20261106/004-cate_757/response.body) · [meta](api-evidence/home-20261106/004-cate_757/response.json) |
| 005 | `cate_739` | 0 | 200 / 0 | 9 | `cate_739` | 1544 | [request](api-evidence/home-20261106/005-cate_739/request.json) | [body](api-evidence/home-20261106/005-cate_739/response.body) · [meta](api-evidence/home-20261106/005-cate_739/response.json) |
| 006 | `cate_1` | 0 | 200 / 0 | 8 | `cate_1` | 1604 | [request](api-evidence/home-20261106/006-cate_1/request.json) | [body](api-evidence/home-20261106/006-cate_1/response.body) · [meta](api-evidence/home-20261106/006-cate_1/response.json) |
| 007 | `cate_1051` | 0 | 200 / 0 | 9 | `cate_1051` | 1375 | [request](api-evidence/home-20261106/007-cate_1051/request.json) | [body](api-evidence/home-20261106/007-cate_1051/response.body) · [meta](api-evidence/home-20261106/007-cate_1051/response.json) |
| 008 | `cate_261` | 0 | 200 / 0 | 8 | `cate_261` | 1721 | [request](api-evidence/home-20261106/008-cate_261/request.json) | [body](api-evidence/home-20261106/008-cate_261/response.body) · [meta](api-evidence/home-20261106/008-cate_261/response.json) |
| 009 | `cate_316` | 0 | 200 / 0 | 9 | `cate_316` | 1642 | [request](api-evidence/home-20261106/009-cate_316/request.json) | [body](api-evidence/home-20261106/009-cate_316/response.body) · [meta](api-evidence/home-20261106/009-cate_316/response.json) |
| 010 | `cate_29` | 0 | 200 / 0 | 9 | `cate_29` | 1555 | [request](api-evidence/home-20261106/010-cate_29/request.json) | [body](api-evidence/home-20261106/010-cate_29/response.body) · [meta](api-evidence/home-20261106/010-cate_29/response.json) |
| 011 | `cate_1021` | 0 | 200 / 0 | 9 | `cate_1021` | 1554 | [request](api-evidence/home-20261106/011-cate_1021/request.json) | [body](api-evidence/home-20261106/011-cate_1021/response.body) · [meta](api-evidence/home-20261106/011-cate_1021/response.json) |
| 012 | `cate_4` | 0 | 200 / 0 | 9 | `cate_4` | 1714 | [request](api-evidence/home-20261106/012-cate_4/request.json) | [body](api-evidence/home-20261106/012-cate_4/response.body) · [meta](api-evidence/home-20261106/012-cate_4/response.json) |
| 013 | `cate_11` | 0 | 200 / 0 | 9 | `cate_11` | 2125 | [request](api-evidence/home-20261106/013-cate_11/request.json) | [body](api-evidence/home-20261106/013-cate_11/response.body) · [meta](api-evidence/home-20261106/013-cate_11/response.json) |
| 014 | `cate_127` | 0 | 200 / 0 | 9 | `cate_127` | 1519 | [request](api-evidence/home-20261106/014-cate_127/request.json) | [body](api-evidence/home-20261106/014-cate_127/response.body) · [meta](api-evidence/home-20261106/014-cate_127/response.json) |
| 015 | `cate_273` | 0 | 200 / 0 | 9 | `cate_273` | 1582 | [request](api-evidence/home-20261106/015-cate_273/request.json) | [body](api-evidence/home-20261106/015-cate_273/response.body) · [meta](api-evidence/home-20261106/015-cate_273/response.json) |
| 016 | `cate_308` | 0 | 200 / 0 | 9 | `cate_308` | 1498 | [request](api-evidence/home-20261106/016-cate_308/request.json) | [body](api-evidence/home-20261106/016-cate_308/response.body) · [meta](api-evidence/home-20261106/016-cate_308/response.json) |
| 017 | `cate_390` | 0 | 200 / 0 | 9 | `cate_390` | 1805 | [request](api-evidence/home-20261106/017-cate_390/request.json) | [body](api-evidence/home-20261106/017-cate_390/response.body) · [meta](api-evidence/home-20261106/017-cate_390/response.json) |
| 018 | `cate_3` | 0 | 200 / 0 | 9 | `cate_3` | 1490 | [request](api-evidence/home-20261106/018-cate_3/request.json) | [body](api-evidence/home-20261106/018-cate_3/response.body) · [meta](api-evidence/home-20261106/018-cate_3/response.json) |
| 019 | `cate_19` | 0 | 200 / 0 | 8 | `cate_19` | 1831 | [request](api-evidence/home-20261106/019-cate_19/request.json) | [body](api-evidence/home-20261106/019-cate_19/response.body) · [meta](api-evidence/home-20261106/019-cate_19/response.json) |
| 020 | `cate_36` | 0 | 200 / 0 | 8 | `cate_36` | 1700 | [request](api-evidence/home-20261106/020-cate_36/request.json) | [body](api-evidence/home-20261106/020-cate_36/response.body) · [meta](api-evidence/home-20261106/020-cate_36/response.json) |
| 021 | `cate_37` | 0 | 200 / 0 | 7 | `cate_37` | 1616 | [request](api-evidence/home-20261106/021-cate_37/request.json) | [body](api-evidence/home-20261106/021-cate_37/response.body) · [meta](api-evidence/home-20261106/021-cate_37/response.json) |
| 022 | `cate_43` | 0 | 200 / 0 | 8 | `cate_43` | 1655 | [request](api-evidence/home-20261106/022-cate_43/request.json) | [body](api-evidence/home-20261106/022-cate_43/response.body) · [meta](api-evidence/home-20261106/022-cate_43/response.json) |
| 023 | `cate_165` | 0 | 200 / 0 | 9 | `cate_165` | 1534 | [request](api-evidence/home-20261106/023-cate_165/request.json) | [body](api-evidence/home-20261106/023-cate_165/response.body) · [meta](api-evidence/home-20261106/023-cate_165/response.json) |
| 024 | `cate_266` | 0 | 200 / 0 | 8 | `cate_266` | 1507 | [request](api-evidence/home-20261106/024-cate_266/request.json) | [body](api-evidence/home-20261106/024-cate_266/response.body) · [meta](api-evidence/home-20261106/024-cate_266/response.json) |
| 025 | `cate_303` | 0 | 200 / 0 | 9 | `cate_303` | 1694 | [request](api-evidence/home-20261106/025-cate_303/request.json) | [body](api-evidence/home-20261106/025-cate_303/response.body) · [meta](api-evidence/home-20261106/025-cate_303/response.json) |
| 026 | `cate_382` | 0 | 200 / 0 | 9 | `cate_382` | 1606 | [request](api-evidence/home-20261106/026-cate_382/request.json) | [body](api-evidence/home-20261106/026-cate_382/response.body) · [meta](api-evidence/home-20261106/026-cate_382/response.json) |
| 027 | `cate_395` | 0 | 200 / 0 | 8 | `cate_395` | 1441 | [request](api-evidence/home-20261106/027-cate_395/request.json) | [body](api-evidence/home-20261106/027-cate_395/response.body) · [meta](api-evidence/home-20261106/027-cate_395/response.json) |
| 028 | `cate_439` | 0 | 200 / 0 | 9 | `cate_439` | 1609 | [request](api-evidence/home-20261106/028-cate_439/request.json) | [body](api-evidence/home-20261106/028-cate_439/response.body) · [meta](api-evidence/home-20261106/028-cate_439/response.json) |
| 029 | `cate_466` | 0 | 200 / 0 | 9 | `cate_466` | 1889 | [request](api-evidence/home-20261106/029-cate_466/request.json) | [body](api-evidence/home-20261106/029-cate_466/response.body) · [meta](api-evidence/home-20261106/029-cate_466/response.json) |
| 030 | `cate_475` | 0 | 200 / 0 | 9 | `cate_475` | 1538 | [request](api-evidence/home-20261106/030-cate_475/request.json) | [body](api-evidence/home-20261106/030-cate_475/response.body) · [meta](api-evidence/home-20261106/030-cate_475/response.json) |
| 031 | `cate_598` | 0 | 200 / 0 | 6 | `cate_598` | 1781 | [request](api-evidence/home-20261106/031-cate_598/request.json) | [body](api-evidence/home-20261106/031-cate_598/response.body) · [meta](api-evidence/home-20261106/031-cate_598/response.json) |
| 032 | `cate_842` | 0 | 200 / 0 | 9 | `cate_842` | 1804 | [request](api-evidence/home-20261106/032-cate_842/request.json) | [body](api-evidence/home-20261106/032-cate_842/response.body) · [meta](api-evidence/home-20261106/032-cate_842/response.json) |
| 033 | `cate_1007` | 0 | 200 / 0 | 9 | `cate_1007` | 1592 | [request](api-evidence/home-20261106/033-cate_1007/request.json) | [body](api-evidence/home-20261106/033-cate_1007/response.body) · [meta](api-evidence/home-20261106/033-cate_1007/response.json) |
| 034 | `cate_1008` | 0 | 200 / 0 | 9 | `cate_1008` | 1455 | [request](api-evidence/home-20261106/034-cate_1008/request.json) | [body](api-evidence/home-20261106/034-cate_1008/response.body) · [meta](api-evidence/home-20261106/034-cate_1008/response.json) |
| 035 | `cate_1013` | 0 | 200 / 0 | 9 | `cate_1013` | 1778 | [request](api-evidence/home-20261106/035-cate_1013/request.json) | [body](api-evidence/home-20261106/035-cate_1013/response.body) · [meta](api-evidence/home-20261106/035-cate_1013/response.json) |
| 036 | `cate_1020` | 0 | 200 / 0 | 9 | `cate_1020` | 1510 | [request](api-evidence/home-20261106/036-cate_1020/request.json) | [body](api-evidence/home-20261106/036-cate_1020/response.body) · [meta](api-evidence/home-20261106/036-cate_1020/response.json) |
| 037 | `cate_1022` | 0 | 200 / 0 | 9 | `cate_1022` | 1501 | [request](api-evidence/home-20261106/037-cate_1022/request.json) | [body](api-evidence/home-20261106/037-cate_1022/response.body) · [meta](api-evidence/home-20261106/037-cate_1022/response.json) |
| 038 | `cate_1024` | 0 | 200 / 0 | 9 | `cate_1024` | 1709 | [request](api-evidence/home-20261106/038-cate_1024/request.json) | [body](api-evidence/home-20261106/038-cate_1024/response.body) · [meta](api-evidence/home-20261106/038-cate_1024/response.json) |
| 039 | `cate_1038` | 0 | 200 / 0 | 9 | `cate_1038` | 1433 | [request](api-evidence/home-20261106/039-cate_1038/request.json) | [body](api-evidence/home-20261106/039-cate_1038/response.body) · [meta](api-evidence/home-20261106/039-cate_1038/response.json) |
| 040 | `cate_1040` | 0 | 200 / 0 | 8 | `cate_1040` | 1574 | [request](api-evidence/home-20261106/040-cate_1040/request.json) | [body](api-evidence/home-20261106/040-cate_1040/response.body) · [meta](api-evidence/home-20261106/040-cate_1040/response.json) |
| 041 | `cate_1041` | 0 | 200 / 0 | 9 | `cate_1041` | 1815 | [request](api-evidence/home-20261106/041-cate_1041/request.json) | [body](api-evidence/home-20261106/041-cate_1041/response.body) · [meta](api-evidence/home-20261106/041-cate_1041/response.json) |
| 042 | `cate_1043` | 0 | 200 / 0 | 9 | `cate_1043` | 1514 | [request](api-evidence/home-20261106/042-cate_1043/request.json) | [body](api-evidence/home-20261106/042-cate_1043/response.body) · [meta](api-evidence/home-20261106/042-cate_1043/response.json) |
| 043 | `cate_1044` | 0 | 200 / 0 | 9 | `cate_1044` | 1871 | [request](api-evidence/home-20261106/043-cate_1044/request.json) | [body](api-evidence/home-20261106/043-cate_1044/response.body) · [meta](api-evidence/home-20261106/043-cate_1044/response.json) |
| 044 | `cate_1047` | 0 | 200 / 0 | 8 | `cate_1047` | 1550 | [request](api-evidence/home-20261106/044-cate_1047/request.json) | [body](api-evidence/home-20261106/044-cate_1047/response.body) · [meta](api-evidence/home-20261106/044-cate_1047/response.json) |
| 045 | `cate_1048` | 0 | 200 / 0 | 9 | `cate_1048` | 1642 | [request](api-evidence/home-20261106/045-cate_1048/request.json) | [body](api-evidence/home-20261106/045-cate_1048/response.body) · [meta](api-evidence/home-20261106/045-cate_1048/response.json) |
| 046 | `cate_1050` | 0 | 200 / 0 | 9 | `cate_1050` | 1563 | [request](api-evidence/home-20261106/046-cate_1050/request.json) | [body](api-evidence/home-20261106/046-cate_1050/response.body) · [meta](api-evidence/home-20261106/046-cate_1050/response.json) |
| 047 | `cate_1053` | 0 | 200 / 0 | 9 | `cate_1053` | 1742 | [request](api-evidence/home-20261106/047-cate_1053/request.json) | [body](api-evidence/home-20261106/047-cate_1053/response.body) · [meta](api-evidence/home-20261106/047-cate_1053/response.json) |
| 048 | `cate_20` | 0 | 200 / 0 | 9 | `cate_20` | 1544 | [request](api-evidence/home-20261106/048-cate_20/request.json) | [body](api-evidence/home-20261106/048-cate_20/response.body) · [meta](api-evidence/home-20261106/048-cate_20/response.json) |
| 049 | `cate_28` | 0 | 200 / 0 | 8 | `cate_28` | 1418 | [request](api-evidence/home-20261106/049-cate_28/request.json) | [body](api-evidence/home-20261106/049-cate_28/response.body) · [meta](api-evidence/home-20261106/049-cate_28/response.json) |
| 050 | `cate_84` | 0 | 200 / 0 | 9 | `cate_84` | 1537 | [request](api-evidence/home-20261106/050-cate_84/request.json) | [body](api-evidence/home-20261106/050-cate_84/response.body) · [meta](api-evidence/home-20261106/050-cate_84/response.json) |
| 051 | `cate_85` | 0 | 200 / 0 | 9 | `cate_85` | 1537 | [request](api-evidence/home-20261106/051-cate_85/request.json) | [body](api-evidence/home-20261106/051-cate_85/response.body) · [meta](api-evidence/home-20261106/051-cate_85/response.json) |
| 052 | `cate_94` | 0 | 200 / 0 | 9 | `cate_94` | 1391 | [request](api-evidence/home-20261106/052-cate_94/request.json) | [body](api-evidence/home-20261106/052-cate_94/response.body) · [meta](api-evidence/home-20261106/052-cate_94/response.json) |
| 053 | `cate_378` | 0 | 200 / 0 | 9 | `cate_378` | 1622 | [request](api-evidence/home-20261106/053-cate_378/request.json) | [body](api-evidence/home-20261106/053-cate_378/response.body) · [meta](api-evidence/home-20261106/053-cate_378/response.json) |
| 054 | `cate_387` | 0 | 200 / 0 | 8 | `cate_387` | 1477 | [request](api-evidence/home-20261106/054-cate_387/request.json) | [body](api-evidence/home-20261106/054-cate_387/response.body) · [meta](api-evidence/home-20261106/054-cate_387/response.json) |
| 055 | `cate_470` | 0 | 200 / 0 | 9 | `cate_470` | 1513 | [request](api-evidence/home-20261106/055-cate_470/request.json) | [body](api-evidence/home-20261106/055-cate_470/response.body) · [meta](api-evidence/home-20261106/055-cate_470/response.json) |
| 056 | `cate_737` | 0 | 200 / 0 | 9 | `cate_737` | 1500 | [request](api-evidence/home-20261106/056-cate_737/request.json) | [body](api-evidence/home-20261106/056-cate_737/response.body) · [meta](api-evidence/home-20261106/056-cate_737/response.json) |
| 057 | `cate_760` | 0 | 200 / 0 | 8 | `cate_760` | 1488 | [request](api-evidence/home-20261106/057-cate_760/request.json) | [body](api-evidence/home-20261106/057-cate_760/response.body) · [meta](api-evidence/home-20261106/057-cate_760/response.json) |
| 058 | `cate_844` | 0 | 200 / 0 | 7 | `cate_844` | 1730 | [request](api-evidence/home-20261106/058-cate_844/request.json) | [body](api-evidence/home-20261106/058-cate_844/response.body) · [meta](api-evidence/home-20261106/058-cate_844/response.json) |
| 059 | `cate_901` | 0 | 200 / 0 | 8 | `cate_901` | 1779 | [request](api-evidence/home-20261106/059-cate_901/request.json) | [body](api-evidence/home-20261106/059-cate_901/response.body) · [meta](api-evidence/home-20261106/059-cate_901/response.json) |
| 060 | `cate_1010` | 0 | 200 / 0 | 9 | `cate_1010` | 1471 | [request](api-evidence/home-20261106/060-cate_1010/request.json) | [body](api-evidence/home-20261106/060-cate_1010/response.body) · [meta](api-evidence/home-20261106/060-cate_1010/response.json) |
| 061 | `cate_1045` | 0 | 200 / 0 | 8 | `cate_1045` | 1752 | [request](api-evidence/home-20261106/061-cate_1045/request.json) | [body](api-evidence/home-20261106/061-cate_1045/response.body) · [meta](api-evidence/home-20261106/061-cate_1045/response.json) |
| 062 | `cate_1046` | 0 | 200 / 0 | 8 | `cate_1046` | 1512 | [request](api-evidence/home-20261106/062-cate_1046/request.json) | [body](api-evidence/home-20261106/062-cate_1046/response.body) · [meta](api-evidence/home-20261106/062-cate_1046/response.json) |
| 063 | `cate_1049` | 0 | 200 / 0 | 9 | `cate_1049` | 1808 | [request](api-evidence/home-20261106/063-cate_1049/request.json) | [body](api-evidence/home-20261106/063-cate_1049/response.body) · [meta](api-evidence/home-20261106/063-cate_1049/response.json) |
| 064 | `cate_1052` | 0 | 200 / 0 | 9 | `cate_1052` | 1485 | [request](api-evidence/home-20261106/064-cate_1052/request.json) | [body](api-evidence/home-20261106/064-cate_1052/response.body) · [meta](api-evidence/home-20261106/064-cate_1052/response.json) |
| 065 | `page1` | 1 | 200 / 0 | 9 | `无` | 1598 | [request](api-evidence/home-20261106/065-page1/request.json) | [body](api-evidence/home-20261106/065-page1/response.body) · [meta](api-evidence/home-20261106/065-page1/response.json) |
| 066 | `cate_4,cate_19` | 0 | 200 / 0 | 9 | `cate_4,cate_19` | 1364 | [request](api-evidence/home-20261106/066-combo-school-system/request.json) | [body](api-evidence/home-20261106/066-combo-school-system/response.body) · [meta](api-evidence/home-20261106/066-combo-school-system/response.json) |
| 067 | `cate_19,cate_4` | 0 | 200 / 0 | 9 | `cate_4,cate_19` | 1469 | [request](api-evidence/home-20261106/067-combo-system-school/request.json) | [body](api-evidence/home-20261106/067-combo-system-school/response.body) · [meta](api-evidence/home-20261106/067-combo-system-school/response.json) |
| 068 | `cate_nonexistent_probe_20261106` | 0 | 200 / 0 | 9 | `无` | 1681 | [request](api-evidence/home-20261106/068-invalid-selector/request.json) | [body](api-evidence/home-20261106/068-invalid-selector/response.body) · [meta](api-evidence/home-20261106/068-invalid-selector/response.json) |

## 7. 回归复用

当前只依赖 Python 标准库。[维护中的取证脚本](../tools/probe-home-api.py) 从当前配方读取首页 host/path/params；遇到非 GET、签名改变、版本改变时主动停止，避免无声套用旧协议。

```zsh
cd /Users/long/Documents/OpenCodeProjects/duanju
python3 tools/probe-home-api.py --all-filters \
  --output docs/api-evidence/home-YYYYMMDD-HHMMSS
```

输出必须使用新目录；脚本拒绝覆盖既有取证。`--all-filters` 表示遍历本次首屏返回的全部唯一值，未来项数变化时请求总量也会变化。省略该参数只做首屏、分页、两组组合和无效值探测。本次不存在失败，无须补发；后续失败应保留原记录，另开新目录复测，不能覆盖成成功结果。

回归至少比较：HTTP/业务码、非空正文、目标 tab、当前返回枚举、提交值的选中回显、非空剧集、分页 session/游标、正文哈希完整性。推荐流内容本身会变化，不应以剧名或剧集 ID 与本次完全一致作为通过条件。

## 8. 旧文档修订范围

- `docs/android-app.md`：纠正首页 `tab_type=0`，链接本报告，将固定“每页 9 部”改为本次 6–9 部的观察。
- `hongguo-docs/API.md` 首页章节：标记 2026-11-06 实测覆盖；修正“多选只生效第一个”为组合回显的有限证据；保留旧枚举为历史背景。
- 其它接口旧结论没有因本轮首页探测而重新获证，不能把首页免签成功推广到详情或取流。

## 9. 筛选交互与首页请求头落地（2026-11-07）

用户确认采用第 5.3 节方案后，已修改首页布局、面板和解析：固定筛选入口、完整动态分组、顶部独有项纳入面板、单选即时生效、已选摘要/重开定位/清除、仅内组与多份 selector 兼容。按钮视觉与触摸区域分开：顶部约 32dp、面板约 36dp，点击区域仍为 48dp；筛选栏上下留白和面板项间距已收紧。

浅色/深色使用首页筛选专属前景和背景配对；浅色选中项为浅红底深红字，避免白字落在浅灰底上。弹层只由容器绘制圆角并裁切内容，去掉额外方形阴影与叠加背景。

首页顶部胶囊增加独立底色和 1dp 描边，避免浅色模式下与页面融合。剧集网格改为完整内容卡：封面与标题/评分/播放量共享 12dp 圆角外轮廓，信息区内边距 8dp、细描边、零阴影，每张卡片填满自己的网格列。

选择控件统一使用 `choice_*` 颜色：首页胶囊、详情/播放页集数分组及播放页当前集共享选中底色、文字色和描边；相关季的“当前”标记同步配对。详情播放按钮显式指定 Material 底色和描边，浅色下为浅粉底深红字，深色下为暗红底浅粉字。详情/普通播放页返回栏使用 `app_bg`，与主题的系统状态栏底色一致。

### 9.1 仅首页模拟客户端请求头

配方版本 6，只增加 `endpoints.home.headers`：

```json
{
  "User-Agent": "com.phoenix.read/65532 (Linux; U; Android 16; zh_CN; Redmi Note 8 Pro; Build/BP4A.251205.006)",
  "Accept": "application/json",
  "Accept-Language": "zh-CN,zh;q=0.9"
}
```

该 UA 采用 App 包名/版本与当前首页参数中的设备字段，不冒称已抓取到官方完全相同的原始头，也没有伪造 Cronet/TTNet 的网络栈信息。历史文档“首页无需特殊头”仅说明旧样本可用，不能证明新头能永久规避上游风控。

本次未改公共 `RecipeHttp` 请求头逻辑；逐项比较归档 v5 与 v6，search/detail/media 的全部端点配置及原 model 签名 UA 均相同。离线测试和联网原始记录同时确认首页新增头没有串到其它接口。

### 9.2 新首页头的全部真实请求

2026-11-06 23:56:06–23:58:10（北京时间），配方 v6、独立随机测试身份，重新发出 68 次请求。全部 HTTP 200/code=0；63 个唯一筛选值正确回显，单项 6–9 部；第 2 页 9 部，与首屏无重复。组合仍只证明两个选择均被回显，无效值仍不回显选中项。

完整 [68 次入参/出参索引](api-evidence/home-app-headers-20261106/README.md)、[首个请求](api-evidence/home-app-headers-20261106/001-default/request.json)、[原始首个响应](api-evidence/home-app-headers-20261106/001-default/response.body)、[校验结果](api-evidence/home-app-headers-20261106/verification.json) 均已归档。此轮依旧由 Python urllib 发送，不视为 Android 抓包。

### 9.3 其它接口与播放回归

2026-11-07 00:01，显式运行生产 Kotlin `RecipeLiveTest`，配方 v6、临时随机设备身份：

| 项目 | 实际结果 |
| --- | --- |
| 首页 | 9 部，使用新首页头 |
| 搜索“修仙” | 18 条，保持原搜索头 |
| 详情 | 1000 条分集、封面可读，保持原签名头 |
| 首集取流 | 第 1 次空正文，第 2 次成功；1080p、5 档 |
| CDN 与真实媒体 | HTTP 206，读 65536 字节；FFmpeg 解密后实际解码一帧成功 |
| 第 4/40/78 集 | 均取流成功；第 40 集在同档第 4 次成功 |
| 无签名详情/取流对照 | HTTP 200、空正文，不能恢复为全局免签假设 |
| 历史 plan 对照 | HTTP 200、code=101116，本轮仍不可用 |

15 个 HttpURLConnection 发送全部记录，正文长度/哈希已逐份校验；媒体原始记录包含本轮临时签名和密钥信封，仅存于本机 [完整联网回归归档](/Users/long/Documents/OpenCodeProjects/0922/hongguo-docs/api-evidence/core-regression-20261107/README.md)，未纳入 duanju Git 工作区。记录的是显式应用层请求头，HTTP 栈自动添加的头未抓包；FFmpeg 自身的 CDN 请求不经过该记录器。为保留完整出参，测试取证读取整份搜索正文，生产流式搜索实现没有改动。

### 9.4 自动化与真机界面验收

- `HomeFilterRegressionTest`：横向滚动时固定入口完整可见、32dp 可见背景与 48dp 触摸区域分离；快捷独有项和综合新增项可达；跨组替换、重新打开定位、重复点击不重发、清除归零；仅内组；320dp 小屏/1.3 倍字体/夜间下长选项完整且可点击。
- `RecipeRuntimeTest`：多份 selector/嵌套分组/按 ID 去重；首页头不会改变搜索或详情/取流签名 UA。
- 完整单测 89 个：88 通过、1 个联网默认跳过、0 失败/错误；联网测试另行显式通过。最终 Lint 0 错误/188 警告，Debug APK 构建成功；新增警告为旧播放背景资源不再使用。
- Redmi Note 8 Pro / Android 16 真机覆盖安装并保留数据：横向滚动后入口可见；完整五组；选校园后显示“筛选 · 校园”；重开定位时代背景并高亮校园；清除回到推荐。最终按钮缩小版已再次覆盖安装。

界面截图/XML 位于本机 `android/app/build/reports/home-filter-ui/`，包括 `scrolled.xml`、`campus-home.xml`、`campus-reopened.png`、`cleared.xml` 与最终紧凑按钮截图。测试没有清应用数据，也没有发布或推送。

### 9.5 随后真机空响应仍有复现

00:49 首集取流在 legacy 4 次和 none 4 次后全部失败；00:58 再次点击同集后，legacy 第 4 次成功，随后第 2 集预取也在 legacy 第 4 次成功。电脑同一个 videoId 首次成功。这一失败不能被 00:01 的另一部剧验收覆盖，也不能据恢复宣告根治。详见 [空响应复现与同分集对照](2026-11-07-empty-response-observation.md)。

## 10. 后续全页面验收

2026-11-07 随后完成八个页面的浅深色与交互优化，包含首页刷新游标、搜索/相关重试、实时选集高亮、全屏返回、缓存输入校验与大字体适配。最终完整测试为 99 项（98 通过、1 联网默认跳过），Lint 0 错误/188 警告。这里第 9.4 节保留首页专项当时的测试数量，后续范围及真机证据以 [全页面验收记录](2026-11-07-android-ui-polish.md) 为准；未据此重新确认上游接口长期稳定性。
