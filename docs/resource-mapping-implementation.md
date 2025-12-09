# MicRun 资源映射实现修正总结

## 概述

本文档总结了根据资源映射规范对 MicRun 代码进行的修正。修正工作确保了 CPU 和内存资源从容器（Kubernetes/containerd）到 RTOS 客户机的正确映射。

## 1. CPU 资源映射修正

### 1.1 映射关系明确化

已明确以下映射关系：

```
Container CPU Share (cpu.shares) <==(放缩比例 1024:256)==> RTOS Client CPU Weight
Container Quota/Period (cpu.quota/cpu.period) <==(放缩比例 1:100)==> RTOS Client CPU Capacity (百分比，占满单核100%)
Container cpuset (cpu.cpus) <====> RTOS Client CPUS (CPU 亲和性)
```

### 1.2 关键代码修正

#### 1.2.1 `pkg/pedestal/planner.go` - `linuxResourceToEssential()`
- 添加了详细的资源映射注释
- 明确了 VCPU 数量策略：默认 VCPU = 1
- 实现了 cpuset 容量限制：`effective_capacity = min(quota容量, cpuset容量)`
- 正确处理了无 quota/period 但有 cpuset 的情况

#### 1.2.2 `pkg/pedestal/xen.go` - `ShareToWeight()`
- 明确了转换比例：1024 (cgroup默认) : 256 (Xen默认) = 4:1
- 添加了范围说明：
  - cgroup shares: 2-262144, default=1024
  - Xen weight: 1-65535, default=256
- 转换公式：`weight = max(1, min(shares / 4, 65535))`

#### 1.2.3 `pkg/micantainer/container_resources.go`
- 为所有资源访问方法添加了映射关系注释
- 明确了 `CPUCapacity()` 返回的是百分比（100% = 占满一个 vCPU）
- 明确了 `CPUShares()` 的转换比例
- 在 `ParseOCIResources()` 中添加了详细的调试日志

### 1.3 VCPU 数量策略实现

#### 默认策略：VCPU = 1
- 大多数情况下，RTOS 客户机只需要 1 个 vCPU
- 简化调度，减少 Hypervisor 开销

#### 可选策略：VCPU = Size(cpuSetUnion)
- 需要通过 runtime config 或 annotation 显式启用
- 启用开关：`vcpu_pcpu_binding=true`
- **VCPU 与 PCPU 对应关系**：
  1. 启用 vcpu_pcpu_binding：VCPUs : PCPUs = 1:1
  2. 通常情况下：
     - 对于 sandbox：VCPUs : PCPUs = 1:N，N = Size(cpuSetUnion) 或 = Sum(cpuCapacity)
     - 对于 container：VCPUs : PCPUs = 1:M，M = Size(cpuSet) 或 = cpuCapacity

#### CPU 容量为 0 的特殊情况
- 如果 `CPUCapacity = 0`，表示 pedestal (hypervisor) 不限制 CPU 用量
- 客户机可以尽可能使用分配的 CPU 资源

## 2. 内存资源映射修正

### 2.1 映射关系明确化

```
No Container Memory resource                     <====> RTOS Client pedestal max memory
Container memory limit      <====> RTOS Client memory limit
Container memory reservation < memory limit <====> RTOS Client memory min
```

### 2.2 关键代码修正

#### 2.2.1 `pkg/libmica/resource_manager.go`
- **`MemoryThresholdMB()`**：明确返回当前内存阈值
- **`CurrentMaxMem()`**：明确返回 RTOS Client memory limit
- **`RecordMemoryState()`**：实现单调递增特性，只更新更大的阈值
- **`EnsureMemoryLimit()`**：保证 `memory threshold >= container memory limit`
- **`UpdateMemoryThreshold()`**：实现单调递增，只增不减
- **`UpdateMemory()`**：更新 RTOS Client memory limit，同时保证阈值足够

#### 2.2.2 `pkg/oci/oci_configs.go` - `calculateClientMemThreshold()`
- 重命名函数以更清晰地表达其目的
- 添加了详细的内存资源映射规范注释
- 实现保守策略：`memory threshold = max(2 * memory limit, 默认值)`
- 确保 pedestal 有足够内存分配给 RTOS client

#### 2.2.3 内存阈值管理原则
1. **container.me.records**：记录 libmica 语境下的资源量
2. **container.me.memoryThreshold**：设计为单调递增的，仅在新的 memory threshold 出现时才会正向更新
3. **pedestal.EssentialResource**：不记录 memoryThreshold
   - `memorymaxmb` 对应 OCI spec 中的 `mem.Limit`
   - `mem min` 对应 OCI spec 中的 `mem.Reservation`
   - 该类型记录的是实际资源
