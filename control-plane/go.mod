module control-plane

go 1.24.0

toolchain go1.24.4

require (
	cloud.google.com/go/compute v1.23.4
	github.com/aws/aws-sdk-go-v2 v1.41.1
	github.com/aws/aws-sdk-go-v2/config v1.32.7
	github.com/aws/aws-sdk-go-v2/service/ec2 v1.283.0
	github.com/aws/smithy-go v1.24.0
	github.com/gin-gonic/gin v1.9.1
	github.com/google/uuid v1.6.0
	github.com/pkg/sftp v1.13.10
	go.etcd.io/etcd/client/v3 v3.6.7
	go.etcd.io/etcd/server/v3 v3.6.7
	google.golang.org/api v0.160.0
	google.golang.org/genproto v0.0.0-20240116215550-a9fa1716bcac
	gopkg.in/yaml.v3 v3.0.1
)

require (
	cloud.google.com/go/compute/metadata v0.7.0 // indirect
	github.com/aws/aws-sdk-go-v2/credentials v1.19.7 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.18.17 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.4.17 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.7.17 // indirect
	github.com/aws/aws-sdk-go-v2/internal/ini v1.8.4 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.4 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.13.17 // indirect
	github.com/aws/aws-sdk-go-v2/service/signin v1.0.5 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.30.9 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.35.13 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.41.6 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cenkalti/backoff/v4 v4.3.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/coreos/go-semver v0.3.1 // indirect
	github.com/coreos/go-systemd/v22 v22.5.0 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang-jwt/jwt/v5 v5.2.2 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/google/btree v1.1.3 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/s2a-go v0.1.7 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.2 // indirect
	github.com/googleapis/gax-go/v2 v2.12.0 // indirect
	github.com/gorilla/websocket v1.4.2 // indirect
	github.com/grpc-ecosystem/go-grpc-middleware/providers/prometheus v1.0.1 // indirect
	github.com/grpc-ecosystem/go-grpc-middleware/v2 v2.1.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.26.3 // indirect
	github.com/jonboulle/clockwork v0.5.0 // indirect
	github.com/klauspost/compress v1.17.9 // indirect
	github.com/kr/fs v0.1.0 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/prometheus/client_golang v1.20.5 // indirect
	github.com/prometheus/client_model v0.6.1 // indirect
	github.com/prometheus/common v0.62.0 // indirect
	github.com/prometheus/procfs v0.15.1 // indirect
	github.com/sirupsen/logrus v1.9.3 // indirect
	github.com/soheilhy/cmux v0.1.5 // indirect
	github.com/spf13/pflag v1.0.6 // indirect
	github.com/tmc/grpc-websocket-proxy v0.0.0-20201229170055-e5319fda7802 // indirect
	github.com/xiang90/probing v0.0.0-20190116061207-43a291ad63a2 // indirect
	go.etcd.io/bbolt v1.4.3 // indirect
	go.etcd.io/etcd/api/v3 v3.6.7 // indirect
	go.etcd.io/etcd/client/pkg/v3 v3.6.7 // indirect
	go.etcd.io/etcd/pkg/v3 v3.6.7 // indirect
	go.etcd.io/raft/v3 v3.6.0 // indirect
	go.opencensus.io v0.24.0 // indirect
	go.opentelemetry.io/auto/sdk v1.1.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.61.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.47.0 // indirect
	go.opentelemetry.io/otel v1.37.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.34.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.34.0 // indirect
	go.opentelemetry.io/otel/metric v1.37.0 // indirect
	go.opentelemetry.io/otel/sdk v1.37.0 // indirect
	go.opentelemetry.io/otel/trace v1.37.0 // indirect
	go.opentelemetry.io/proto/otlp v1.5.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.27.0 // indirect
	golang.org/x/oauth2 v0.30.0 // indirect
	golang.org/x/sync v0.18.0 // indirect
	golang.org/x/time v0.14.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20251213004720-97cd9d5aeac2 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251213004720-97cd9d5aeac2 // indirect
	google.golang.org/grpc v1.76.0 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
	sigs.k8s.io/json v0.0.0-20211020170558-c049b76a60c6 // indirect
	sigs.k8s.io/yaml v1.4.0 // indirect
)

require (
	github.com/bytedance/sonic v1.9.1 // indirect; 间接依赖
	github.com/chenzhuoyu/base64x v0.0.0-20221115062448-fe3a3abad311 // indirect; 间接依赖
	github.com/gabriel-vasile/mimetype v1.4.2 // indirect; 间接依赖
	github.com/gin-contrib/sse v0.1.0 // indirect; 间接依赖
	github.com/go-playground/locales v0.14.1 // indirect; 间接依赖
	github.com/go-playground/universal-translator v0.18.1 // indirect; 间接依赖
	github.com/go-playground/validator/v10 v10.15.0 // indirect; 间接依赖
	github.com/goccy/go-json v0.10.2 // indirect; 间接依赖
	github.com/json-iterator/go v1.1.12 // indirect; 间接依赖
	github.com/klauspost/cpuid/v2 v2.2.4 // indirect; 间接依赖
	github.com/leodido/go-urn v1.2.4 // indirect; 间接依赖
	github.com/mattn/go-isatty v0.0.19 // indirect; 间接依赖
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect; 间接依赖
	github.com/modern-go/reflect2 v1.0.2 // indirect; 间接依赖
	github.com/pelletier/go-toml/v2 v2.0.8 // indirect; 间接依赖
	github.com/twitchyliquid64/golang-asm v0.15.1 // indirect; 间接依赖
	github.com/ugorji/go/codec v1.2.11 // indirect; 间接依赖
	golang.org/x/arch v0.3.0 // indirect; 间接依赖
	golang.org/x/crypto v0.43.0 // 间接依赖
	golang.org/x/net v0.46.0 // indirect; 间接依赖
	golang.org/x/sys v0.37.0 // indirect; 间接依赖
	golang.org/x/text v0.30.0 // indirect; 间接依赖
	google.golang.org/protobuf v1.36.11 // 间接依赖
)
