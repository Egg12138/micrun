<file_path>
micrun/README.md
</file_path>

<edit_description>
优化 README.md 的结构和排版，将内容重新组织为更合理的层次，并翻译说明性的英文部分。
</edit_description>

# MicRun

MicRun 是一个基于 containerd shimv2 的容器运行时，专为 Mica 项目设计，用于在不同 CPU 核上运行 RTOS（实时操作系统）。

代码中加了一些中文注释和说明，但是量有点大，所以会发现注释比较残缺

## 待办事项

### 短期待办（天级）

- **AI 整理该文档**
- **yocto 全量构建**
- **修复 IO 错误**
- **改善 netns 处理**
- **修复 metrics 错误**
- **修复 pty 错误**
- **删除冗余定义**
- **重构代码** -> 获取一个缩小的最小版本
- **修复 goroutine 泄漏和可能的阻塞**
- **考虑一个适当的管理策略**，用于 mica-image-builder（在 yocto 中），uv 和 poetry 可能不是这个案例的好选择

### 长期待办（周级）

考虑：

- **CI 设置**
- **更新 mica-image-builder**：
  > bundle 内容：添加一个 client.conf
  > 重命名为 micrun-image-builder
- **是否值得实现一个 micrun 作为通用运行时**，能够处理标准 Linux OCI 镜像？
  > 1. ~~手动实现~~
  > 2. 发送请求给其他运行时：lcr、runc、crun、gvisor、youki 等.. 当容器是一个标准 Linux OCI 镜像时，通过注解过滤
- **youki** (0.5.7, arm64, musl)，二进制大小 5.6MB；大约 `200%` runc 的速度；仓库活动：相当活跃但明显下降；yocto 中构建缓慢（rustc + 下载依赖）
- **crun** (1.25.1, arm64, glibc)，二进制大小 3.1MB；大约 `400%` runc 的速度；仓库活动：相当活跃；
- **lcr**（与 iSulad 一起，不是常见选择用于其他容器引擎）
- ==> **crun** 是首选：轻量级、快速，并适合 openEuler Embedded 构建工具

### 改进

- **循环引用**（container <-> sandbox）
  > cntr.Container.sandbox, cntr.Sandbox.containers
  > 例如，cntr.Container.delete() 调用 sandbox 方法，而 cntr.sandbox.Delete 调用 container 方法
  > 这可能很好地移除 c.sandbox.methods

## 安装和注册运行时

### 注册运行时

