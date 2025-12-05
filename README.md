# todos
## todos in days

* yocto all-in-one building
* fix io bugs
* better netns handling
* fix metrics bugs
* refactor codes -> get a shrinked minimal version
* consider a proper managment stragety for mica-image-builder (in yocto), uv and poetry may be not a good choice for this case

## todos in weeks

to consider:

* update mica-image-builder:
> bundle content: add a client.conf
> rename to micrun-image-builder
* is it **worthy?** to implementa micrun a **common runtime** which is capable of dealing with Linux OCI images?
> 1. ~~implement it manually~~
> 2. send request to other runtimes: lcr, runc, crun, gvisor, youki, etc.. when container is a standard Linux OCI image, filtered by annotations
     if annotations contain neither `defs.MicranAnnotationPrefix` nor `Infra container annotation`, transfer tasks to external runtime

* youki (0.5.7, arm64, musl), binary size 5.6MB; about `200%` speed of `runc`; repo activity: pretty active but on an obivious decline; slow to build in yocto (rustc + downloading dependencies)
* crun (1.25.1, arm64, glibc), binary size 3.1MB; about `400%` speed of `runc`; repo activity: pretty active;
* lcr (together with iSulad, not a common choice for other container engine)
* ==> **crun** is preferred: lightweight, fast and fit with openEuler Embedded building tools

3. 

# micrun container runimte


## register runtime

