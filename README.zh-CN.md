[English](README.md) | **简体中文**

<picture>
  <source media="(max-width: 640px) and (prefers-color-scheme: dark)" srcset="assets/presentation/hero-mobile-dark.svg">
  <source media="(max-width: 640px)" srcset="assets/presentation/hero-mobile-light.svg">
  <source media="(prefers-color-scheme: dark)" srcset="assets/presentation/hero-dark.svg">
  <img src="assets/presentation/hero-light.svg" width="960" alt="EngramTier — Measure access patterns before choosing a tier.">
</picture>

**EngramTier 汇总 n-gram 查询日志，输出访问热度、Pareto 曲线和给定覆盖目标下的工作集大小，帮助检查冷热分层是否值得继续实验。**

`Go 1.24+` · [MIT](LICENSE) · [GitHub](https://github.com/SuperMarioYL/engramtier) · [网站](https://engramtier.lei6393.com)

## 为什么需要它

表很大不等于访问一定稀疏。先看真实工作负载访问过哪些 key、各访问多少次，才能讨论需要常驻多少条目。EngramTier 将查询日志变成可复查的覆盖曲线；分层后的实际内存与吞吐收益仍需运行时测量。

<picture>
  <source media="(max-width: 640px) and (prefers-color-scheme: dark)" srcset="assets/presentation/process-mobile-dark.svg">
  <source media="(max-width: 640px)" srcset="assets/presentation/process-mobile-light.svg">
  <source media="(prefers-color-scheme: dark)" srcset="assets/presentation/process-dark.svg">
  <img src="assets/presentation/process-light.svg" width="960" alt="Read a ten-lookup example">
</picture>

## 架构

Collector 按日志行聚合 key 计数，HeatMap 排序后供 Pareto 与 WorkingSet 查询使用。report 生成文本和 CSV；bench 使用同一热度数据及默认延迟模型做数学模拟。probe 保存 llama.cpp 插桩补丁。tier 的 mmap/LRU 组件已经存在，但 serve 只组装预览，并未接入实时模型查询。

<picture>
  <source media="(max-width: 640px) and (prefers-color-scheme: dark)" srcset="assets/presentation/architecture-mobile-dark.svg">
  <source media="(max-width: 640px)" srcset="assets/presentation/architecture-mobile-light.svg">
  <source media="(prefers-color-scheme: dark)" srcset="assets/presentation/architecture-dark.svg">
  <img src="assets/presentation/architecture-light.svg" width="960" alt="Lookup records to working-set estimates">
</picture>

## 安装

需要 Go 1.24+。构建可能下载模块；下面的日志分析不加载模型，也不需要 GPU。

```bash
git clone https://github.com/SuperMarioYL/engramtier.git
cd engramtier
go build -o engram-tier ./cmd/engram-tier
```

## 快速开始

```bash
bash docs/demo.sh
```

自带日志有 10 次查询，3 个 key 的次数为 6、3、1。给定 64 KiB 表大小和 16 字节条目，90% 覆盖对应 2 个 key、32 B 热工作集。例子被判为 NOT SPARSE。

## 用法

```bash
# 读取已有日志
./engram-tier profile --heat-log examples/presentation.heat.log --table-bytes 64KiB --entry-bytes 16 --coverage 0.9

# 保存文本/机器摘要及 Pareto CSV
./engram-tier profile --heat-log examples/presentation.heat.log --out profile.report --csv pareto.csv

# 使用数学延迟模型扫描覆盖目标
./engram-tier bench --heat-log examples/presentation.heat.log --table-bytes 64KiB --entry-bytes 16 --csv sweep.csv
```

日志以 `# engram-tier heat-log v1` 开头，每行一个由冒号分隔的 2–3 个 token ID。重复行表示重复查询。`--simulate` 可另行创建合成 Zipf 日志；真实日志不需要该标志。

## 能力与集成

| 环节 | 当前状态 |
|---|---|
| Heat log | 读取并聚合原始查询行 |
| Profile | 文本摘要与 Pareto CSV |
| Bench | 默认延迟参数下的模拟表/CSV |
| llama.cpp probe | 提供补丁和应用/重构建命令；已对 llama.cpp master 96ffdc41（2026-09-14）验证，见 `internal/probe/testdata/` |
| Serve | mmap/LRU 组装预览，实时 wiring 未完成 |
| MCP | 命令只输出路线图提示 |

<picture>
  <source media="(max-width: 640px) and (prefers-color-scheme: dark)" srcset="assets/presentation/integrations-mobile-dark.svg">
  <source media="(max-width: 640px)" srcset="assets/presentation/integrations-mobile-light.svg">
  <source media="(prefers-color-scheme: dark)" srcset="assets/presentation/integrations-dark.svg">
  <img src="assets/presentation/integrations-light.svg" width="960" alt="Implemented analysis routes">
</picture>

## 配置与边界

| profile 参数 | 默认值 |
|---|---|
| --table-bytes | 24GiB，须替换为你的表尺寸 |
| --entry-bytes | 4096，须匹配条目大小 |
| --coverage | 0.95 |
| --simulate-distinct | 200000 |
| --simulate-alpha | 1.2 |

直接采集要求同时提供 --llamacpp-bin 与 --model，且二进制确实带有插桩；只给 --model 不会运行。内置补丁作用于 common/ngram-cache.cpp，已对 llama.cpp master 96ffdc41ceb055e1c2d3d96667ae6d9f0ccb710b 验证；其他版本可能无法应用。

bench 的 100 ns 热查询、50000 ns 冷查询、每 token 1 次查询及 25000000 ns 基础解码时间来自代码默认值，不是本机测得。CLI 当前没有这些延迟参数的独立 flags；改变模拟模型需使用 Go API/源码。详见 internal/bench/harness.go。不要把模拟 t/s 当实测发布。

## 运行记录

v0.1.0 对构造日志的实际 profile 输出。表尺寸和条目大小是输入假设，不是模型结构或硬件测量；没有执行 SSD offload。

[输入、命令和完整输出](docs/demo-results.json)

[保留的历史终端录屏](assets/demo.gif) · [录制脚本](docs/demo.tape)。本轮示例以以上可重放记录为准。

## 路线图

- [x] 查询日志、访问计数、Pareto 曲线与工作集报告。
- [x] 可显式标注的延迟模拟和 CSV。
- [x] mmap/LRU 基础组件及 serve 预览。
- [ ] 在实际查询路径中接入冷热分层。
- [ ] 目标模型/硬件上的内存、吞吐实测。
- [ ] MCP 和多机聚合。

当前不提供付费 fleet 服务或托管控制台。

## 开发与许可证

```bash
go test ./...
```

主要实现：[collector](internal/heat/collector.go)、[Pareto](internal/heat/pareto.go)、[模拟模型](internal/bench/harness.go)。

[MIT](LICENSE) · [Issues](https://github.com/SuperMarioYL/engramtier/issues)