通过 `--runtime io.containerd.<runtime name>` 选项，用户可以指定运行容器的运行时（如果在 `$PATH` 上安装）。我们可以使用 containerd shim 运行时 [无需在 PATH 上安装](https://docs.docker.com/engine/daemon/alternative-runtimes/#use-a-containerd-shim-without-installing-on-path)

#### 注册

#### 在 containerd 上注册

通常，在 `/etc/containerd/config.toml` 中添加一个新插件：

```
[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.micrun]
  runtime_type = "io.containerd.mica.v2"
  pod_annotations = ["org.openeuler.micran."]

[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.micrun]
  # 保持为空
  # micrun 配置选项设计不稳定
  # 我们从 mcs 仓库中删除了这些代码
```

#### 注册为 Kubernetes RuntimeClass

```yaml
version: v1
runtimeClass:
  name: micrun
  type: RuntimeClass

```

```shell
kubelet --cpu-manager-policy=static
# isolcpus, nohz_full, ... 可以自定义
```

推荐

### 使用运行时

使用 nerdctl

```shell
# 注意，在 nerdctl 中 '--label' 选项不是 docker 中的 "Label"，它是 "Annotation"
# 因此 -l 选项将注解传递给容器 oci 配置
nerdctl run -d --runtime io.containerd.mica.v2 -l org.openeuler.micran.auto_disconnect=true <image>
nerdctl update --memory 1024m  <container_id>
```

使用 ctr (containerd 测试工具用于开发者)

```shell
ctr container create --runtime io.containerd.mica.v2 -t --annotation org.openeuler.micran.auto_disconnect=true <image> <container_id>
ctr task start <container_id>
ctr task kill <container_id>
ctr task del <container_id>
```

## 开发指南

## 计划

* 重写 mica-image-builder，它只是一个 vibe 编码演示工件
  > 肮脏的逻辑
  > 不优雅和不安全的实现
* yocto
* 更深入地集成到 mica 库中
  > 更通用的 pedestal 接口
  > 从 Linux 到 RTOS 的共享文件系统
  > 快照设计，我有一些实验性想法：
  * 1. 模拟 RTOS overlayfs
  * 2. 维护一个层修改记录，应用一个关于它的热补丁给 RTOS

## MicRun 实现

### 架构

这个最小预览版本，我保持代码结构简单和模块化：

- **容器引擎**
  - **shim**
    - shim 生命周期：New, StartShim, Cleanup, ...
    - shim 任务服务：container, sandbox, pod container: Start, Kill, Delete, ...
    - shim io：二进制 IO, 文件 IO, 管道 IO
    - shim 工具
  - **runtime 核心**
  - **libs**
    - libmica
    - pedestal
- **mica**

### Shim

#### Sandbox

为什么维护一个 sandbox 结构，当 containerd SandboxAPI 没有启用？
> 问题是答案：维护一个 Infra（像 pause 容器）容器是一个变通方法
> 迁移到 containerd SandboxAPI 是未来，
> 在 sandbox 内部管理和单个容器、pod 容器不是麻烦的，所以我们这样做了。


### 挂载

通常，*挂载* 在 Task Create 时设置，类型定义如下：

```go
// containerd 1.7.x
type Mount struct {
	Type string `protobuf:"bytes,1,opt,name=type,proto3" json:"type,omitempty"`
	Source string `protobuf:"bytes,2,opt,name=source,proto3" json:"source,omitempty"`
	Target string `protobuf:"bytes,3,opt,name=target,proto3" json:"target,omitempty"`
	// Options 指定零个或多个 fstab 风格的挂载选项。
	Options []string `protobuf:"bytes,4,rep,name=options,proto3" json:"options,omitempty"`
}
```

**Containerd 如何处理挂载选项：**
```go
// mount/mount_linux.go

// parseMountOptions 接受 fstab 风格的挂载选项并解析它们以用于标准 mount() 系统调用
func parseMountOptions(options []string) (int, []string, bool) {
	var (
		flag    int
		losetup bool
		data    []string
	)
	loopOpt := "loop"
	flags := map[string]struct {
		clear bool
		flag  int
	}{
		"async":         {true, unix.MS_SYNCHRONOUS},
		"atime":         {true, unix.MS_NOATIME},
		"bind":          {false, unix.MS_BIND},
		"defaults":      {false, 0},
		"dev":           {true, unix.MS_NODEV},
		"diratime":      {true, unix.MS_NODIRATIME},
		"dirsync":       {false, unix.MS_DIRSYNC},
		"exec":          {true, unix.MS_NOEXEC},
		"mand":          {false, unix.MS_MANDLOCK},
		"noatime":       {false, unix.MS_NOATIME},
		"nodev":         {false, unix.MS_NODEV},
		"nodiratime":    {false, unix.MS_NODIRATIME},
		"noexec":        {false, unix.MS_NOEXEC},
		"nomand":        {true, unix.MS_MANDLOCK},
		"norelatime":    {true, unix.MS_RELATIME},
		"nostrictatime": {true, unix.MS_STRICTATIME},
		"nosuid":        {false, unix.MS_NOSUID},
		"rbind":         {false, unix.MS_BIND | unix.MS_REC},
		"relatime":      {false, unix.MS_RELATIME},
		"remount":       {false, unix.MS_REMOUNT},
		"ro":            {false, unix.MS_RDONLY},
		"rw":            {true, unix.MS_RDONLY},
		"strictatime":   {false, unix.MS_STRICTATIME},
		"suid":          {true, unix.MS_NOSUID},
		"sync":          {false, unix.MS_SYNCHRONOUS},
	}
	for _, o := range options {
		// 如果选项不存在于 flags 表中或平台不支持该标志，
		// 那么它是一个特定文件系统类型的数据值
		// flags 组合连接......
	}
	return flag, data, losetup
}
```

在哪里找到支持的挂载类型？

> 散布在 containerd 源代码中！
> 例如，`snapshots/native/native_default.go` 声明了 `const mountType = "bind"`

再看一下：

```golang
// os: linux
// oci/mounts.go

func defaultMounts() []specs.Mount {
	return []specs.Mount{
		{
			Destination: "/proc",
			Type:        "proc",
			Source:      "proc",
			Options:     []string{"nosuid", "noexec", "nodev"},
		},
		{
			Destination: "/dev",
			Type:        "tmpfs",
			Source:      "tmpfs",
			Options:     []string{"nosuid", "strictatime", "mode=755", "size=65536k"},
		},
		{
			Destination: "/dev/pts",
			Type:        "devpts",
			Source:      "devpts",
			Options:     []string{"nosuid", "noexec", "newinstance", "ptmxmode=0666", "mode=0620", "gid=5"},
		},
		{
			Destination: "/dev/shm",
			Type:        "tmpfs",
			Source:      "shm",
			Options:     []string{"nosuid", "noexec", "nodev", "mode=1777", "size=65536k"},
		},
		{
			Destination: "/dev/mqueue",
			Type:        "mqueue",
			Source:      "mqueue",
			Options:     []string{"nosuid", "noexec", "nodev"},
		},
		{
			Destination: "/sys",
			Type:        "sysfs",
			Source:      "sysfs",
			Options:     []string{"nosuid", "noexec", "nodev", "ro"},
		},
		{
			Destination: "/run",
			Type:        "tmpfs",
			Source:      "tmpfs",
			Options:     []string{"nosuid", "strictatime", "mode=755", "size=65536k"},
		},
	}
}

```

---

对于 RTOS: zephyr, uniproton？

一个方法是维护一个 containerd 源代码中的文件系统定义：糟糕实践

### Snapshot

### Metrics

### Tracing

**要点摘要**（先读这段再看下方细节与代码）：

- **分布式追踪的核心**：span 与上下文传播（context propagation，通常使用 W3C traceparent）。
- **通信机制**：containerd <-> shimv2 的通信使用 ttrpc（轻量 RPC），要让追踪连贯，需要在 containerd 调用 shim 前注入 trace headers，在 shim 的 ttrpc server 端提取并以其为 parent 启动子 span。
- **内部传递**：在 shim 内部你也应把 trace 上下文传给下游（调用 runc、启动进程、通过环境变量把 TRACEPARENT 传给容器内进程等），以便实现端到端分布式链路。
- **推荐工具**：使用 OpenTelemetry（otel-go）生态：OTLP/Jaeger/Zipkin exporter，使用 W3C Propagator，采样、资源属性、日志关联都一起规划。
- **关键实现点**：
  - Tracer provider 初始化
  - ttrpc 客户端/服务器端拦截器（inject/extract）
  - 对关键操作创建 span
  - 把 trace id 写入日志/metrics 标签
  - 把 traceparent 传给容器内的进程（如果需要）

下面是详细说明、实现步骤与示意图。

一、基础概念（为后续实现统一术语）
- Trace / Span：trace 是一次分布式调用链，span 是 trace 中的一个节点（有开始/结束时间、attributes/metadata）。
- Context propagation：把当前 span 的上下文在 RPC 调用之间传递，常用格式是 W3C Trace Context（traceparent + tracestate）。OpenTelemetry 提供 TextMapPropagator 来统一 inject/extract。
- Instrumentation：在关键 API/函数处创建 span（例如：Create/Start/Exec/Wait/Process IO）。
- Exporter：把本地的 spans 发送到 collector/backends（OTLP endpoint, Jaeger, Zipkin 等）。
- Correlation：把 trace id/parent id 注入日志与 metrics 中，便于调试。

二、containerd + MicRun 的典型 tracing 点（与 shim 相关）
- 客户端（比如 CRI 或 higher-level caller）发起请求 -> containerd：containerd shimService 的多数服务函数通常会有个 span（例如 "tasks/Start"）。
- containerd 调用 shim（ttrpc）：containerd 在发 ttrpc 请求时，需要把当前 ctx 的 trace context 注入 ttrpc 的 metadata（即 inject）。
- shim（ttrpc server）接收请求：在 ttrpc 服务端拦截器中 extract 出 tracecontext，并用它作为 parent 来开始一个新的 span（例如 "shimv2.Start"）。
- 较难：MicRun 可能与micad 联合，在RTOS内维护某个agent/monitor, 在内部追踪
- exporter：每个进程把 spans 发到同一个 collector 或直接到后端，形成完整链路。

三、实现步骤（详细、可操作）

1) 选择与初始化 tracing 库（在 shim 和 containerd 两端都要）
- 使用 OpenTelemetry-Go（go.opentelemetry.io/otel），在 shim 进程启动时初始化 TracerProvider。
- 选择 exporter：建议 OTLP/gRPC -> 部署一个 otel-collector（集中接收并转发到 Jaeger/Zipkin/Tempo 等）。
- 设置 Resource attributes（service.name=你的 shim 名称、containerd.namespace、container.id、shim.pid、host.name 等）。
- 设置采样策略（例如 parentbased + 1.0 或 0.1，开发时可全部采样，生产可调低）。

