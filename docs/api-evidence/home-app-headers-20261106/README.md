# 首页 App 格式请求头取证（配方 v6）

2026-11-06 23:56–23:58，68 次真实请求，63 个唯一筛选值全部正确回显。完整接口基线见 [报告](../../2026-11-06-home-api-verification.md#9-筛选交互与首页请求头落地2026-11-07)。

每次请求的 request.json 保存完整 URL、全部参数和显式请求头，response.body 为原始正文，response.json 保存状态/头/耗时/哈希。Python urllib 与 Android HTTP 栈有差异，自动请求头与公共参数编码的边界见基线报告。

公开归档中的 `response.json` 已将 `x-request-ip` 和 `X-Agw-Info` 响应头替换为 `<redacted>`，不再视为未脱敏的原始头；`response.body` 正文和正文哈希未改动。逐文件脱敏记录见 `redaction.json`，`SHA256SUMS` 校验公开版本。原始元数据仅保存在本地被 Git 忽略的 `android/app/build/reports/home-evidence-original-headers/`，不会随仓库推送。目录内脚本快照仍为当时实际运行版本，后续采集使用仓库 `tools/probe-home-api.py` 的脱敏逻辑。

| # | 用例 / selected_items | HTTP / code | 部数 | 完整入参 | 原始出参 |
| --- | --- | --- | --- | --- | --- |
| 001 | `default` | 200 / 0 | 9 | [request](001-default/request.json) | [body](001-default/response.body) · [metadata](001-default/response.json) |
| 002 | `ranklist_hot` | 200 / 0 | 7 | [request](002-ranklist_hot/request.json) | [body](002-ranklist_hot/response.body) · [metadata](002-ranklist_hot/response.json) |
| 003 | `firstonlinetime_new` | 200 / 0 | 8 | [request](003-firstonlinetime_new/request.json) | [body](003-firstonlinetime_new/response.body) · [metadata](003-firstonlinetime_new/response.json) |
| 004 | `cate_757` | 200 / 0 | 8 | [request](004-cate_757/request.json) | [body](004-cate_757/response.body) · [metadata](004-cate_757/response.json) |
| 005 | `cate_739` | 200 / 0 | 9 | [request](005-cate_739/request.json) | [body](005-cate_739/response.body) · [metadata](005-cate_739/response.json) |
| 006 | `cate_1` | 200 / 0 | 9 | [request](006-cate_1/request.json) | [body](006-cate_1/response.body) · [metadata](006-cate_1/response.json) |
| 007 | `cate_1051` | 200 / 0 | 8 | [request](007-cate_1051/request.json) | [body](007-cate_1051/response.body) · [metadata](007-cate_1051/response.json) |
| 008 | `cate_261` | 200 / 0 | 8 | [request](008-cate_261/request.json) | [body](008-cate_261/response.body) · [metadata](008-cate_261/response.json) |
| 009 | `cate_316` | 200 / 0 | 9 | [request](009-cate_316/request.json) | [body](009-cate_316/response.body) · [metadata](009-cate_316/response.json) |
| 010 | `cate_29` | 200 / 0 | 9 | [request](010-cate_29/request.json) | [body](010-cate_29/response.body) · [metadata](010-cate_29/response.json) |
| 011 | `cate_1021` | 200 / 0 | 9 | [request](011-cate_1021/request.json) | [body](011-cate_1021/response.body) · [metadata](011-cate_1021/response.json) |
| 012 | `cate_4` | 200 / 0 | 9 | [request](012-cate_4/request.json) | [body](012-cate_4/response.body) · [metadata](012-cate_4/response.json) |
| 013 | `cate_11` | 200 / 0 | 9 | [request](013-cate_11/request.json) | [body](013-cate_11/response.body) · [metadata](013-cate_11/response.json) |
| 014 | `cate_127` | 200 / 0 | 9 | [request](014-cate_127/request.json) | [body](014-cate_127/response.body) · [metadata](014-cate_127/response.json) |
| 015 | `cate_273` | 200 / 0 | 9 | [request](015-cate_273/request.json) | [body](015-cate_273/response.body) · [metadata](015-cate_273/response.json) |
| 016 | `cate_308` | 200 / 0 | 8 | [request](016-cate_308/request.json) | [body](016-cate_308/response.body) · [metadata](016-cate_308/response.json) |
| 017 | `cate_390` | 200 / 0 | 9 | [request](017-cate_390/request.json) | [body](017-cate_390/response.body) · [metadata](017-cate_390/response.json) |
| 018 | `cate_3` | 200 / 0 | 9 | [request](018-cate_3/request.json) | [body](018-cate_3/response.body) · [metadata](018-cate_3/response.json) |
| 019 | `cate_19` | 200 / 0 | 7 | [request](019-cate_19/request.json) | [body](019-cate_19/response.body) · [metadata](019-cate_19/response.json) |
| 020 | `cate_36` | 200 / 0 | 8 | [request](020-cate_36/request.json) | [body](020-cate_36/response.body) · [metadata](020-cate_36/response.json) |
| 021 | `cate_37` | 200 / 0 | 8 | [request](021-cate_37/request.json) | [body](021-cate_37/response.body) · [metadata](021-cate_37/response.json) |
| 022 | `cate_43` | 200 / 0 | 8 | [request](022-cate_43/request.json) | [body](022-cate_43/response.body) · [metadata](022-cate_43/response.json) |
| 023 | `cate_165` | 200 / 0 | 9 | [request](023-cate_165/request.json) | [body](023-cate_165/response.body) · [metadata](023-cate_165/response.json) |
| 024 | `cate_266` | 200 / 0 | 9 | [request](024-cate_266/request.json) | [body](024-cate_266/response.body) · [metadata](024-cate_266/response.json) |
| 025 | `cate_303` | 200 / 0 | 9 | [request](025-cate_303/request.json) | [body](025-cate_303/response.body) · [metadata](025-cate_303/response.json) |
| 026 | `cate_382` | 200 / 0 | 8 | [request](026-cate_382/request.json) | [body](026-cate_382/response.body) · [metadata](026-cate_382/response.json) |
| 027 | `cate_395` | 200 / 0 | 9 | [request](027-cate_395/request.json) | [body](027-cate_395/response.body) · [metadata](027-cate_395/response.json) |
| 028 | `cate_439` | 200 / 0 | 9 | [request](028-cate_439/request.json) | [body](028-cate_439/response.body) · [metadata](028-cate_439/response.json) |
| 029 | `cate_466` | 200 / 0 | 9 | [request](029-cate_466/request.json) | [body](029-cate_466/response.body) · [metadata](029-cate_466/response.json) |
| 030 | `cate_475` | 200 / 0 | 9 | [request](030-cate_475/request.json) | [body](030-cate_475/response.body) · [metadata](030-cate_475/response.json) |
| 031 | `cate_598` | 200 / 0 | 6 | [request](031-cate_598/request.json) | [body](031-cate_598/response.body) · [metadata](031-cate_598/response.json) |
| 032 | `cate_842` | 200 / 0 | 9 | [request](032-cate_842/request.json) | [body](032-cate_842/response.body) · [metadata](032-cate_842/response.json) |
| 033 | `cate_1007` | 200 / 0 | 9 | [request](033-cate_1007/request.json) | [body](033-cate_1007/response.body) · [metadata](033-cate_1007/response.json) |
| 034 | `cate_1008` | 200 / 0 | 9 | [request](034-cate_1008/request.json) | [body](034-cate_1008/response.body) · [metadata](034-cate_1008/response.json) |
| 035 | `cate_1013` | 200 / 0 | 8 | [request](035-cate_1013/request.json) | [body](035-cate_1013/response.body) · [metadata](035-cate_1013/response.json) |
| 036 | `cate_1020` | 200 / 0 | 9 | [request](036-cate_1020/request.json) | [body](036-cate_1020/response.body) · [metadata](036-cate_1020/response.json) |
| 037 | `cate_1022` | 200 / 0 | 8 | [request](037-cate_1022/request.json) | [body](037-cate_1022/response.body) · [metadata](037-cate_1022/response.json) |
| 038 | `cate_1024` | 200 / 0 | 9 | [request](038-cate_1024/request.json) | [body](038-cate_1024/response.body) · [metadata](038-cate_1024/response.json) |
| 039 | `cate_1038` | 200 / 0 | 9 | [request](039-cate_1038/request.json) | [body](039-cate_1038/response.body) · [metadata](039-cate_1038/response.json) |
| 040 | `cate_1040` | 200 / 0 | 6 | [request](040-cate_1040/request.json) | [body](040-cate_1040/response.body) · [metadata](040-cate_1040/response.json) |
| 041 | `cate_1041` | 200 / 0 | 9 | [request](041-cate_1041/request.json) | [body](041-cate_1041/response.body) · [metadata](041-cate_1041/response.json) |
| 042 | `cate_1043` | 200 / 0 | 8 | [request](042-cate_1043/request.json) | [body](042-cate_1043/response.body) · [metadata](042-cate_1043/response.json) |
| 043 | `cate_1044` | 200 / 0 | 9 | [request](043-cate_1044/request.json) | [body](043-cate_1044/response.body) · [metadata](043-cate_1044/response.json) |
| 044 | `cate_1047` | 200 / 0 | 7 | [request](044-cate_1047/request.json) | [body](044-cate_1047/response.body) · [metadata](044-cate_1047/response.json) |
| 045 | `cate_1048` | 200 / 0 | 9 | [request](045-cate_1048/request.json) | [body](045-cate_1048/response.body) · [metadata](045-cate_1048/response.json) |
| 046 | `cate_1050` | 200 / 0 | 9 | [request](046-cate_1050/request.json) | [body](046-cate_1050/response.body) · [metadata](046-cate_1050/response.json) |
| 047 | `cate_1053` | 200 / 0 | 9 | [request](047-cate_1053/request.json) | [body](047-cate_1053/response.body) · [metadata](047-cate_1053/response.json) |
| 048 | `cate_20` | 200 / 0 | 9 | [request](048-cate_20/request.json) | [body](048-cate_20/response.body) · [metadata](048-cate_20/response.json) |
| 049 | `cate_28` | 200 / 0 | 8 | [request](049-cate_28/request.json) | [body](049-cate_28/response.body) · [metadata](049-cate_28/response.json) |
| 050 | `cate_84` | 200 / 0 | 9 | [request](050-cate_84/request.json) | [body](050-cate_84/response.body) · [metadata](050-cate_84/response.json) |
| 051 | `cate_85` | 200 / 0 | 9 | [request](051-cate_85/request.json) | [body](051-cate_85/response.body) · [metadata](051-cate_85/response.json) |
| 052 | `cate_94` | 200 / 0 | 9 | [request](052-cate_94/request.json) | [body](052-cate_94/response.body) · [metadata](052-cate_94/response.json) |
| 053 | `cate_378` | 200 / 0 | 8 | [request](053-cate_378/request.json) | [body](053-cate_378/response.body) · [metadata](053-cate_378/response.json) |
| 054 | `cate_387` | 200 / 0 | 9 | [request](054-cate_387/request.json) | [body](054-cate_387/response.body) · [metadata](054-cate_387/response.json) |
| 055 | `cate_470` | 200 / 0 | 9 | [request](055-cate_470/request.json) | [body](055-cate_470/response.body) · [metadata](055-cate_470/response.json) |
| 056 | `cate_737` | 200 / 0 | 9 | [request](056-cate_737/request.json) | [body](056-cate_737/response.body) · [metadata](056-cate_737/response.json) |
| 057 | `cate_760` | 200 / 0 | 9 | [request](057-cate_760/request.json) | [body](057-cate_760/response.body) · [metadata](057-cate_760/response.json) |
| 058 | `cate_844` | 200 / 0 | 9 | [request](058-cate_844/request.json) | [body](058-cate_844/response.body) · [metadata](058-cate_844/response.json) |
| 059 | `cate_901` | 200 / 0 | 8 | [request](059-cate_901/request.json) | [body](059-cate_901/response.body) · [metadata](059-cate_901/response.json) |
| 060 | `cate_1010` | 200 / 0 | 9 | [request](060-cate_1010/request.json) | [body](060-cate_1010/response.body) · [metadata](060-cate_1010/response.json) |
| 061 | `cate_1045` | 200 / 0 | 8 | [request](061-cate_1045/request.json) | [body](061-cate_1045/response.body) · [metadata](061-cate_1045/response.json) |
| 062 | `cate_1046` | 200 / 0 | 8 | [request](062-cate_1046/request.json) | [body](062-cate_1046/response.body) · [metadata](062-cate_1046/response.json) |
| 063 | `cate_1049` | 200 / 0 | 9 | [request](063-cate_1049/request.json) | [body](063-cate_1049/response.body) · [metadata](063-cate_1049/response.json) |
| 064 | `cate_1052` | 200 / 0 | 9 | [request](064-cate_1052/request.json) | [body](064-cate_1052/response.body) · [metadata](064-cate_1052/response.json) |
| 065 | `page1` | 200 / 0 | 9 | [request](065-page1/request.json) | [body](065-page1/response.body) · [metadata](065-page1/response.json) |
| 066 | `cate_4,cate_19` | 200 / 0 | 9 | [request](066-combo-school-system/request.json) | [body](066-combo-school-system/response.body) · [metadata](066-combo-school-system/response.json) |
| 067 | `cate_19,cate_4` | 200 / 0 | 9 | [request](067-combo-system-school/request.json) | [body](067-combo-system-school/response.body) · [metadata](067-combo-system-school/response.json) |
| 068 | `cate_nonexistent_probe_20261106` | 200 / 0 | 8 | [request](068-invalid-selector/request.json) | [body](068-invalid-selector/response.body) · [metadata](068-invalid-selector/response.json) |
