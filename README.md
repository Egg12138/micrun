# todos

## todos in days

* AI 整理该文档
* yocto all-in-one building
* fix io bugs
* better netns handling
* fix metrics bugs
* fix pty bugs
* remove redundant definitions
* refactor codes -> get a shrinked minimal version
* fix goroutine leaks and possible blobks 
* consider a proper managment stragety for mica-image-builder (in yocto), uv and poetry may be not a good choice for this case

## todos in weeks

to consider:

* 
* update mica-image-builder:
> bundle content: add a client.conf
> rename to micrun-image-builder
* is it **worthy?** to implementa micrun a **common runtime** which is capable of dealing with Linux OCI images?
> 1. ~~implement it manually~~
> 2. send request to other runtimes: lcr, runc, crun, gvisor, youki, etc.. when container is a standard Linux OCI image, filtered by annotations
     if annotations contain neither `defs.MicrunAnnotationPrefix` nor `Infra container annotation`, transfer tasks to external runtime

* youki (0.5.7, arm64, musl), binary size 5.6MB; about `200%` speed of `runc`; repo activity: pretty active but on an obivious decline; slow to build in yocto (rustc + downloading dependencies)
* crun (1.25.1, arm64, glibc), binary size 3.1MB; about `400%` speed of `runc`; repo activity: pretty active;
* lcr (together with iSulad, not a common choice for other container engine)
* ==> **crun** is preferred: lightweight, fast and fit with openEuler Embedded building tools




## improvements

* cycle referencing (container <-> sandbox)
> cntr.Container.sandbox, cntr.Sandbox.containers
> for example, cntr.Container.delete() calls sandbox methods, and cntr.sandbox.Delete calls container methods
> it could be good to remove c.sandbox.methods 
* 


# micrun container runimte

## 

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

可否不再使用 netns 呢？
完全可以！


---

# 整合设计

micrun 是 shim+runtime，其模型是和容器1:1的，这意味着对 micrun 的配置都是 对单个容器的设置；
如果需要全局的通用改变，应该对daemon (micad) 做配置，并且让micrun可以感知到。否则对于比如 `shared_cpu_pool` 这样的全局设定，micrun难以处理




# containerd actions

“shimv2 runtime”在 containerd 中通常暴露两类 gRPC/ttrpc 接口：
sandbox-level ttrpc API（sandbox service）：CreateSandbox / StartSandbox / StopSandbox / SandboxStatus / WaitSandbox / Platform / PingSandbox / ShutdownSandbox / SandboxMetrics 等（见 bridge.go 的包装），用于沙箱生命周期管理（pod-sandbox 级别）。
task/container-level API（task service / container service）：Create (task), Start, Kill, Delete, Wait, State, Pids, Stats, Exec 等，用于单个 container/task 的创建/运行/停止/查询。
containerd 在 CRI 层（RunPodSandbox 等）会经由内部的 sandbox controller / sandboxService 把请求转成对 shim 的上述 RPC 调用；同时 containerd 也会通过自身的 task service 调用触发 shim 对具体 OCI runtime 的 runc/runhcs 操作（即 shim 会接收 task API 调用）。
二、RunPodSandbox()（创建并启动 pod sandbox） —— containerd 对 shimv2 发出的 RPC（按常见实现路径、时序） 主要 RPC（ttrpc / sandbox API）：

CreateSandbox(CreateSandboxRequest)

目的：让 shim/Controller 初始化 sandbox 相关环境（可用于 mount、prepare rootfs、创建/使用 network namespace 等）。
传入信息（常见 / containerd 端会传的）：PodSandboxConfig（metadata、labels、annotations、namespace、runtime options）、可能的 “NetNSPath”（如果 containerd 先创建或传入 netns）、Options（runtime-specific options）。（core/sandbox/controller.go 中有 WithNetNSPath、WithOptions 等 create 选项。）
期望响应：成功/错误。Create 阶段通常不会把 sandbox 标记为 ready，但会准备环境并可能返回创建中元信息（取决实现）。
StartSandbox(StartSandboxRequest) -> StartSandboxResponse