4. 仅在 `micaexecutor` 中记录 `memoryThreshold`
5. 保证简单性：只有 `memory threshold >= container memory limit`

## 3. 新增文档和测试

### 3.1 新增文档
1. **`docs/resource-mapping.md`**：完整的资源映射规范文档
   - 详细说明了 CPU 和内存资源的映射关系
   - 解释了 VCPU 数量策略和 shared_cpu_pool 概念
   - 提供了配置优先级和实现要点

2. **`docs/resource-mapping-implementation.md`**（本文档）：实现修正总结

### 3.2 新增测试
1. **`pkg/pedestal/resource_mapping_test.go`**：全面的资源映射测试
   - 测试 CPU 资源映射的各种场景
   - 测试内存资源映射逻辑
   - 测试 ShareToWeight 转换函数
   - 验证资源映射原则

## 4. 核心概念澄清

### 4.1 pCPU 和 vCPU 关系
- **pCPU (Physical CPU)**：物理 CPU 核心，实际的硬件计算单元
- **vCPU (Virtual CPU)**：虚拟 CPU，Hypervisor 呈现给客户机的 CPU 抽象
- **cpuset (Affinity)**：CPU 亲和性设置，限制进程/客户机只能在指定的 pCPU 上运行

### 4.2 Shared CPU Pool 概念
- 如果一个容器设置了 cpuset，调度器不会允许它运行在 cpuset 之外的 pCPU 上
- 如果一个 sandbox 中有多个容器都设置了 cpuset，可以考虑将它们的 cpuset 并集作为一个 CPU pool
- **shared_cpu_pool** 选项：sandbox 内的所有容器都只能运行在这个 CPU pool 的 pCPU 上
- **当前状态**：仅在 MicRun 中保留的概念，未来可能实现对 pedestal CPU pool 的实际操控

### 4.3 面向 PPT 的设计说明
- VCPU 数量可以反映出 RTOS 内部能看到的 VCPU 数量和实际分给它的 PCPU 数量的对应关系
- 这是**面向PPT**的设计（`vcpu_pcpu_binding` 选项）
- 最佳实践：默认 VCPU = 1，必须显式启用才使用 VCPU = Size(cpuSetUnion)

## 5. 代码质量改进

### 5.1 注释增强
- 所有关键函数都添加了详细的资源映射注释
- 明确了每个参数的映射关系和转换公式
- 添加了使用示例和注意事项

### 5.2 调试信息改进
- 资源解析时输出详细的调试信息
- 包括原始值、转换后的值和映射关系
- 便于问题排查和性能分析

### 5.3 错误处理
- 增强了资源验证逻辑
- 添加了边界条件检查
- 提供了清晰的错误消息

## 6. 未来工作

### 6.1 待实现功能
1. **vcpu_pcpu_binding** 选项：需要通过 runtime config 或 annotation 实现
2. **shared_cpu_pool** 选项：需要实现对 pedestal CPU pool 的实际操控
3. **动态资源调度**：基于负载动态调整 RTOS 资源分配

### 6.2 优化方向
1. **性能优化**：减少资源映射的开销
2. **内存管理**：实现更精细的内存阈值管理
3. **监控集成**：与现有的监控系统集成

### 6.3 扩展功能
1. **异构计算**：支持 GPU、FPGA 等异构计算资源
2. **安全隔离**：增强 RTOS 之间的安全隔离机制
3. **能源管理**：根据负载动态调整 CPU 频率和功耗

## 7. 总结

本次修正工作系统地梳理和明确了 MicRun 的资源映射规范，确保了：

1. **正确性**：CPU 和内存资源从容器到 RTOS 客户机的映射关系正确
2. **清晰性**：代码注释和文档详细说明了映射逻辑
3. **可维护性**：代码结构清晰，便于后续扩展和维护
4. **可测试性**：新增了全面的测试用例，确保映射逻辑的正确性

通过这次修正，MicRun 的资源管理能力得到了显著提升，为后续的功能扩展和性能优化奠定了坚实的基础。

---
**版本**：1.0  
**最后更新**：2024年  
**维护者**：MicRun 开发团队  
**相关文档**：
- `docs/resource-design.md` - 资源设计文档
- `docs/resource-mapping.md` - 资源映射规范
- `pkg/pedestal/resource_mapping_test.go` - 资源映射测试