#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

export RUN_ONLINE_TESTS="${RUN_ONLINE_TESTS:-1}"

usage() {
  cat <<'USAGE'
Usage:
  scripts/run_online_tests.sh [target]

Targets:
  all              Run ASR + LLM + TTS online tests (default)
  asr              Run all ASR online tests
  llm              Run all LLM online tests
  tts              Run OpenAI + Doubao TTS online tests
  asr:funasr
  asr:aliyun_funasr
  asr:doubao
  asr:aliyun_qwen3
  asr:xunfei
  llm:openai
  llm:dify
  llm:coze
  tts:openai
  tts:doubao_http
  tts:doubao_ws

Notes:
  - RUN_ONLINE_TESTS defaults to 1 in this script.
  - Missing provider env vars will cause corresponding test cases to be skipped.
USAGE
}

run_cmd() {
  echo "+ $*"
  "$@"
}

run_asr_all() {
  run_cmd go test -v ./internal/domain/asr -run 'TestASROnlineProviders'
}

run_llm_all() {
  run_cmd go test -v ./internal/domain/llm -run 'TestLLMOnlineProviders'
}

run_tts_all() {
  run_cmd go test -v ./internal/domain/tts/openai -run 'TestOpenAITTSOnline'
  run_cmd go test -v ./internal/domain/tts/doubao -run 'TestDoubao(TTSOnline|WSTTSOnline)'
}

TARGET="${1:-all}"

case "$TARGET" in
  all)
    run_asr_all
    run_llm_all
    run_tts_all
    ;;
  asr)
    run_asr_all
    ;;
  llm)
    run_llm_all
    ;;
  tts)
    run_tts_all
    ;;

  asr:funasr)
    run_cmd go test -v ./internal/domain/asr -run 'TestASROnlineProviders/funasr'
    ;;
  asr:aliyun_funasr)
    run_cmd go test -v ./internal/domain/asr -run 'TestASROnlineProviders/aliyun_funasr'
    ;;
  asr:doubao)
    run_cmd go test -v ./internal/domain/asr -run 'TestASROnlineProviders/doubao'
    ;;
  asr:aliyun_qwen3)
    run_cmd go test -v ./internal/domain/asr -run 'TestASROnlineProviders/aliyun_qwen3'
    ;;
  asr:xunfei)
    run_cmd go test -v ./internal/domain/asr -run 'TestASROnlineProviders/xunfei'
    ;;

  llm:openai)
    run_cmd go test -v ./internal/domain/llm -run 'TestLLMOnlineProviders/eino_openai'
    ;;
  llm:dify)
    run_cmd go test -v ./internal/domain/llm -run 'TestLLMOnlineProviders/dify'
    ;;
  llm:coze)
    run_cmd go test -v ./internal/domain/llm -run 'TestLLMOnlineProviders/coze'
    ;;

  tts:openai)
    run_cmd go test -v ./internal/domain/tts/openai -run 'TestOpenAITTSOnline'
    ;;
  tts:doubao_http)
    run_cmd go test -v ./internal/domain/tts/doubao -run 'TestDoubaoTTSOnline'
    ;;
  tts:doubao_ws)
    run_cmd go test -v ./internal/domain/tts/doubao -run 'TestDoubaoWSTTSOnline'
    ;;

  -h|--help|help)
    usage
    ;;
  *)
    echo "Unknown target: $TARGET" >&2
    usage
    exit 1
    ;;
esac