示例环境变量（常见）：
- OTEL_EXPORTER_OTLP_ENDPOINT
- OTEL_TRACES_SAMPLER=parentbased_traceidratio
- OTEL_TRACES_SAMPLER_ARG=0.1

2) 在 containerd 调用 shim 的地方注入 trace 上下文（client-side interceptor）
- 在 containerd 的 ttrpc client 代码（发起到 shim 的 RPC）添加一个 client 拦截器（或在创建 ttrpc client 时，wrap call），它的工作是：
  - 使用 otel.GetTextMapPropagator().Inject(ctx, carrier) 将 trace context 写入 carrier（carrier 是 map[string]string 或某个可写入 metadata 的抽象）。
  - 把 carrier 的 KV 放入 ttrpc 的 metadata（调用方 context），这样 metadata 会随 ttrpc 请求发送到 shim。

3) shim 的 ttrpc server 端实现拦截器（server-side interceptor）
- 在 ttrpc server 的 handler 入口加一个拦截器：
  - 从 ttrpc 请求上下文中读取 metadata，构建一个 carrier（从 metadata 中装载 KV）。
  - 使用 otel.GetTextMapPropagator().Extract(ctx, carrier) 得到 parentCtx。
  - 用 tracer.Start(parentCtx, spanName, ...) 创建一个 span（child）。
  - 在 handler 执行过程中把新 ctx 传给后续代码，这样你在后续代码里直接使用 ctx 启动子 span 即可。

4) 在 shim 的关键操作处创建 spans 与 attributes
- 在 shim 的 Start、Create、Exec、Kill、Wait、IO copy（stdin/stdout/stderr）、checkpoint/restore 等关键路径创建 spans，并把上下文继续传递。
- 为每个 span 添加丰富 attribute（container.id, task.id, pid, exit.status, image.name, snapshotter=... 等），便于观察。

5) 为容器内进程保留/传递 trace context（可选，但推荐）
- 当 shim 启动容器进程或 Exec 进程时，可以将当前 trace context 注入环境变量 TRACEPARENT（W3C traceparent）或者自定义 header（但 W3C 推荐）。
- 这样容器内的应用（或 sidecar/instrumentation）能自动 pick up 并继续 trace。

6) 日志 & Metrics 关联
- 在日志框架（logrus/zerolog 等）里把 trace_id/span_id 注入每条日志（通常通过 log hook 或用 otel/semconv 的 correlation API）。
- 在 metrics 上用 trace id 做 label（通常不大量做 trace->metrics，避免标签爆炸），但在调试性指标上可以使用。

7) 验证与调试
- 在开发/测试时，把 OTEL_SAMPLER=always_on，exporter 指向本地 otel-collector + jaeger UI，调用 Create/Start/Exec，检查 jaeger 中完整链路。
- 使用 otel-cli 或直接导出 traces。
- 检查 ttrpc metadata（抓包或打印）确认 traceparent 已被注入。

四、常见实现细节与陷阱（避免踩坑）
- ttrpc metadata API：ttrpc 的 metadata 存放通常不是 HTTP headers，但也有类似的 metadata 机制；在 inject/extract 时需要使用 ttrpc 提供的 metadata API（不同于 gRPC 的 metadata.MD）。实现时要看你用的 ttrpc 版本与 containerd 的 ttrpc wrapper。
- 如果 containerd 已经在它的端做了 tracing 注入（containerd 主进程可能已做），你只需在 shim server 端提取并使用；若没有，你需要在 containerd 的调用处增加 client-side 注入（这需要修改 containerd 本身或作为插件）。
- 传递给容器的 env 只能由 shim 设定一次（Create 时），Exec 时要特别处理。
- 使用 W3C TraceContext（traceparent）是互操作性最好的方式。
- 注意不要在高 QPS 路径贴太多阻塞型 exporter（使用异步 batcher）。

五、

说明：
- containerd 在发 ttrpc 调用时把 traceparent 写到 ttrpc metadata。
- shim 的 ttrpc server interceptor 读出 metadata，使用 otel propagator 从中恢复 parent ctx 并新建子 span。
- shim 启动 container 进程时写入 TRACEPARENT 环境变量（或其它方式），容器内的 app 若有 instrument，继续 trace。

六、示例代码（可直接放进 shim 或 containerd 的代码中改造）

下面给出若干文件：tracer 初始化、ttrpc 拦截器 template（client-side 和 server-side），以及如何在 handler 中创建 span。注意：ttrpc metadata 的读写函数名为 getTTRPCMetadata / withTTRPCMetadata，这里给出抽象实现点（你需要根据你使用的 ttrpc 版本/库实现具体的 metadata 操作）；示例侧重 otel 注入/提取逻辑与 span 使用方式。

```go name=shim/tracing.go
package shim

import (
	"context"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"google.golang.org/grpc"
)

// SetupTracer sets up an OTLP exporter and TracerProvider.
// Call this once at shim start.
func SetupTracer(ctx context.Context, serviceName string, otlpEndpoint string) (*sdktrace.TracerProvider, error) {
	opts := []otlptracegrpc.Option{}
	if otlpEndpoint != "" {
		opts = append(opts, otlptracegrpc.WithEndpoint(otlpEndpoint))
		// If using insecure http, you may need WithInsecure()
		opts = append(opts, otlptracegrpc.WithDialOption(grpc.WithBlock()))
	}

	exp, err := otlptracegrpc.New(ctx, opts...)
	if err != nil {
		return nil, err
	}

	res, _ := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
			attribute.String("process.pid", os.GetpidString()),
		),
	)

	bsp := sdktrace.NewBatchSpanProcessor(exp)
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(bsp),
		sdktrace.WithResource(res),
		// Optionally add sampler
	)

	otel.SetTracerProvider(tp)
	return tp, nil
}
```

