module xiaozhi-esp32-server-golang

go 1.24.2

toolchain go1.24.11

require (
	github.com/ThinkInAIXYZ/go-mcp v0.2.19
	github.com/antonfisher/nested-logrus-formatter v1.3.1
	github.com/asaskevich/EventBus v0.0.0-20200907212545-49d423059eef
	github.com/bytedance/sonic v1.15.0
	github.com/cloudwego/eino v0.3.40
	github.com/cloudwego/eino-ext/components/model/ollama v0.0.0-20250530094010-bd1c4fc20bbe
	github.com/cloudwego/eino-ext/components/model/openai v0.0.0-20250530094010-bd1c4fc20bbe
	github.com/difyz9/edge-tts-go v0.0.2
	github.com/eclipse/paho.mqtt.golang v1.5.1
	github.com/getkin/kin-openapi v0.118.0
	github.com/gin-gonic/gin v1.10.1
	github.com/go-audio/audio v1.0.0
	github.com/go-audio/wav v1.1.0
	github.com/golang-jwt/jwt/v4 v4.5.2
	github.com/google/uuid v1.6.0
	github.com/gopxl/beep v1.4.1
	github.com/gorilla/websocket v1.5.3
	github.com/hackers365/go-webrtcvad v0.0.0-20250711024710-dde35479e077
	github.com/hackers365/mem0-go v1.0.2
	github.com/hraban/opus v0.0.0-20220302220929-eeacdbcb92d0
	github.com/lestrrat-go/file-rotatelogs v2.4.0+incompatible
	github.com/mark3labs/mcp-go v0.36.0
	github.com/memodb-io/memobase/src/client/memobase-go v0.0.0-20251008012534-936f45328453
	github.com/mitchellh/hashstructure/v2 v2.0.2
	github.com/mochi-mqtt/server/v2 v2.7.9
	github.com/orcaman/concurrent-map/v2 v2.0.1
	github.com/redis/go-redis/v9 v9.7.3
	github.com/sirupsen/logrus v1.9.3
	github.com/spf13/viper v1.20.1
	github.com/streamer45/silero-vad-go v0.2.1
	github.com/stretchr/testify v1.11.1
	github.com/tmaxmax/go-sse v0.11.0
	go.uber.org/zap v1.27.0
	gopkg.in/hraban/opus.v2 v2.0.0-20230925203106-0188a62cb302
	gorm.io/gorm v1.30.0
	voice_server v0.0.0-00010101000000-000000000000
	xiaozhi/manager/backend v0.0.0-00010101000000-000000000000
)

// 主进程内嵌 manager HTTP 时引用 backend 子模块
replace xiaozhi/manager/backend => ./manager/backend

// 主进程内嵌 asr_server 时引用 asr_server 子模块（Git submodule）
replace voice_server => ./asr_server

require (
	github.com/bahlo/generic-list-go v0.2.0 // indirect
	github.com/buger/jsonparser v1.1.1 // indirect
	github.com/bytedance/gopkg v0.1.3 // indirect
	github.com/bytedance/sonic/loader v0.5.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/cloudwego/base64x v0.1.6 // indirect
	github.com/cloudwego/eino-ext/libs/acl/openai v0.0.0-20250519084852-38fafa73d9ea // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/fsnotify/fsnotify v1.9.0 // indirect
	github.com/gabriel-vasile/mimetype v1.4.3 // indirect
	github.com/gin-contrib/cors v1.7.2 // indirect
	github.com/gin-contrib/sse v0.1.0 // indirect
	github.com/glebarez/go-sqlite v1.21.2 // indirect
	github.com/glebarez/sqlite v1.11.0 // indirect
	github.com/go-audio/riff v1.0.0 // indirect
	github.com/go-openapi/jsonpointer v0.19.5 // indirect
	github.com/go-openapi/swag v0.19.5 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/go-playground/validator/v10 v10.20.0 // indirect
	github.com/go-sql-driver/mysql v1.7.0 // indirect
	github.com/go-viper/mapstructure/v2 v2.2.1 // indirect
	github.com/goccy/go-json v0.10.2 // indirect
	github.com/goph/emperror v0.17.2 // indirect
	github.com/hajimehoshi/go-mp3 v0.3.4 // indirect
	github.com/invopop/jsonschema v0.13.0 // indirect
	github.com/invopop/yaml v0.1.0 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	github.com/jonboulle/clockwork v0.5.0 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/k2-fsa/sherpa-onnx-go v1.12.4 // indirect
	github.com/k2-fsa/sherpa-onnx-go-linux v1.12.4 // indirect
	github.com/k2-fsa/sherpa-onnx-go-macos v1.12.4 // indirect
	github.com/k2-fsa/sherpa-onnx-go-windows v1.12.4 // indirect
	github.com/klauspost/cpuid/v2 v2.2.9 // indirect
	github.com/leodido/go-urn v1.4.0 // indirect
	github.com/lestrrat-go/strftime v1.1.0 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/meguminnnnnnnnn/go-openai v0.0.0-20250408071642-761325becfd6 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/mohae/deepcopy v0.0.0-20170929034955-c48cc78d4826 // indirect
	github.com/nikolalohinski/gonja v1.5.3 // indirect
	github.com/ollama/ollama v0.5.12 // indirect
	github.com/pelletier/go-toml/v2 v2.2.3 // indirect
	github.com/perimeterx/marshmallow v1.1.4 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/qdrant/go-client v1.16.2 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/rs/xid v1.4.0 // indirect
	github.com/sagikazarmark/locafero v0.7.0 // indirect
	github.com/slongfield/pyfmt v0.0.0-20220222012616-ea85ff4c361f // indirect
	github.com/sourcegraph/conc v0.3.0 // indirect
	github.com/spf13/afero v1.12.0 // indirect
	github.com/spf13/cast v1.7.1 // indirect
	github.com/spf13/pflag v1.0.6 // indirect
	github.com/subosito/gotenv v1.6.0 // indirect
	github.com/tidwall/gjson v1.18.0 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.0 // indirect
	github.com/twitchyliquid64/golang-asm v0.15.1 // indirect
	github.com/ugorji/go/codec v1.2.12 // indirect
	github.com/wk8/go-ordered-map/v2 v2.1.8 // indirect
	github.com/yargevad/filepathx v1.0.0 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	go.uber.org/multierr v1.10.0 // indirect
	golang.org/x/arch v0.11.0 // indirect
	golang.org/x/crypto v0.44.0 // indirect
	golang.org/x/exp v0.0.0-20231110203233-9a3e6036ecaa // indirect
	golang.org/x/net v0.47.0 // indirect
	golang.org/x/sync v0.18.0 // indirect
	golang.org/x/sys v0.38.0 // indirect
	golang.org/x/text v0.31.0 // indirect
	golang.org/x/time v0.14.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251111163417-95abcf5c77ba // indirect
	google.golang.org/grpc v1.76.0 // indirect
	google.golang.org/protobuf v1.36.10 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	gorm.io/driver/mysql v1.5.6 // indirect
	modernc.org/libc v1.22.5 // indirect
	modernc.org/mathutil v1.5.0 // indirect
	modernc.org/memory v1.5.0 // indirect
	modernc.org/sqlite v1.23.1 // indirect
)
