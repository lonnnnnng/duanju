#!/usr/bin/env bash
# 把内置配方同步成可直接分发的签名产物（recipes/recipe.json + .sig）。
#
# 用法：
#   tools/publish-recipe.sh                 # 用本地私钥签名
#   RECIPE_SIGNING_KEY_FILE=... tools/publish-recipe.sh
#
# 改完 android/app/src/main/assets/recipes/hongguo.json 后执行一次，
# 提交 3 个文件即可让已安装的 App 通过 jsDelivr / raw / Release 任一通道更新。
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SRC="$ROOT/android/app/src/main/assets/recipes/hongguo.json"
KEY="${RECIPE_SIGNING_KEY_FILE:-$ROOT/android/keystore/recipe/recipe-private.pem}"
OUT="$ROOT/recipes"

[ -f "$SRC" ] || { echo "缺少配方: $SRC" >&2; exit 1; }
[ -f "$KEY" ] || { echo "缺少私钥: $KEY（可用 RECIPE_SIGNING_KEY_FILE 指定）" >&2; exit 1; }

python3 -c "import json;d=json.load(open('$SRC'));print('配方版本:',d['version'],'更新:',d.get('updated',''))"

mkdir -p "$OUT"
cp "$SRC" "$OUT/recipe.json"
openssl dgst -sha256 -sign "$KEY" -out "$OUT/recipe.json.sig" "$OUT/recipe.json"

# 自检：用私钥导出公钥验证
openssl ec -in "$KEY" -pubout -out /tmp/recipe-pub-check.pem 2>/dev/null
openssl dgst -sha256 -verify /tmp/recipe-pub-check.pem -signature "$OUT/recipe.json.sig" "$OUT/recipe.json"
echo "已生成: recipes/recipe.json + recipes/recipe.json.sig"