目的：让 shim 启动 sandbox “进程/任务”（即 sandbox container / pause container），并返回 runtime-side 的运行信息。containerd 将把 Start 返回的控制信息记录到 sandbox store（见 sandbox_run.go：若 ctrl.Address 非空则保存为 sandbox.Endpoint）。
重要响应字段（ControllerInstance / StartSandboxResponse）：
Address（shim 对外的 address / endpoint，用于后续与该 shim 对话），
Pid（sandbox process pid），
Version（shim/protocol版本），
Labels（可能含 selinux_label 等扩展字段）。
containerd 期待：Start 返回时 sandbox 已经“可用/可被管理”（若需要等待进入 ready 还会有 Ping 或 Status 调用）。
Platform(PlatformRequest) / SandboxPlatform（containerd 可能调用以确定 sandbox 报告的平台）

目的：获取 sandbox 报告的 platform（OS/Arch）；containerd 会据此决定 metrics/转换/处理方式。
期望响应：PlatformResponse（包含 OS/Arch）。
PingSandbox / SandboxStatus（可选/校验）

目的：校验 sandbox 是否“就绪”、获取更多状态（包括可能的网络状态/annotations/extra info）。containerd 在恢复/查询链路可能会调用 SandboxStatus。
期望响应：Ping 返回成功或 SandboxStatus 返回包含状态、启动时间、可能的 network info（具体字段取决 shim 的实现/协议版本）。
另外（伴随 sandbox 的“容器化”启动流程）：

containerd 会在创建 sandbox container（pause）时通过 container/task API 向对应 shim 发起 task 相关 RPC（Create task、Start task）。也就是说，RunPodSandbox 导致的还包括 task-level RPC：
Task.Create（创建 task）
Task.Start（启动 task）
之后 containerd 可能会调用 Task.State / Task.Pids / Task.Stats 等以同步状态。
时序要点（summary）：

containerd 整体顺序典型为：可能先准备网络（视 CNI 模式） -> CreateSandbox -> StartSandbox -> containerd 在本地创建 containerd container/task（这会转成对 shim 的 Task.Create/Task.Start） -> （后续）调用 SandboxStatus / WaitSandbox 来观察 sandbox。

三、StopContainer()（停止单个 container） —— containerd 对 shimv2 发出的 RPC 在 CRI 的 StopContainer，containerd 的实现会尽量可靠地结束 container，并在必要时强制 kill。对应到 shim/task API，典型调用序列为：

Task.Kill（第一次，signal = SIGTERM 或等同于请求的停止信号）

目的：发送优雅停止信号给容器进程，触发容器内部停机行为。containerd 的 StopContainerRequest 会带超时时间（grace period），containerd 会传递合适信号/参数到 Kill RPC。
期望响应：成功或错误。若连接在中途关闭，containerd 会有重试逻辑（stopContainerRetryOnConnectionClosed 之类的重试）。
等待容器退出（Task.Wait / 监听 event）：

containerd 会等待 TaskExit 事件或显式调用 Task.Wait（等待任务退出），以确保容器确实终止。CRi 实现里有等待逻辑并会根据 timeout 强制下一步。
如果超时未退出 -> 再次 Task.Kill（signal = SIGKILL）以强制终止。

Task.Delete（删除 task）

目的：清理 runtime 状态（删除 runtime 管理的 task），通常 Delete 会被调用以移除容器在 runtime 的记录并释放资源。注：某些实现里 containerd 在某些路径不会显式立刻 Delete，而是依赖事件监控在 TaskExit 后做清理（sandbox_stop.go 中有“task.Delete is not called here because it will be called when event monitor handles TaskExit” 的注释，说明细节会因流程而异）。
其他/查询 RPC：Task.State、Task.Pids、Task.Stats（用于日志/metrics/状态采集）在停止过程中也可能被调用以判断当前状态。