注意：os.GetpidString() 是示例。你可以用 fmt.Sprintf("%d", os.Getpid()) 替代。

```go name=shim/ttrpc_interceptor.go
package shim

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"github.com/containerd/ttrpc" // 如果你的项目使用相同包
)

// carrier 实现 TextMapCarrier，用来从 ttrpc metadata 抽取/注入。
// 这里 carrier 使用 map[string]string，但实际与 ttrpc metadata 的映射需要你用具体 API 实现。
type mapCarrier map[string]string

func (c mapCarrier) Get(key string) string {
	return c[key]
}
func (c mapCarrier) Set(key, value string) {
	c[key] = value
}
func (c mapCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}

// ServerInterceptor 为 ttrpc server 的 unary interceptor 示例。
// 在实际 ttrpc 的 server 创建处注册此拦截器（或手动在每个 handler 的最开头做相同逻辑）。
func ServerInterceptor() ttrpc.UnaryServerInterceptor {
	propagator := otel.GetTextMapPropagator()
	tracer := otel.Tracer("shimv2")

	return func(ctx context.Context, method string, req, resp interface{}, opts *ttrpc.UnaryServerInfo, handler ttrpc.UnaryHandler) (interface{}, error) {
		// 从 ctx 的 metadata 中读取 KV，构造 carrier
		// TODO: 下面两个 helper 需要根据你用的 ttrpc 元数据 API 实现：
		// md := getTTRPCMetadata(ctx)  // map[string]string
		// carrier := mapCarrier(md)

		md := getTTRPCMetadata(ctx) // 你需要实现这个函数
		carrier := mapCarrier(md)

		// Extract parent context
		parentCtx := propagator.Extract(ctx, carrier)
		ctx, span := tracer.Start(parentCtx, fmt.Sprintf("shim:%s", method), trace.WithSpanKind(trace.SpanKindServer))
		defer span.End()

		// attach attributes
		span.SetAttributes()
		// Execute handler with new ctx
		return handler(ctx, req)
	}
}
```

`getTTRPCMetadata`，它的职责是从 ttrpc 的上下文中取出 metadata（key/value），返回 map[string]string。实现取决于你使用的 ttrpc 版本（通常 containerd 的 ttrpc 在 context 中有 metadata，或者有 helper 函数）。

下面是 client-side 的示例（在 containerd 或在任何 ttrpc client 调用 shim 之前使用）：

```go name=containerd/ttrpc_client_inject.go
package containerd

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"github.com/containerd/ttrpc"
)

// ClientInterceptor demonstrates injecting current trace context into ttrpc metadata before call.
func ClientInterceptor() ttrpc.UnaryClientInterceptor {
	propagator := otel.GetTextMapPropagator()

	return func(ctx context.Context, method string, req, resp interface{}, opts *ttrpc.UnaryClientInfo, handler ttrpc.UnaryClientHandler) error {
		// carrier which will be filled by propagator.Inject
		carrier := make(map[string]string)
		propagator.Inject(ctx, mapCarrier(carrier))

		// attach carrier to ctx metadata so ttrpc sends as headers
		// TODO: you must implement withTTRPCMetadata to write KV into the outgoing context usable by ttrpc
		ctx = withTTRPCMetadata(ctx, carrier)

		// proceed with call
		return handler(ctx, method, req, resp, opts)
	}
}
```

withTTRPCMetadata 需要根据 ttrpc 的客户端 metadata API 实现（通常是把 metadata 写进 context，让 ttrpc 在发送请求时包含这些 kv）。

示例：在 shim handler 中启动一个子操作的 span：

```go name=shim/handler_example.go
package shim

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

func (s *Service) Start(ctx context.Context, req *StartTaskRequest) (*StartTaskResponse, error) {
	tr := otel.Tracer("shimv2")
	ctx, span := tr.Start(ctx, "StartTask")
	defer span.End()

	span.SetAttributes(
		attribute.String("container.id", req.ContainerID),
		attribute.String("image", req.Image),
	)

	// do actual start: call runc or create process
	// Before exec, optionally inject ctx into environment for child:
	if tp := getTraceParentFromContext(ctx); tp != "" {
		// set env TRACEPARENT=tp for the created process
	}

	// ... rest of start
	return &StartTaskResponse{}, nil
}
```

getTraceParentFromContext 可以使用 otel 的 propagator 将 ctx 注入到一个临时 carrier 然后取出 traceparent 值。

七、把 trace context 写入容器进程（示例）
- 方法一（推荐）：在 Create/Exec 的环境变量中加入 TRACEPARENT（使用 otel GetTextMapPropagator().Inject 到一个 carrier，然后把 carrier["traceparent"] 写到 env）。
- 方法二：通过文件或 CLI arg 传递，但 env 最直接。

八、示例：如何从 ctx 得到 traceparent string 并放 env
（伪代码，基于上面 mapCarrier）

```go
func TraceparentFromContext(ctx context.Context) string {
	carrier := make(map[string]string)
	otel.GetTextMapPropagator().Inject(ctx, mapCarrier(carrier))
	return carrier["traceparent"] // W3C key
}
```

九、校验步骤（快速验证链路）
1. 在 containerd 端触发一个请求（Start），确保 containerd 的 span 有 trace id。
2. 在 shim 的拦截器处打印/记录接收到的 traceparent（或在 jaeger UI 中看是否有 child spans）。
3. 在 shim 内创建一个 span，用描述性的 name（shimv2.Start:containerID）。
4. 在创建容器进程时设置 TRACEPARENT，并在容器内运行一个简单的 instrumented app（或使用 otel-cli）来验证 trace 被继续。
5. 在 backend（jaeger/tempo）里检索 trace id，验证完整链路。

---

### IO

我们考虑几种用法

```shell
# -i 保持 stdin
# -t 打开 terminal mode
# hello-world 镜像执行输出后退出
# alpine 镜像进入 alpine linux shell 交互
```

1. nerdctl run -d hello-world; then attach it
2. nerdctl run -it hello-world
3. nerdctl run -t hello-world
4. nerdctl run hello-world
5. nerdctl run -t alpine,
6. nerdctl run -it alpine
7. nerdctl run -i alpine

