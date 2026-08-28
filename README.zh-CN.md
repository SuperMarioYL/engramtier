<div align="right"><sub><a href="./README.md">English</a>&nbsp;&nbsp;⇄&nbsp;&nbsp;<b>中文</b></sub></div>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="./assets/hero-dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="./assets/hero-light.svg">
  <img src="./assets/hero-light.svg" width="880" alt="EngramTier — 分析 Qwen3.8 n-gram 访问热度，将冷区主体卸载到 SSD">
</picture>

<p align="center"><sub>EngramTier 是面向自托管用户的 MCP 技术栈 n-gram 表分析器，把 Qwen3.8 的冷条目卸载到 SSD。</sub></p>

<p align="center">
  <a href="./LICENSE"><img alt="license" src="https://img.shields.io/badge/license-MIT-blue"></a>
  <a href="https://github.com/SuperMarioYL/engramtier/releases"><img alt="release" src="https://img.shields.io/github/v/release/SuperMarioYL/engramtier"></a>
  <a href="https://github.com/SuperMarioYL/engramtier/actions/workflows/ci.yml"><img alt="ci" src="https://img.shields.io/github/actions/workflow/status/SuperMarioYL/engramtier/ci.yml?branch=main&label=ci"></a>
  <a href="https://goreportcard.com/report/github.com/SuperMarioYL/engramtier"><img alt="go report card" src="https://goreportcard.com/badge/github.com/SuperMarioYL/engramtier"></a>
  <img alt="go" src="https://img.shields.io/badge/Go-1.24-00ADD8?logo=go&logoColor=white">
  <img alt="MCP-ready" src="https://img.shields.io/badge/MCP--ready-interop-5E5CE6">
</p>

**Qwen3.8-Flash-Next 的 ~24 GiB n-gram 查找表是把真实 4-bit 量化推到 80–90 GB 的关键变量——EngramTier 分析你的工作负载到底命中了哪些带键条目，再把冷区主体卸载到 SSD/NVMe，让模型以个位数吞吐损失塞进本地内存。**

### 为什么是现在