重试/连接关闭处理：

如果 shim 的 ttrpc 连接被关闭，containerd 有专门的重试/退避（例如 stopSandboxContainerRetryOnConnectionClosed 在 StopSandbox 场景）——StopContainer 的停止路径也会在遇到 ttrpc 连接断开做 retry/backoff。
四、StopPodSandbox()（停止整个 pod sandbox） —— containerd 对 shimv2 发出的 RPC（总体） 高层流程：

StopPodSandbox 会：
枚举并强制停止 sandbox 下的所有 containers（对每个 container 使用 StopContainer 路径 -> 导致上面列出的 task.Kill/Wait/Delete 等一系列 RPC）。
如果 sandbox 本身处于 Ready/Unknown 状态，调用 sandbox controller 的 Stop（对应 shimv2 的 StopSandbox RPC）来停止 sandbox container（pause）及清理 sandbox 层面资源。
teardown pod network（containerd 自己的 CNI teardown，详见下文）；
触发 NRI（如果启用）的 StopPodSandbox 通知等。
具体 shim RPC：

StopSandbox(StopSandboxRequest) -> StopSandboxResponse

目的：通知 shim 停止 sandbox（seding request to sandbox service）。shim 应当终止 sandbox 里运行的 pause/sandbox task（或至少将其置为停止），并清理任何 shim 托管的资源。
期望响应：成功/错误。containerd 对 StopSandbox 的调用会在遇到 ttrpc 连接关闭时做有限重试（见 stopSandboxContainerRetryOnConnectionClosed 中的 retry loop）。
伴随的 task-level RPC：当 StopSandbox 导致 sandbox container 停止时，containerd 仍然会收到 TaskExit 事件，并可能调用 Task.Delete、查看 Task.State 等以完成清理。

ShutdownSandbox（在 RemovePodSandbox 或清理时可能被调用）：要求 shim 完全删除/销毁 sandbox（停止所有子任务并释放资源）。

网络（net）相关：containerd 对 runtime（shim）在网络信息与 setup/teardown 上的期待（详细） containerd 在 CRI 模块中管理 Pod 网络与网络状态的交互点较多，关键期待包括：

网络 namespace 的“定位”（NetNSPath）

containerd 的 sandbox 管理结构里保留了 sandbox.NetNS（一个 NetNS object）和 sandbox.NetNSPath 字段（core/sandbox/controller.go 中有 WithNetNSPath 用于传入）。这表示：
containerd 可能会在外部（由 CNI 或其它机制）创建或管理一个网络命名空间，并将该 netns 的路径（例如 /var/run/netns/<id> 或 /proc/<pid>/ns/net 的路径）传给 sandbox controller（通过 WithNetNSPath），或者
shim / sandbox controller 本身也可能创建/返回一个 netns path 给 containerd（例如在 StartSandbox 或 SandboxStatus 的响应中包含 netns 路径/标识）。
containerd 期待的是最终能访问到“sandbox 的 network namespace path”，以便在 teardown 时检查 namespace 是否已关闭或确保正确 cleanup（see StopPodSandbox teardown checks: 在 RemovePodSandbox 会检查 sandbox.NetNS.Closed()，要求 netns 已经 closed）。
CNIResult / network setup result 的传递与保存

