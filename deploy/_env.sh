#!/usr/bin/env bash
# 读取 deploy/.env 中的单个键。
#
# 为什么不用 source：.env 里有 BLOG_DB_DSN=root:xxx@tcp(127.0.0.1:3306)/blog?...
# 这类未加引号的值，bash source 会在括号处直接语法报错。
# 这里按「第一个 = 之后的内容整体取值」，对环境文件格式不做额外假设。
env_get() {
  local key="$1" default="${2-}" line
  [[ -f "${ENV_FILE:-deploy/.env}" ]] || { echo "$default"; return; }
  line=$(grep -E "^${key}=" "${ENV_FILE:-deploy/.env}" | tail -n 1 || true)
  if [[ -z "$line" ]]; then
    echo "$default"
  else
    printf '%s' "${line#*=}" | sed -e 's/[[:space:]]*$//' -e 's/^"//' -e 's/"$//'
  fi
}