n-gram 带键嵌入表是 Qwen 架构特有的原语，两年前还不存在，而它刚刚越过了一条预算线：[Mac Studio 的真实测速](https://terminalbytes.com/run-qwen-3-8-27b-locally/) 显示自托管用户已把 24 GB 的表视为"被稀疏访问 → 非常适合卸载到系统内存"——但还没有人真正基准测试过。EngramTier 的存在就是用真实的帕累托曲线去证伪这个说法，然后再做点什么。分析器运行在模型服务层，而 MCP 是当前活跃的互操作标准（[punkpeye/awesome-mcp-servers](https://github.com/punkpeye/awesome-mcp-servers)）；热度图将在路线图上成为一个 MCP 工具，让 Agent 栈可以查询工作集。

<h2><img src="https://api.iconify.design/tabler:topology-star-3.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> 架构</h2>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="./assets/atlas-dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="./assets/atlas-light.svg">
  <img src="./assets/atlas-light.svg" width="880" alt="架构：打过补丁的 llama.cpp 向 Go 收集器发送热度日志，后者推导出帕累托工作集；冷分层运行时把冷查找回送给 llama.cpp">
</picture>

Profile 模式跑一个进程：打过补丁的 llama.cpp 发出热度日志（原始查找，每行一个 n-gram 键），进程内的 Go 收集器聚合成访问热度图。Serve 模式跑一个进程：冷分层运行时（一个 mmap 的 SSD 区域，前面挂一个小 LRU）把冷查找回送给 llama.cpp。没有微服务、没有 Kubernetes、没有数据库。

## 目录

- [安装与快速开始](#安装与快速开始)
- [用法](#用法)
- [演示](#演示)
- [基准测试](#基准测试)
- [工作原理](#工作原理)
- [定价](#定价)
- [路线图](#路线图)
- [许可证](#许可证)

<h2><img src="https://api.iconify.design/tabler:rocket.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> 安装与快速开始</h2>

从冷克隆到首个可见结果不到一分钟——不需要 24 GiB 模型，`--simulate` 标志合成一个稀疏的 n-gram 工作负载：

```bash
git clone https://github.com/SuperMarioYL/engramtier && cd engramtier
go build -o engram-tier ./cmd/engram-tier
./engram-tier profile --simulate --table-bytes 24GiB --coverage 0.95
```

<details><summary>示例输出</summary>

```
Total lookups:     5,000
Distinct keys hit: 397
Table size:         24.0 GiB
Pareto curve (hottest keys first):
  top 1.0%   → 63.5% of lookups
  top 10.1%  → 88.8% of lookups
Working set @ 95% coverage: 147 keys (37.0%) = 588.0 KiB RAM
Cold region (offload to SSD):   24.00 GiB
Sparseness verdict: SPARSE — top-10% of keys cover >= 80% of lookups: cold-tier thesis HOLDS.
```

</details>

真实 Qwen3.8 运行请先安装打过补丁的 llama.cpp：

```bash
engram-tier patch-llamacpp --llamacpp ~/src/llama.cpp --rebuild
# 然后用打过插桩的二进制：
ENGRAM_TIER_PROFILE=1 ENGRAM_TIER_HEAT_LOG=q.heat.log \
  ~/src/llama.cpp/build-engram-tier/bin/llama-speculative -m qwen3.8-flash-next-q4.gguf -n 512
engram-tier profile --heat-log q.heat.log --table-bytes 24GiB --out profile.report
```

> Homebrew：`brew install engram-tier` 是计划中的一行命令（formula 落地后）；在此之前用 `go install github.com/SuperMarioYL/engramtier/cmd/engram-tier@latest` 编译二进制。

<h2><img src="https://api.iconify.design/tabler:terminal-2.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> 用法</h2>

五个子命令；`profile` 是 m1 的门槛，`serve`/`bench` 是 m2/m3 的交付物（serve 的查找路径实接是 m2 的 TODO，已明确标注）。

```bash
# 1) 给 llama.cpp 打补丁并重新编译（一次性，<5 分钟）
engram-tier patch-llamacpp --llamacpp ~/src/llama.cpp --rebuild

# 2) 分析一次会话 → 访问热度报告
engram-tier profile --model qwen3.8-flash-next-q4.gguf --prompt prompt.txt --tokens 512 \
  --table-bytes 24GiB --out profile.report

# 3) 开启 SSD 冷分层运行 → tokens/s 对比全内存基线（m2）
engram-tier serve --hotset profile.report

# 4) 扫描热集覆盖率 → 吞吐损失表（m3）
engram-tier bench --heat-log q.heat.log --table-bytes 24GiB --csv sweep.csv

# 5) 把热度图暴露为 MCP 工具（路线图，v0.2）
engram-tier mcp
```

主要参数：

| 参数 | 默认值 | 含义 |
|---|---|---|
| `--simulate` / `--simulate-alpha` | off / 1.2 | 合成 Zipf n-gram 工作负载（无需模型） |
| `--heat-log` | `engram-tier.heat.log` | 读取打过补丁运行生成的热度日志 |
| `--table-bytes` | `24GiB` | n-gram 表总大小 |
| `--entry-bytes` | 4096 | 每个表条目字节数（约 1024 维 float32） |
| `--coverage` | 0.95 | 热工作集需达到的查找覆盖率 |
| `--out` | — | 写出报告（文本 + 机读摘要） |
| `--csv` | — | 把帕累托曲线 / 扫描写成 CSV |

<h2><img src="https://api.iconify.design/tabler:photo.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> 演示</h2>

![demo](assets/demo.gif)

30 秒走查：`profile --simulate` 打印帕累托曲线和稀疏性判定，`bench` 把热集覆盖率扫描成吞吐损失表，`serve` 针对报告组装冷分层运行时。该 gif 由 `docs/demo.tape` 渲染，并可由 `demo` 工作流按需重渲染。

<h2><img src="https://api.iconify.design/tabler:chart-bar.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> 基准测试</h2>

基准测试表是发布的核心物料——"我把 Qwen3.8 的 24 GiB n-gram 表卸载到 SSD，只损失 X% 吞吐。" 下表数字是**模拟值**（来自 harness 的延迟模型，不是真实 serve 运行）；方法学和每一个旋钮都在 [`docs/benchmark-methodology.md`](./docs/benchmark-methodology.md)。真实数字会在 `engram-tier serve` 于目标机器上跑过之后替换掉模拟列。

| 覆盖率 | 热内存 | 冷 SSD | 卸载占比 | 基线 t/s | 分层 t/s | 损失 |
|---|---|---|---|---|---|---|
| 50% | 8.0 KiB | 24.00 GiB | 100.0% | 40.0 | 40.0 | 0.10% |
| 80% | 60.0 KiB | 24.00 GiB | 100.0% | 40.0 | 40.0 | 0.04% |
| 90% | 196.0 KiB | 24.00 GiB | 100.0% | 40.0 | 40.0 | 0.02% |
| 95% | 588.0 KiB | 24.00 GiB | 100.0% | 40.0 | 40.0 | 0.01% |
| 99% | 1.4 MiB | 24.00 GiB | 100.0% | 40.0 | 40.0 | 0.00% |

诚实读法：在 95% 热集时冷未命中率是 5%，而 n-gram 查找只占每 token 解码时间的一小部分，所以模拟吞吐损失低于 0.1%。在引用数字之前，请用你自己的 serve 运行重新校准 `coldLatencyNs`（默认 50 µs，NVMe 换页）和 `lookupsPerToken`。

<h2><img src="https://api.iconify.design/tabler:bulb.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> 工作原理</h2>

新原语是 **n-gram 访问热度图**：一份按工作负载记录的、每个带键 n-gram（Qwen3.8 带键嵌入表的 2–3 token 索引）被查找的频率，外加推导出的帕累托 / 工作集曲线。

```go
type HeatMap struct {
    Keys         []NGramKey   // 被访问的互异 n-gram 键
    Counts       []uint64     // 每个键的查找次数（对齐）
    TotalLookups uint64
}
// 累计查找达到 coverage 的最小键集
func (h HeatMap) WorkingSet(coverage, entryBytes float64) (keyCount int, keyFraction, byteSize float64)
```

冷分层把表当作两个区域——一个离线从热度图选出的、驻留内存的热工作集，和一个通过小 LRU 服务的、mmap 到 SSD 的冷区。这个"带键查找 + 热度"原语在闭源前沿模型中不存在（GPT 之类没有 n-gram 表），在 xpref / k3thrash 中也不存在（它们按路由 logit 限定 MoE 专家，而非带键表）。

### EngramTier 对比通用张量卸载

llama.cpp 和 vLLM 把张量均匀卸载得很好——它们是既得者，在通用性上完胜。EngramTier 故意做得更窄：

| 维度 | EngramTier | llama.cpp 通用卸载 |
|---|---|---|
| 按条目访问热度分析 | ✓ 带键 n-gram 查找 | ✗ 把每个张量一视同仁 |
| 用实测热度做热/冷切分 | ✓ 离线静态切分 | ✗ 全有或全无的张量驻留 |
| 针对 24 GiB n-gram 表 | ✓ Qwen 专属带键查找 | 部分——模型无关，无 n-gram 感知 |
| 模型通用性 | — 仅 Qwen n-gram 表 | ✓ 数十个模型 |
| LRU 吸收冷未命中延迟 | ✓ | ✗ 每次查找都是全量冷未命中 |

## 定价

EngramTier 单机版免费开源——分析器是地基。**EngramTier Team** 是面向跑 3–10 台 Qwen3.8 机器的小实验室的付费舰队版，在 v0.1 里只是一个 CLI 标志桩（还没有后端）：

- **按舰队聚合热集** —— 每台机器都分析，看舰队级工作集漂移。
- **托管遥测面板** —— 热集重算告警，按机器下发 SSD 分层配置。
- **MCP 端点**，把舰队工作集暴露给 Agent 栈。
- **定价**：约 $49/台/月，或 5 台实验室约 $290/月一口价。按机器计费，不按席位——这是基础设施。

单机分析器永久免费；付费基础设施是 v0.2/v0.3，等实验室真的要聚合时再做。不要把发布卡在付费基础设施上——先发基准测试。

<h2><img src="https://api.iconify.design/tabler:map-2.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> 路线图</h2>

- [x] **m1 — 分析 n-gram 访问热度**：探针补丁 + 热度收集器 + 帕累托分析器；`engram-tier profile` 打印"前 X% 的键 → Y% 查找，卸载 Z GB"。证伪稀疏性说法。
- [ ] **m2 — 冷条目分层**：冷分层运行时（mmap SSD + LRU）接进查找路径；`engram-tier serve` 让 Qwen3.8 部分驻留 SSD 运行并打印 tokens/s 对比全内存。*前提是 m1 确认稀疏。*
- [ ] **m3 — 发布基准演示**：基准 harness + Show-HN 数字 + EngramTier Team 舰队版桩。
- [ ] **v0.2 — MCP 工具**：把热度图暴露为 MCP 工具，让 Agent 栈可以查询工作集。
- [ ] **上游**：向 llama.cpp 贡献一个按键热度钩子，让 EngramTier 成为 n-gram 分析器的参考实现。

## 分享这个

```
EngramTier — 基于 MCP 技术栈的分析器，把 Qwen3.8 的 24GB n-gram 表卸载到 SSD。分析你到底命中了哪些条目，卸载冷区主体，只损失个位数 % 吞吐。 https://github.com/SuperMarioYL/engramtier
```

<h2><img src="https://api.iconify.design/tabler:license.svg?color=%230071E3&width=24" height="22" align="absmiddle" alt=""> 许可证</h2>

MIT —— 见 [LICENSE](./LICENSE)。提 issue 或开 PR；基准方法学文档是反驳任何数字的地方。

<p align="center"><sub><a href="./LICENSE">MIT</a> © 2026 SuperMarioYL</sub></p>