containerd 在 sandbox 的生命周期内会维护 sandbox.CNIResult（见 sandbox_stop.go 中：if sandbox.CNIResult != nil { c.teardownPodNetwork(...) }）。这说明：
如果 containerd 自己负责调用 CNI plugin 来 setup pod network（常见模式），则 containerd 会把 CNI 的返回结果（包括分配到的 IP、接口名、gateway、routes、DNS 等）保存在 sandbox.CNIResult 里；随后 StopPodSandbox/RemovePodSandbox 会根据该结果执行 teardown（调用 teardownPodNetwork）。
如果 runtime（shim）选择自己做网络配置，那么它必须把等效信息告诉 containerd（通过 sandbox store extensions / SandboxStatus / annotations / StartSandboxResponse 的 labels/extension 等机制），以便 containerd 在 teardown 时能够获得必要的上下文并调用 teardown（或由 shim 自己在 StopSandbox 时完成 teardown，但 containerd 仍然会基于 sandbox.CNIResult 做额外检查 / teardown）。
containerd 期待 CNIResult 的结构（至少包含 IP / interface name / namespace/path），并且在 teardown 时能够据此撤销 CNI 配置。
时序与责任边界（谁做 setup / 谁做 teardown）

两种常见模式： 
A) containerd-managed CNI（containerd 负责调用 CNI 在 sandbox 启动前后做 setup）：
containerd 在 StartSandbox/CreateSandbox 的流程中调用 c.setupPodNetwork（或在 StartSandbox 之后、在创建 pause container 之前），保存 CNIResult 到 sandbox.CNIResult，并把 NetNSPath（或 netns fd）传给 shim（CreateSandbox/StartSandbox 可能会收到 NetNSPath）。随后 Stop/Remove 会调用 teardownPodNetwork 使用 sandbox.CNIResult。 B) runtime-managed network（shim 自己做网络）：
shim 在 CreateSandbox/StartSandbox 内部执行网络 namespace 的创建和接口绑定，并在 SandboxStatus / StartSandboxResponse / extensions 中报告网络信息（IP、NetNSPath 等）。containerd 会读取这些信息并保存；但 containerd 仍然在 Remove/Stop 时检查 sandbox.NetNS 是否已 closed（未关闭则报错），并期望 shim 在 StopSandbox/ShutdownSandbox 时释放 net 资源。
containerd 的代码显示：StopPodSandbox 会检查 sandbox.NetNS 是否 closed（若未 closed，会在 RemovePodSandbox 时返回错误），并在 sandbox.CNIResult != nil 时调用 teardownPodNetwork。因此 containerd 明确期待要么：
containerd self has CNIResult and will teardown it, 或
shim already did network teardown and made netns closed; containerd 会检查 closed 状态并 succeed。
必须保证的字段与语义（containerd 侧期待）

NetNSPath：能够被访问（或空字符串表示 netns 不可用/已关闭），并且在 Remove 时应处于 closed（或 shim/host 已清理）。
StopPodSandbox 中的检查逻辑体现了这点：
在 StopPodSandbox：若 sandbox.NetNS != nil，则先判断 sandbox.NetNS.Closed()；若 closed 则将 sandbox.NetNSPath = ""。在 RemovePodSandbox 前会再次检查 netns closed；如果未 closed，会返回错误 “sandbox network namespace is not fully closed”。
CNIResult：若不为空（表示 containerd 进行了 CNI setup），containerd 会调用 teardownPodNetwork(ctx, sandbox)；因此 containerd 期待 CNIResult 含有 teardown 所需的全部信息（例如 ifName、CNI result object），并且 teardown 操作要成功。
timing：containerd 在 StopPodSandbox 的顺序上先停止 containers、再 stop sandbox、再 teardown network（见 stopPodSandbox 的实现顺序）。因此 shim 若在 StopSandbox 时执行网络 teardown，应在 StopSandbox 返回前完成，这样 containerd 的后续检查（NetNS.Closed）才会通过。
错误 / 重试语义（network 相关）

如果 teardownPodNetwork 失败，StopPodSandbox 会返回错误（并阻止进一步的删除）；containerd 的实现不会忽略 network teardown 的错误（即这被认为是重大失败，需要上报）。
对于 shim/ttrpc 连接断开的场景，containerd 在停止 sandbox 时会做有限次数的重试（stopSandboxContainerRetryOnConnectionClosed），并且采用退避策略（见实现中的 100ii 毫秒退避例子）。
六、对 shim 的实现端给出的明确兼容建议（契约式） 为了与 containerd 的 Run/Stop 流程无缝配合，shim（或 sandbox controller）应当满足下列契约：

