#!/usr/bin/env bash

set -euo pipefail

# 用于 macOS 发布包：将开发机源码绝对路径 rpath 改为相对可执行文件路径。
# 适用目录结构：
#   ./xiaozhi_server
#   ./ten-vad/lib/macOS/ten_vad.framework

if [[ $# -ne 1 ]]; then
  echo "用法: $0 <xiaozhi_server二进制路径>" >&2
  exit 1
fi

BIN_PATH="$1"
TARGET_RPATH="@executable_path/ten-vad/lib/macOS"

if [[ ! -f "$BIN_PATH" ]]; then
  echo "二进制不存在: $BIN_PATH" >&2
  exit 1
fi

if ! command -v otool >/dev/null 2>&1; then
  echo "缺少 otool，请安装 Xcode Command Line Tools" >&2
  exit 1
fi

if ! command -v install_name_tool >/dev/null 2>&1; then
  echo "缺少 install_name_tool，请安装 Xcode Command Line Tools" >&2
  exit 1
fi

CURRENT_RPATHS=()
while IFS= read -r line; do
  CURRENT_RPATHS+=("$line")
done < <(
  otool -l "$BIN_PATH" | awk '
    $1 == "cmd" && $2 == "LC_RPATH" { in_rpath = 1; next }
    in_rpath && $1 == "path" { print $2; in_rpath = 0 }
  '
)

if [[ ${#CURRENT_RPATHS[@]} -eq 0 ]]; then
  echo "未检测到 LC_RPATH，准备直接写入目标 rpath"
fi

for rpath in "${CURRENT_RPATHS[@]}"; do
  if [[ "$rpath" == "$TARGET_RPATH" ]]; then
    continue
  fi
  install_name_tool -delete_rpath "$rpath" "$BIN_PATH" 2>/dev/null || true
done

if ! otool -l "$BIN_PATH" | grep -Fq "path $TARGET_RPATH "; then
  install_name_tool -add_rpath "$TARGET_RPATH" "$BIN_PATH"
fi

echo "已写入 rpath: $TARGET_RPATH"