- `nerdctl run -d; attach`

启动时用 `-d`（detach，默认没有 -i -t），所以容器：

1. stdin: 关闭（或为 EOF）
2. stdout: pipe（或由 shim/containerd 捕获）
3. stderr: pipe（独立）

数据流（简化）：
```
容器进程 -> stdout/stderr pipe -> containerd/shim -> （可能被 buffer/写入日志） -> attach 客户端 从 shim 拉取并写到客户端 stdout
# attach 只是在事后打开并订阅这些 stdout/stderr 流，若容器启动时没有打开 stdin（非 -i），attach 通常不能向容器发送 stdin。
```
特殊控制符：不会由内核转换成信号（只是原始字节），因为没有 pty；所以 Ctrl-C 等不会变成 SIGINT（除非外部有额外的信号转发逻辑）。
IO 类型：
pipeIO / binaryIO（stdout/stderr 是字节流）；如果 containerd configured 将日志写文件，则也可能涉及 fileIO（写到日志文件）。
stderr/stdout：分开，不合并。

- `nerdctl run -it hello-world`

使用 -i（keep stdin open）和 -t（allocate tty）。注意 hello-world 进程很快退出，交互性有限，但配置如下：
* 容器端：有 pty slave 作为控制终端；stdin/stdout/stderr 都绑到该 tty（通常 stderr 与 stdout 合并为同一 tty）。
* 数据流：
* 本地终端（用户键盘）被客户端切到 raw 模式 -> 客户端通过 pty master 交换字节 -> shim -> pty slave (/dev/ttyRPMSG*), ttyRPMSG 是一 /dev/pts/* 的符号链接
反向：RTOS 写入 pty slave(/dev/ttyRPMSG*) -> shim/pty master -> 客户端显示

**特殊控制符**：TTY 情况下可以处理特殊控制符。我们需要**在客户端侧实现 INTR**， 当 INTR 被捕获时，容器会被手动kill，**而不能期望RTOS内都能处理 POSIX INTR信号, 并且即便因此中断了，mica也不知道RTOS退出了**
> 客户端通常把本地终端设为 raw，使所有按键字节传给容器的 tty；在 Linux 容器实践中， 内核/tty 驱动会把像 Ctrl-C (INTR， 未必是Ctrl-C，具体要看host termios怎么绑定的) 转成 SIGINT 送给容器进程的前台进程组（前提是容器进程的 termios 没屏蔽）。
> 然而在RTOS中不具备这样的能力，因此我们需要在客户端侧将本地终端设置为 canoinical
IO 类型：pty/character（不是 pipe/file），属于终端字符 IO（可视为非-binary，因为有行规/信号）。
stderr/stdout：合并到同一个 tty。
nerdctl run -t hello-world
仅 -t：分配 tty，但没有 -i，所以 stdin 不是保持打开的交互流（通常是关闭/EOF），但 stdout/stderr 仍通过 pty 输出。
数据流：
容器端有 pty slave（进程把输出写到 tty）；客户端 attach（若 attach 时连接到该 tty）会收到 tty 输出。
因为没有 -i，客户端不能把 stdin 发送到容器（stdin 源头缺失），即使键盘有输入也不会被送入容器。
特殊控制符：
输出端是 tty（有终端语义），但因为 stdin 未绑定，用户无法交互地发送控制字符。若 attach 后手工打开输入流并对 pty 写入字节（但通常 attach 不允许打开 stdin），才可能发送控制字节。
IO 类型：pty（character）；stderr/stdout 合并。
nerdctl run hello-world
无 -i -t（非交互、非 tty），常见用于短命程序：
stdin: closed
stdout: pipe
stderr: pipe
数据流：
容器 -> stdout/stderr pipes -> shim -> client（或被日志后端收集）
特殊控制符：无，都是字节流；Ctrl-C 在本地不会传给容器进程。
IO 类型：pipeIO / binaryIO。stderr/stdout 分开。
nerdctl run -t alpine
-t 但无 -i：如场景 3，但以 alpine 为例，结果依命令而定：
若默认 CMD/ENTRYPOINT 是交互 shell，shell 发现没有 stdin（EOF）可能会直接退出；若是 sleep 或其它长期进程，pty 仍存在供输出使用。
数据流：
输出通过 pty；输入不可用（除非 attach 强行打开 stdin）。
特殊控制符：同场景 3，tty 存在但输入未连接。
IO 类型：pty/character。stderr/stdout 合并。
nerdctl run -it alpine
完全交互（常用场景）：
pty master/slave 建立，stdin 保持打开
数据流：
本地终端（客户端）切 raw -> 按键字节通过 pty master -> containerd/shim -> pty slave -> 容器进程（比如 /bin/sh）
容器输出反向流回显示
特殊控制符：能完整处理（Ctrl-C、Ctrl-Z、行编辑、信号传递、窗口大小、终端属性均可工作）。容器内 shell 会把自己认为是前台的进程组接收由 tty 产生的信号。
IO 类型：pty/character。
nerdctl run -i alpine
仅 -i（stdin 保持打开，但不分配 tty）：
stdin: open pipe 到容器进程
stdout/stderr: pipe
数据流：
本地键盘（注意：客户端不会把本地终端切 raw，因为没有 tty） -> 客户端把键入数据作为字节写到 stdin pipe -> container process 读 stdin
容器输出经 stdout/stderr pipes 发回客户端
特殊控制符：
按键如 Ctrl-C 会作为字节 0x03 写到 stdin，但内核不会把它转成 SIGINT（因为没有 tty 的 line discipline）。因此容器进程不会因为 Ctrl-C 自动收到 SIGINT，除非进程显式检测该字节并自己处理。
许多 shell 在 detect 到 stdin 非 tty 时会进入非交互模式（没有作业控制），因此 -i 单独使用通常无法得到真正的交互 shell 体验。
IO 类型：pipeIO / binaryIO（stdin/stdout/stderr 都是字节流），stderr/stdout 分开。

相关的io模型

* binaryIO：可以理解为“字节流”，通常对应 pipe/FIFO/file 后端（没有终端行规处理），传输的是原始字节。
* pipeIO：严格对应 runtime 使用的 pipe 或 FIFO（stdin/stdout/stderr 独立流）。
* fileIO：对应后端是文件（或日志文件），或用户把 IO 重定向到文件。
* ttyIO: 

```
IO (interface)
├── pipeIO (fifo://) - Named pipes
├── fileIO (file://) - Direct file logging  
├── binaryIO (binary://) - Containerd binary logger
└── ttyIO (wrapper) - TTY mode handling
```

### CRI 和运行时

从云原生生态架构，虽然，没有必要关心 Kubernetes 如何与底层容器运行时交互，因为有一个容器端点（像 containerd）服务作为代理，接收来自 Kubernetes 的 CRI，将它们转换为运行时任务 API，我们必须知道 CRI 做什么

#### CRI 列表

#### 事件

#### CNI 和 Netns

- **可否不再使用一个阻塞进程维护 netns 呢？**
  - 完全可以！并且，这非常好

---

# MicRun 整合 micad 设计

## MicRun 模型

- **配置方式**：micrun 是 shim+runtime，其模型是和容器 1:1 的，这意味着对 micrun 的配置都是对单个容器的设置。
- **全局配置**：如果需要全局的通用改变，应该对 daemon (micad) 做配置，并且让 micrun 可以感知到。否则对于比如 `shared_cpu_pool` 这样的全局设定，micrun 难以处理。

## 实现侧挑战

- **Workaround**：MicRun 都直接与 xen 打交道，在分层架构中这很糟糕。
- **整合方案**：如何和 micad 整合到一起，在结构上就不会有伸手过长的问题。

## 未来模型

- **当前模型**：MicRun(shim + containerd runtime) + Micad
- **目标模型**：MicaShim + OCIMicad(containerd runtime + micad)

# Containerd 操作

## 接口概述

“shimv2 runtime” 在 containerd 中通常暴露两类 gRPC/ttrpc 接口：

- **Sandbox-level ttrpc API**（sandbox service）：CreateSandbox / StartSandbox / StopSandbox / SandboxStatus / WaitSandbox / Platform / PingSandbox / ShutdownSandbox / SandboxMetrics 等（见 bridge.go 的包装），用于沙箱生命周期管理（pod-sandbox 级别）。
- **Task/container-level API**（task service / container service）：Create (task), Start, Kill, Delete, Wait, State, Pids, Stats, Exec 等，用于单个 container/task 的创建/运行/停止/查询。

Containerd 在 CRI 层（RunPodSandbox 等）会经由内部的 sandbox controller / sandboxService 把请求转成对 shim 的上述 RPC 调用；同时 containerd 也会通过自身的 task service 调用触发 shim 对具体 OCI runtime 的 runc/runhcs 操作（即 shim 会接收 task API 调用）。

## RunPodSandbox()（创建并启动 pod sandbox）

Containerd 对 shimv2 发出的 RPC（按常见实现路径、时序）：

### 主要 RPC（ttrpc / sandbox API）
### CreateSandbox(CreateSandboxRequest)

- **目的**：让 shim/Controller 初始化 sandbox 相关环境（可用于 mount、prepare rootfs、创建/使用 network namespace 等）。
- **传入信息**：PodSandboxConfig（metadata、labels、annotations、namespace、runtime options）、可能的 “NetNSPath”（如果 containerd 先创建或传入 netns）、Options（runtime-specific options）。（core/sandbox/controller.go 中有 WithNetNSPath、WithOptions 等 create 选项。）
- **期望响应**：成功/错误。Create 阶段通常不会把 sandbox 标记为 ready，但会准备环境并可能返回创建中元信息（取决实现）。

### StartSandbox(StartSandboxRequest) -> StartSandboxResponse

- **目的**：让 shim 启动 sandbox “进程/任务”（即 sandbox container / pause container），并返回 runtime-side 的运行信息。containerd 将把 Start 返回的控制信息记录到 sandbox store（见 sandbox_run.go：若 ctrl.Address 非空则保存为 sandbox.Endpoint）。
- **重要响应字段**（ControllerInstance / StartSandboxResponse）：
  - Address（shim 对外的 address / endpoint，用于后续与该 shim 对话）
  - Pid（sandbox process pid）
  - Version（shim/protocol版本）
  - Labels（可能含 selinux_label 等扩展字段）
- **Containerd 期待**：Start 返回时 sandbox 已经“可用/可被管理”（若需要等待进入 ready 还会有 Ping 或 Status 调用）。

### Platform(PlatformRequest) / SandboxPlatform

- **目的**：获取 sandbox 报告的 platform（OS/Arch）；containerd 会据此决定 metrics/转换/处理方式。
- **期望响应**：PlatformResponse（包含 OS/Arch）。

### PingSandbox / SandboxStatus（可选/校验）

- **目的**：校验 sandbox 是否“就绪”、获取更多状态（包括可能的网络状态/annotations/extra info）。containerd 在恢复/查询链路可能会调用 SandboxStatus。
- **期望响应**：Ping 返回成功或 SandboxStatus 返回包含状态、启动时间、可能的 network info（具体字段取决 shim 的实现/协议版本）。

### 伴随 sandbox 的“容器化”启动流程

Containerd 会在创建 sandbox container（pause）时通过 container/task API 向对应 shim 发起 task 相关 RPC：

- Task.Create（创建 task）
- Task.Start（启动 task）

之后 containerd 可能会调用 Task.State / Task.Pids / Task.Stats 等以同步状态。

### 时序要点（summary）

Containerd 整体顺序典型为：可能先准备网络（视 CNI 模式） -> CreateSandbox -> StartSandbox -> containerd 在本地创建 containerd container/task（这会转成对 shim 的 Task.Create/Task.Start） -> （后续）调用 SandboxStatus / WaitSandbox 来观察 sandbox。

## StopContainer()（停止单个 container）

Containerd 对 shimv2 发出的 RPC 在 CRI 的 StopContainer，containerd 的实现会尽量可靠地结束 container，并在必要时强制 kill。对应到 shim/task API，典型调用序列为：

### Task.Kill（第一次，signal = SIGTERM 或等同于请求的停止信号）

- **目的**：发送优雅停止信号给容器进程，触发容器内部停机行为。containerd 的 StopContainerRequest 会带超时时间（grace period），containerd 会传递合适信号/参数到 Kill RPC。
- **期望响应**：成功或错误。若连接在中途关闭，containerd 会有重试逻辑（stopContainerRetryOnConnectionClosed 之类的重试）。

### 等待容器退出（Task.Wait / 监听 event）

- **行为**：containerd 会等待 TaskExit 事件或显式调用 Task.Wait（等待任务退出），以确保容器确实终止。CRI 实现里有等待逻辑并会根据 timeout 强制下一步。
- **超时处理**：如果超时未退出 -> 再次 Task.Kill（signal = SIGKILL）以强制终止。

### Task.Delete（删除 task）

- **目的**：清理 runtime 状态（删除 runtime 管理的 task），通常 Delete 会被调用以移除容器在 runtime 的记录并释放资源。注：某些实现里 containerd 在某些路径不会显式立刻 Delete，而是依赖事件监控在 TaskExit 后做清理（sandbox_stop.go 中有“task.Delete is not called here because it will be called when event monitor handles TaskExit” 的注释，说明细节会因流程而异）。

### 其他/查询 RPC

- **Task.State、Task.Pids、Task.Stats**（用于日志/metrics/状态采集）在停止过程中也可能被调用以判断当前状态。

### 重试/连接关闭处理

- **行为**：如果 shim 的 ttrpc 连接被关闭，containerd 有专门的重试/退避（例如 stopContainerContainerRetryOnConnectionClosed 在 StopContainer 场景）——StopContainer 的停止路径也会在遇到 ttrpc 连接断开做 retry/backoff。

## StopPodSandbox()（停止整个 pod sandbox）

Containerd 对 shimv2 发出的 RPC（总体）：

### 高层流程

StopPodSandbox 会：
- 枚举并强制停止 sandbox 下的所有 containers（对每个 container 使用 StopContainer 路径 -> 导致上面列出的 task.Kill/Wait/Delete 等一系列 RPC）。
- 如果 sandbox 本身处于 Ready/Unknown 状态，调用 sandbox controller 的 Stop（对应 shimv2 的 StopSandbox RPC）来停止 sandbox container（pause）及清理 sandbox 层面资源。
- Teardown pod network（containerd 自己的 CNI teardown，详见下文）。
- 触发 NRI（如果启用）的 StopPodSandbox 通知等。

### 具体 shim RPC

#### StopSandbox(StopSandboxRequest) -> StopSandboxResponse

- **目的**：通知 shim 停止 sandbox（sending request to sandbox service）。shim 应当终止 sandbox 里运行的 pause/sandbox task（或至少将其置为停止），并清理任何 shim 托管的资源。
- **期望响应**：成功/错误。containerd 对 StopSandbox 的调用会在遇到 ttrpc 连接关闭时做有限重试（stopSandboxContainerRetryOnConnectionClosed），并且采用退避策略（见实现中的 100ms 毫秒退避例子）。

#### 伴随的 task-level RPC

当 StopSandbox 导致 sandbox container 停止时，containerd 仍然会收到 TaskExit 事件，并可能调用 Task.Delete、查看 Task.State 等以完成清理。

#### ShutdownSandbox

（在 RemovePodSandbox 或清理时可能被调用）：要求 shim 完全删除/销毁 sandbox（停止所有子任务并释放资源）。

## 网络（net）相关

Containerd 对 runtime（shim）在网络信息与 setup/teardown 上的期待（详细）：

### NetNSPath

- Containerd 可能会在外部（由 CNI 或其它机制）创建或管理一个网络命名空间，并将该 netns 的路径（例如 /var/run/netns/<id> 或 /proc/<pid>/ns/net 的路径）传给 sandbox controller（通过 WithNetNSPath），或者 shim / sandbox controller 本身也可能创建/返回一个 netns path 给 containerd（例如在 StartSandbox 或 SandboxStatus 的响应中包含 netns 路径/标识）。
- Containerd 期待的是最终能访问到“sandbox 的 network namespace path”，以便在 teardown 时检查 namespace 是否已关闭或确保正确 cleanup（see StopPodSandbox teardown checks: 在 RemovePodSandbox 会检查 sandbox.NetNS.Closed()，要求 netns 已经 closed）。

### CNIResult / network setup result 的传递与保存

- Containerd 在 sandbox 的生命周期内会维护 sandbox.CNIResult（见 sandbox_stop.go 中：if sandbox.CNIResult != nil { c.teardownPodNetwork(...) }）。
- 如果 containerd 自己负责调用 CNI plugin 来 setup pod network（常见模式），则 containerd 会把 CNI 的返回结果（包括分配到的 IP、接口名、gateway、routes、DNS 等）保存在 sandbox.CNIResult 里；随后 StopPodSandbox/RemovePodSandbox 会根据该结果执行 teardown（调用 teardownPodNetwork）。
- 如果 runtime（shim）选择自己做网络配置，那么它必须把等效信息告诉 containerd（通过 sandbox store extensions / SandboxStatus / annotations / StartSandboxResponse 的 labels/extension 等机制），以便 containerd 在 teardown 时能够获得必要的上下文并调用 teardown（或由 shim 自己在 StopSandbox 时完成 teardown，但 containerd 仍然会基于 sandbox.CNIResult 做额外检查 / teardown）。
- Containerd 期待 CNIResult 的结构（至少包含 IP / interface name / namespace/path），并且在 teardown 时能够据此撤销 CNI 配置。

### 时序与责任边界（谁做 setup / 谁做 teardown）

两种常见模式：

- **A) containerd-managed CNI**（containerd 负责调用 CNI 在 sandbox 启动前后做 setup）：
  - Containerd 在 StartSandbox/CreateSandbox 的流程中调用 c.setupPodNetwork（或在 StartSandbox 之后、在创建 pause container 之前），保存 CNIResult 到 sandbox.CNIResult，并把 NetNSPath（或 netns fd）传给 shim（CreateSandbox/StartSandbox 可能会收到 NetNSPath）。
  - 随后 Stop/Remove 会调用 teardownPodNetwork 使用 sandbox.CNIResult。

- **B) runtime-managed network**（shim 自己做网络）：
  - Shim 在 CreateSandbox/StartSandbox 内部执行网络 namespace 的创建和接口绑定，并在 SandboxStatus / StartSandboxResponse / extensions 中报告网络信息（IP、NetNSPath 等）。
  - Containerd 会读取这些信息并保存；但 containerd 仍然在 Remove/Stop 时检查 sandbox.NetNS 是否已 closed（未关闭则报错），并期望 shim 在 StopSandbox/ShutdownSandbox 时释放 net 资源。

Containerd 的代码显示：StopPodSandbox 会检查 sandbox.NetNS 是否 closed（若未 closed，会在 RemovePodSandbox 时返回错误），并在 sandbox.CNIResult != nil 时调用 teardownPodNetwork。因此 containerd 明确期待要么：
- Containerd self has CNIResult and will teardown it, 或
- Shim already did network teardown and made netns closed; containerd 会检查 closed 状态并 succeed。

### 必须保证的字段与语义（containerd 侧期待）

- **NetNSPath**：能够被访问（或空字符串表示 netns 不可用/已关闭），并且在 Remove 时应处于 closed（或 shim/host 已清理）。
- **StopPodSandbox 中的检查逻辑**体现了这点：
  - 在 StopPodSandbox：若 sandbox.NetNS != nil，则先判断 sandbox.NetNS.Closed()；若 closed 则将 sandbox.NetNSPath = ""。
  - 在 RemovePodSandbox 前会再次检查 netns closed；如果未 closed，会返回错误 “sandbox network namespace is not fully closed”。
- **CNIResult**：若不为空（表示 containerd 进行了 CNI setup），containerd 会调用 teardownPodNetwork(ctx, sandbox)；因此 containerd 期待 CNIResult 含有 teardown 所需的全部信息（例如 ifName、CNI result object），并且 teardown 操作要成功。
- **Timing**：containerd 在 StopPodSandbox 的顺序上先停止 containers、再 stop sandbox、再 teardown network（见 stopPodSandbox 的实现顺序）。因此 shim 若在 StopSandbox 时执行网络 teardown，应在 StopSandbox 返回前完成，这样 containerd 的后续检查（NetNS.Closed）才会通过。

### 错误 / 重试语义（network 相关）

- 如果 teardownPodNetwork 失败，StopPodSandbox 会返回错误（并阻止进一步的删除）；containerd 的实现不会忽略 network teardown 的错误（即这被认为是重大失败，需要上报）。
- 对于 shim/ttrpc 连接断开的场景，containerd 在停止 sandbox 时会做有限次数的重试（stopSandboxContainerRetryOnConnectionClosed），并且采用退避策略（见实现中的 100ms 毫秒退避例子）。

## 对 shim 的实现端给出的明确兼容建议（契约式）

为了与 containerd 的 Run/Stop 流程无缝配合，shim（或 sandbox controller）应当满足下列契约：

- 支持并实现 sandbox ttrpc API（至少 CreateSandbox、StartSandbox、StopSandbox、SandboxStatus/Platform、WaitSandbox、PingSandbox、ShutdownSandbox）。StartSandboxResponse 必须返回 Address / Pid （以便 containerd 保存为 sandbox.Endpoint 并在后续与 shim 通信）。
- 在 CreateSandbox/StartSandbox 中接受 containerd 传入的 NetNSPath（若 containerd 提供），并在该 namespace 上正确进行 sandbox/pause container 的配置；或者如果 shim 自己创建 netns，应在 StartSandboxResponse / SandboxStatus 中报告 netns path/标识和网络配置（包含 IP/ifname 等），以便 containerd 记录（或 containerd 能够调用 teardown）。
- 在 StopSandbox/ShutdownSandbox 返回前，完成 sandbox-level 的清理（包括确保 netns 已释放或明确标记为 closed，或者将 CNI teardown 的 trigger/信息交回 containerd）。否则 containerd 在 Remove 时可能因为 netns 未 closed 而失败。
- 在 task-level 上实现 task API（Create、Start、Kill、Delete、Wait、State、Pids、Stats 等），并保证对 Kill(Delete) 等 RPC 的语义与容器进程生命周期一致（支持优雅终止的 grace period，然后可被强制杀死）。
- 在 ttrpc/连接异常时，尽量提供可重连的语义或确保在 StopSandbox/StopContainer 时返回合适的错误码以触发 containerd 的重试逻辑。

## 常见陷阱 / 注意点（从 containerd 实现角度）

- Containerd 在 StopPodSandbox 的实现里，先强制停止 containers（逐个 StopContainer），然后才停止 sandbox；因此如果 shim 在 StopSandbox 内部以外停止了子 container（或 race），containerd 仍会可靠地重复尝试停止 containers 并处理可能的 races（CRI 的 StopPodSandbox 是幂等的）。
- 如果 shim 并未把网络 teardown 的信息/状态暴露给 containerd（例如没有写入 sandbox.CNIResult 或没有在 SandboxStatus/report 中返回），containerd 可能无法在 Remove 时完成 teardown，从而导致资源泄露或 Remove 失败。
- Containerd 对 ttrpc 连接断开的恢复策略是有限重试而非无限重试；shim 不应简单地在 Stop 期间断开连接而不保证资源已清理。

## 总结

- **RunPodSandbox** 会至少触发：CreateSandbox、StartSandbox、（可能的 Platform/Ping/SandboxStatus）以及随后产生的 task-level Create/Start RPC（shim 将看到 sandbox container 的 task.Create/Start）。StartSandbox 的响应（Address/Pid/Version/labels）是 containerd 记录 sandbox Endpoint/状态的关键。
- **StopContainer** 会触发 task-level 的 Kill（SIGTERM），等待（Wait / 监听 TaskExit），在超时后再 Kill(SIGKILL)，最后 Delete（清理）——相应 RPC 分别是 Task.Kill、Task.Wait/事件、Task.Delete（并伴随 State/Stats 查询）。containerd 对连接中断有 retry 逻辑。
- **StopPodSandbox** 会调用 StopSandbox（sandbox API）来停止 sandbox 本身，且在 Stop 成功后或并行会做网络 teardown（若 sandbox.CNIResult 非空）。containerd 期望 shim/控制器要么把 netns/CNIResult 等信息报告出来（以便 containerd teardown），要么自行在 StopSandbox 时完成 teardown 并使 netns closed；containerd 会检查 netns closed 状态并以此作为删除的前置条件之一。

影子进程：有一个这样的想法：micrun 对每一个容器 RTOS 起一个对应的影子进程，
但实际上这是不必的，我们直接让 micrun shim 承担“影子进程”的职责即可。

# 资源模拟

[参考这份notes](./docs/resource-design.md)

# 想法

## 公共的 mica 符号库
