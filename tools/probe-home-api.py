#!/usr/bin/env python3
"""long: 将首页真实请求逐次归档，枚举由本次响应发现，避免旧文档变成隐含接口契约。"""

import argparse
import datetime as dt
import hashlib
import json
import platform
import secrets
import time
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path


def write_json(path, data):
    path.write_text(json.dumps(data, ensure_ascii=False, indent=2) + "\n")


def analyze(payload, tab_type=8):
    data = json.loads(payload)
    tabs = data.get("data", {}).get("tab_item", [])
    target = next((t for t in tabs if t.get("tab_type") == tab_type), {})
    selectors, videos = [], []

    def walk(node):
        if isinstance(node, dict):
            if isinstance(node.get("cell_selector"), dict):
                selectors.append(node["cell_selector"])
            videos.extend(node.get("video_data") or [])
            for key, value in node.items():
                if key != "video_data":
                    walk(value)
        elif isinstance(node, list):
            for value in node:
                walk(value)

    walk(target)
    selected = list(dict.fromkeys(
        item["selector_item_id"]
        for selector in selectors
        for row in [selector.get("outer_row", {})] + selector.get("inner_rows", [])
        for item in row.get("items", []) if item.get("is_selected")
    ))
    ids = list(dict.fromkeys(v.get("series_id") or v.get("vid") for v in videos
                             if v.get("series_id") or v.get("vid")))
    return {
        "businessCode": data.get("code"), "message": data.get("message"),
        "items": len(ids), "seriesIds": ids, "selectedIds": selected,
        "nextOffset": target.get("next_offset"), "hasMoreField": target.get("has_more"),
        "sessionId": target.get("session_id", ""), "selectors": selectors,
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True, help="新的证据目录；已有目录不覆盖")
    parser.add_argument("--all-filters", action="store_true", help="逐项请求首屏返回的全部去重筛选值")
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[1]
    source = root / "android/app/src/main/assets/recipes/hongguo.json"
    recipe = json.loads(source.read_text())
    endpoint = recipe["endpoints"]["home"]
    if endpoint["method"] != "GET" or endpoint["sign"] != ["none"]:
        parser.error("当前脚本仅验收无签名 GET 首页；配方协议已变化时须先更新探测器")
    version = endpoint["versionCode"]
    if version != "65532":
        parser.error("首页版本已改变，须先与 RecipeHttp.commonParams 核对公共参数")
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=False)
    device = str(secrets.randbelow(8 * 10**18) + 10**18)
    install = str(secrets.randbelow(8 * 10**18) + 10**18)
    # long: 使用本轮生成的独立测试身份，不读取用户手机的账号、Cookie 或设备标识。
    common = {
        "iid": install, "device_id": device, "ac": "wifi", "channel": "hongguo_baidu_pz_kaiping0105_android",
        "aid": "8662", "app_name": "novelread", "version_code": version, "version_name": "6.5.5.32",
        "device_platform": "android", "os": "android", "ssmix": "a", "device_type": "Redmi Note 8 Pro",
        "device_brand": "Redmi", "language": "zh", "os_api": "36", "os_version": "16",
        "manifest_version_code": version, "resolution": "1080*2198", "dpi": "440",
        "update_version_code": version, "pv_player": version, "gender": "2", "need_personal_recommend": "1",
        "player_so_load": "1", "is_android_pad_screen": "0", "host_abi": "arm64-v8a",
        "dragon_device_type": "phone", "rom_version": "BP4A.251205.006 release-keys", "compliance_status": "0",
        "cdid": "1f1d14b3-8c25-4733-9f6b-2fdc01640081",
    }
    base_url = recipe["hosts"][endpoint["host"]].rstrip("/") + endpoint["path"]
    write_json(output / "run.json", {
        "startedAt": dt.datetime.now().astimezone().isoformat(), "recipeVersion": recipe["version"],
        "recipeSha256": hashlib.sha256(source.read_bytes()).hexdigest(),
        "commonParamsSource": "android/app/src/main/java/com/duanju/player/core/RecipeHttp.kt",
        "transport": f"Python urllib {platform.python_version()}; 非手机 HTTP 栈",
        "identity": "本轮随机生成，非用户身份；每次 request.json 均完整保留",
        "sign": "none", "automaticRetries": False,
    })
    results = []

    def request(label, selected=None, offset=0, session=""):
        folder = output / f"{len(results) + 1:03d}-{label}"
        folder.mkdir()
        variables = {"tabType": "8", "offset": str(offset), "sessionId": session,
                     "selectedItems": selected or ""}
        query = common.copy()
        for key, template in endpoint["params"]:
            value = variables[template[1:]] if template.startswith("$") else template
            if value:
                query[key] = value
        url = base_url + "?" + urllib.parse.urlencode(query, quote_via=urllib.parse.quote)
        headers = {"User-Agent": f"Python-urllib/{platform.python_version_tuple()[0]}.{platform.python_version_tuple()[1]}",
                   "Accept-Encoding": "identity", "Connection": "close", "Host": urllib.parse.urlsplit(url).netloc,
                   **endpoint.get("headers", {})}
        started = dt.datetime.now().astimezone().isoformat()
        write_json(folder / "request.json", {
            "startedAt": started, "method": "GET", "baseUrl": base_url, "url": url,
            "query": query, "headers": headers, "body": None, "sign": "none",
        })
        response, body, error, status, summary = None, b"", None, None, None
        begin = time.monotonic()
        try:
            req = urllib.request.Request(url, headers=headers)
            # long: 每次发送对应一份记录，不隐藏重试或重定向，否则无法复盘实际请求次数。
            class NoRedirect(urllib.request.HTTPRedirectHandler):
                def redirect_request(self, req, fp, code, msg, hdrs, newurl):
                    return None
            opener = urllib.request.build_opener(NoRedirect)
            try:
                response = opener.open(req, timeout=30)
            except urllib.error.HTTPError as exc:
                response = exc
            with response:
                status = response.status
                # long: 公开回归归档保留业务响应，隐藏出口 IP 和不透明网关信息，避免暴露采集环境。
                private_headers = {"set-cookie", "x-request-ip", "x-agw-info"}
                response_headers = [[key, "<redacted>" if key.lower() in private_headers else value]
                                    for key, value in response.headers.items()]
                body = response.read()
                (folder / "response.body").write_bytes(body)
                write_json(folder / "response.json", {
                    "status": status, "reason": response.reason, "headers": response_headers,
                    "bytes": len(body), "sha256": hashlib.sha256(body).hexdigest(),
                    "finishedAt": dt.datetime.now().astimezone().isoformat(),
                    "elapsedMs": round((time.monotonic() - begin) * 1000),
                })
            summary = analyze(body)
        except Exception as exc:
            error = f"{type(exc).__name__}: {exc}"
            write_json(folder / "error.json", {"error": error, "status": status,
                       "elapsedMs": round((time.monotonic() - begin) * 1000)})
        entry = {"case": label, "directory": folder.name, "selectedItems": selected, "offset": offset,
                 "httpStatus": status, "error": error, **(summary or {})}
        results.append(entry)
        write_json(folder / "analysis.json", entry)
        write_json(output / "index.json", results)
        print(f"{folder.name}: HTTP {status}, code={(summary or {}).get('businessCode')}, "
              f"items={(summary or {}).get('items')}, selected={(summary or {}).get('selectedIds')}, error={error}", flush=True)
        time.sleep(.3)
        return summary

    first = request("default")
    if not first or first["businessCode"] != 0 or not first["items"]:
        raise SystemExit("首个请求不可用；保留证据并停止，避免在未知响应上扩大发送")
    selectors = first["selectors"]
    unique = {item["selector_item_id"]: item["show_name"] for selector in selectors
              for row in [selector.get("outer_row", {})] + selector.get("inner_rows", [])
              for item in row.get("items", []) if item.get("selector_item_id")}
    write_json(output / "filter-catalog.json", {"selectors": selectors, "uniqueIds": unique})
    if args.all_filters:
        for filter_id in unique:
            request(filter_id, selected=filter_id)
    request("page1", offset=first["nextOffset"], session=first["sessionId"])
    request("combo-school-system", selected="cate_4,cate_19")
    request("combo-system-school", selected="cate_19,cate_4")
    request("invalid-selector", selected="cate_nonexistent_probe_20261106")
    write_json(output / "completed.json", {"finishedAt": dt.datetime.now().astimezone().isoformat(),
                                          "requestCount": len(results)})


if __name__ == "__main__":
    main()