支持并实现 sandbox ttrpc API（至少 CreateSandbox、StartSandbox、StopSandbox、SandboxStatus/Platform、WaitSandbox、ShutdownSandbox、PingSandbox）。StartSandboxResponse 必须返回 Address / Pid （以便 containerd 保存为 sandbox.Endpoint 并在后续与 shim 通信）。
在 CreateSandbox/StartSandbox 中接受 containerd 传入的 NetNSPath（若 containerd 提供），并在该 namespace 上正确进行 sandbox/pause container 的配置；或者如果 shim 自己创建 netns，应在 StartSandboxResponse / SandboxStatus 中报告 netns path/标识和网络配置（包含 IP/ifname 等），以便 containerd 记录（或 containerd 能够调用 teardown）。
在 StopSandbox/ShutdownSandbox 返回前，完成 sandbox-level 的清理（包括确保 netns 已释放或明确标记为 closed，或者将 CNI teardown 的 trigger/信息交回 containerd）。否则 containerd 在 Remove 时可能因为 netns 未 closed 而失败。
在 task-level 上实现 task API（Create、Start、Kill、Delete、Wait、State、Pids、Stats 等），并保证对 Kill(Delete) 等 RPC 的语义与容器进程生命周期一致（支持优雅终止的 grace period，然后可被强制杀死）。
在 ttrpc/连接异常时，尽量提供可重连的语义或确保在 StopSandbox/StopContainer 时返回合适的错误码以触发 containerd 的重试逻辑。
七、常见陷阱 / 注意点（从 containerd 实现角度）

containerd 在 StopPodSandbox 的实现里，先强制停止 containers（逐个 StopContainer），然后才停止 sandbox；因此如果 shim 在 StopSandbox 内部以外停止了子 container（或 race），containerd 仍会可靠地重复尝试停止 containers 并处理可能的 races（CRI 的 StopPodSandbox 是幂等的）。
如果 shim 并未把网络 teardown 的信息/状态暴露给 containerd（例如没有写入 sandbox.CNIResult 或没有在 SandboxStatus/report 中返回），containerd 可能无法在 Remove 时完成 teardown，从而导致资源泄露或 Remove 失败。
containerd 对 ttrpc 连接断开的恢复策略是有限重试而非无限重试；shim 不应简单地在 Stop 期间断开连接而不保证资源已清理。

RunPodSandbox 会至少触发：CreateSandbox、StartSandbox、（可能的 Platform/Ping/SandboxStatus）以及随后产生的 task-level Create/Start RPC（shim 将看到 sandbox container 的 task.Create/Start）。StartSandbox 的响应（Address/Pid/Version/labels）是 containerd 记录 sandbox Endpoint/状态的关键。
StopContainer 会触发 task-level 的 Kill（SIGTERM），等待（Wait / 监听 TaskExit），在超时后再 Kill(SIGKILL)，最后 Delete（清理）——相应 RPC 分别是 Task.Kill、Task.Wait/事件、Task.Delete（并伴随 State/Stats 查询）。containerd 对连接中断有 retry 逻辑。
StopPodSandbox 会调用 StopSandbox（sandbox API）来停止 sandbox 本身，且在 Stop 成功后或并行会做网络 teardown（若 sandbox.CNIResult 非空）。containerd 期望 shim/控制器要么把 netns/CNIResult 等信息报告出来（以便 containerd teardown），要么自行在 StopSandbox 时完成 teardown 并使 netns closed；containerd 会检查 netns closed 状态并以此作为删除的前置条件之一。


影子进程：有这样一个想法：micrun对每一个容器RTOS起一个对应的影子进程，
但实际上这是不必的，我们直接让micrun shim承担“影子进程”的职责即可。