* by `--runtime io.containerd.<runtime name>` options,  user can specify the runtime to run a container if runtime is installed on `$PATH`. 
* we can use containerd shim runtime [without installing on PATH](https://docs.docker.com/engine/daemon/alternative-runtimes/#use-a-containerd-shim-without-installing-on-path)

### registr  

### register on containerd

generally, add a new plugin `/etc/containerd/config.toml`:

```
[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.micrun]
  runtime_type = "io.containerd.mica.v2"
  pod_annotations = ["org.openeuler.micran."]

[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.micrun]
  # remains empty
  # micrun configuration option design is unstable
  # we remove those codes in mcs repo 
```

### registr as a kubernetes runtimeclass

```yaml
version: v1
runtimeClass:
  name: micrun
  type: RuntimeClass

```

```shell
kubelet --cpu-manager-policy=static
# isolcpus, nohz_full, ... can be customized
```

is recommened


## use runtime

use nerdctl

```shell
# notice, in nerdctl '--label' option is NOT "Label" in docker, it is "Annotation"
# Hence -l option pass annotation to container oci config
nerdctl run -d --runtime io.containerd.mica.v2 -l org.openeuler.micran.auto_disconnect=true <image>  
nerdctl update --memory 1024m  <contaienr_id>
```

use ctr (containerd test tool for developer)

```shell
ctr container create --runtime io.containerd.mica.v2 -t --annotation org.openeuler.micran.auto_disconnect=true <image> <container_id>
ctr task start <container_id>
ctr task kill <container_id>
ctr task del <container_id>
```

## dev guide


## plans

* rewrite mica-image-builder, which is just a vibe coding demo artifact
> dirty logics
> not graceful and safe implementation
* yocto
* integrate into mica library deeper
> more general pedestal interfaces
> shared filesystem, from Linux to RTOS
> snapshot design, I have some experimental ideas:
* 1. mock RTOS overlayfs
* 2. maintain a layer modification records, apply a warm patch about it to RTOS


## micrun implementations

### architecture

this minimal preview version, I remain the codes struct simple and modular:

```
- container engine

++ shim
|--> shim lifecycle, New, StartShim, Cleanup, ...
|--> shim task services, container, sandbox, pod container: Start, Kill, Delete, ...
|--> shim io, binaryIO, fileIO, pipeIO
|--> shim utils
++ runtime core
++ libs
++++ libmica
++++ pedestal

- mica
```

### shim
#### shimIO

why and how we handle IO in shim package?

#### sandbox

why maintain a sandbox struct, when containerd SandboxAPI is not enabled?
> the question is the answer: maintaining an Infra (like pause container) container is a workaround
> migrate to containerd SandboxAPI is the future,
> and manager single container, pod container inside sandbox is not troublesome, so we did it.

### container IO

### mount


generally, *mounting* is setup during Task Create, type of which defined as below:

```go
// containerd 1.7.x
type Mount struct {
	// Type defines the nature of the mount.
	Type string `protobuf:"bytes,1,opt,name=type,proto3" json:"type,omitempty"`
	// Source specifies the name of the mount. Depending on mount type, this
	// may be a volume name or a host path, or even ignored.
	Source string `protobuf:"bytes,2,opt,name=source,proto3" json:"source,omitempty"`
	// Target path in container
	Target string `protobuf:"bytes,3,opt,name=target,proto3" json:"target,omitempty"`
	// Options specifies zero or more fstab style mount options.
	Options []string `protobuf:"bytes,4,rep,name=options,proto3" json:"options,omitempty"`
}
```

How containerd treat mount options:
```go
// mount/mount_linux.go

// parseMountOptions takes fstab style mount options and parses them for
// use with a standard mount() syscall
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
		// If the option does not exist in the flags table or the flag
		// is not supported on the platform,
		// then it is a data value for a specific fs type
		// flags join combinination......
	}
	return flag, data, losetup
}
```

where to find supported mount type?

> spreaded in containerd source codes! 
> e.g. `snapshots/native/native_default.go` declared that `const mountType = "bind"`

take another look:

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

How to do for RTOS: zephyr, uniproton?

An approach is to  maintain a filesystem definitions in containerd source: bad practice


### Snapshot

### Metrics

### Tracing

TODO: 简写

要点摘要（先读这段再看下方细节与代码）：
- 分布式 tracing 的核心是 span 与上下文传播（context propagation，通常使用 W3C traceparent）。
- containerd <-> shimv2 的通信使用 ttrpc（轻量 RPC），要让 tracing 连贯，需要在 containerd 调用 shim 前注入 trace headers，在 shim 的 ttrpc server 端提取并以其为 parent 启动子 span。
- 在 shim 内部你也应把 trace 上下文传给下游（调用 runc、启动进程、通过环境变量把 TRACEPARENT 传给容器内进程等），以便实现端到端分布式链路。
- 推荐使用 OpenTelemetry（otel-go）生态：OTLP/Jaeger/Zipkin exporter，使用 W3C Propagator，采样、资源属性、日志关联都一起规划。
- 关键实现点：tracer provider 初始化、ttrpc 客户端/服务器端拦截器（inject/extract）、对关键操作创建 span、把 trace id 写入日志/metrics 标签，以及把 traceparent 传给容器内的进程（如果需要）。

下面是详细说明、实现步骤与示意图。

一、基础概念（为后续实现统一术语）
- Trace / Span：trace 是一次分布式调用链，span 是 trace 中的一个节点（有开始/结束时间、attributes/metadata）。
- Context propagation：把当前 span 的上下文在 RPC 调用之间传递，常用格式是 W3C Trace Context（traceparent + tracestate）。OpenTelemetry 提供 TextMapPropagator 来统一 inject/extract。
- Instrumentation：在关键 API/函数处创建 span（例如：Create/Start/Exec/Wait/Process IO）。
- Exporter：把本地的 spans 发送到 collector/backends（OTLP endpoint, Jaeger, Zipkin 等）。
- Correlation：把 trace id/parent id 注入日志与 metrics 中，便于调试。

二、containerd + shimv2 的典型 tracing 点（与 shim 相关）
- 客户端（比如 CRI 或 higher-level caller）发起请求 -> containerd：containerd 在其请求处理逻辑里通常会有一个根 span（例如 "containerd/tasks/Start"）。
- containerd 调用 shim（ttrpc）：containerd 在发 ttrpc 请求时，需要把当前 ctx 的 trace context 注入 ttrpc 的 metadata（即 inject）。
- shim（ttrpc server）接收请求：在 ttrpc 服务端拦截器中 extract 出 tracecontext，并用它作为 parent 来开始一个新的 span（例如 "shimv2.Start"）。
- shim 可能调用 runtime 工具（runc）或直接 fork/exec 容器进程：shim 应把 trace context 以环境变量（TRACEPARENT）或 CLI header/文件传递，便于容器内的应用或侧车继续追踪。
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

你需要实现 getTTRPCMetadata，它的职责是从 ttrpc 的上下文中取出 metadata（key/value），返回 map[string]string。实现取决于你使用的 ttrpc 版本（通常 containerd 的 ttrpc 在 context 中有 metadata，或者有 helper 函数）。

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



### CRI and Runtime

From cloud native echo architecture, although, there is no need to concern how Kubernetes interactive with low-level container runtime, for there is a container endpoint(like containerd) serve as an agent, 
receiving CRI from Kubernetes, convert them into runtime tasksAPI, we have to know what does CRI do

#### CRI list

#### event 

#### CNI and Netns



---

# 整合设计

micrun 是 shim+runtime，其模型是和容器1:1的，这意味着对 micrun 的配置都是 对单个容器的设置；
如果需要全局的通用改变，应该对daemon (micad) 做配置，并且让micrun可以感知到。否则对于比如 `shared_cpu_pool` 这样的全局设定，micrun难以处理
