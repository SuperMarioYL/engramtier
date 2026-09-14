// Package cli wires the heat, tier, probe, report, and bench packages into the
// cobra command tree a user invokes as `engram-tier`. Each subcommand is a
// thin adapter: it parses flags, calls one package, and prints the result.
// All domain logic lives in the packages; the CLI owns only arg plumbing and
// process orchestration (running the patched llama.cpp, reading a heat log).
package cli

import (
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/SuperMarioYL/engramtier/internal/bench"
	"github.com/SuperMarioYL/engramtier/internal/heat"
	"github.com/SuperMarioYL/engramtier/internal/probe"
	"github.com/SuperMarioYL/engramtier/internal/report"
	"github.com/SuperMarioYL/engramtier/internal/tier"
)

// NewRoot builds the command tree. It is exported so cmd/engram-tier/main.go
// can wire it to os.Args without duplicating the command definitions here.
func NewRoot() *cobra.Command {
	root := &cobra.Command{
		Use:   "engram-tier",
		Short: "Profile Qwen3.8 n-gram table access heat and offload the cold majority to SSD",
		Long: `EngramTier instruments the keyed n-gram lookup in a patched llama.cpp to
measure which entries of Qwen3.8-Flash-Next's ~24GiB n-gram table your workload
actually hits, then reports the hot-set Pareto curve and the cold-region size
that can be offloaded to SSD/NVMe.

  engram-tier patch-llamacpp --llamacpp ~/src/llama.cpp
  engram-tier profile --model qwen3.8-flash-next-q4.gguf --prompt prompt.txt --tokens 512
  engram-tier serve   --model qwen3.8-flash-next-q4.gguf --hotset profile.report`,
		SilenceUsage: true,
	}
	root.AddCommand(
		newPatchLlamaCppCmd(),
		newProfileCmd(),
		newServeCmd(),
		newBenchCmd(),
		newMCPCmd(),
	)
	return root
}

// newPatchLlamaCppCmd implements `engram-tier patch-llamacpp`: apply the
// vendored probe to a llama.cpp checkout and (optionally) rebuild it.
func newPatchLlamaCppCmd() *cobra.Command {
	var (
		llamacpp string
		rebuild  bool
		buildScript string
		force     bool
	)
	c := &cobra.Command{
		Use:   "patch-llamacpp",
		Short: "Apply the n-gram access-heat probe to a llama.cpp checkout",
		Long: `Applies the vendored probe patch (internal/probe/llamacpp-ngram.patch) to a
llama.cpp source tree, adding a per-key emitter at the n-gram cache lookup
sites in common/ngram-cache.cpp, gated by ENGRAM_TIER_PROFILE=1. Verified
against llama.cpp master 96ffdc41ceb055e1c2d3d96667ae6d9f0ccb710b; newer or
older trees may not accept the patch (go test ./internal/probe/ checks it
against a vendored copy of that file). With --rebuild, runs
scripts/build-llamacpp.sh to produce the instrumented binary. Idempotent: a
tree already carrying the probe marker is left alone unless --force.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := probe.Apply(llamacpp, force); err != nil {
				if err == probe.ErrAlreadyApplied {
					fmt.Fprintln(cmd.OutOrStdout(), "probe already applied — nothing to do")
					return nil
				}
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "probe applied to", llamacpp)
			if rebuild {
				script := buildScript
				if script == "" {
					script = "scripts/build-llamacpp.sh"
				}
				bc := exec.Command("/bin/sh", script, llamacpp)
				bc.Stdout = cmd.OutOrStdout()
				bc.Stderr = cmd.ErrOrStderr()
				if err := bc.Run(); err != nil {
					return fmt.Errorf("rebuild failed: %w", err)
				}
				fmt.Fprintln(cmd.OutOrStdout(), "instrumented llama.cpp rebuilt")
			}
			return nil
		},
	}
	c.Flags().StringVar(&llamacpp, "llamacpp", "", "path to the llama.cpp checkout root (required)")
	c.Flags().BoolVar(&rebuild, "rebuild", false, "rebuild the instrumented llama.cpp binary after patching")
	c.Flags().StringVar(&buildScript, "build-script", "", "path to build-llamacpp.sh (default scripts/build-llamacpp.sh)")
	c.Flags().BoolVar(&force, "force", false, "re-apply the patch even if the marker is present")
	_ = c.MarkFlagRequired("llamacpp")
	return c
}

// newProfileCmd implements `engram-tier profile`: produce the access-heat
// report. The real path runs a patched llama.cpp with ENGRAM_TIER_PROFILE=1 to
// emit a heat log, then aggregates it; --heat-log reads a log produced by a
// prior run; --simulate synthesizes a sparse heat log so the tool is
// demonstrable without a 24GiB model.
func newProfileCmd() *cobra.Command {
	var (
		model        string
		promptFile   string
		tokens       int
		heatLog      string
		llamacppBin  string
		simulate     bool
		simDistinct  int
		simAlpha     float64
		out          string
		csvOut       string
		tableBytes   string
		entryBytes   int
		coverage     float64
	)
	c := &cobra.Command{
		Use:   "profile",
		Short: "Run one instrumented session and print the n-gram access-heat report",
		RunE: func(cmd *cobra.Command, args []string) error {
			logPath := heatLog
			if logPath == "" {
				logPath = "engram-tier.heat.log"
			}

			if simulate {
				if err := writeSimulatedLog(logPath, simDistinct, simAlpha); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "wrote simulated heat log: %s\n", logPath)
			} else if heatLog == "" && llamacppBin != "" && model != "" {
				// Run the instrumented llama.cpp to produce the heat log.
				if err := runInstrumented(llamacppBin, model, promptFile, tokens, logPath, cmd.OutOrStdout()); err != nil {
					return err
				}
			} else if heatLog == "" {
				return fmt.Errorf("profile: provide --heat-log <path>, or --simulate, or --llamacpp-bin + --model to run a real session")
			}

			col := heat.NewCollector()
			if err := col.LoadLog(logPath); err != nil {
				return err
			}
			hm := col.HeatMap()

			tb, err := parseBytes(tableBytes)
			if err != nil {
				return err
			}
			spec := report.Spec{
				TableBytes:     tb,
				EntryBytes:     float64(entryBytes),
				Coverage:       coverage,
				SparsityThresh: 0.10,
				SparsityMin:    0.80,
			}
			rep := report.Build(hm, spec)
			fmt.Fprint(cmd.OutOrStdout(), rep.Text)

			if out != "" {
				if err := report.Write(out, rep); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "report written to %s\n", out)
			}
			if csvOut != "" {
				f, err := os.Create(csvOut)
				if err != nil {
					return err
				}
				defer f.Close()
				if err := report.WriteCSV(f, hm); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "pareto CSV written to %s\n", csvOut)
			}
			return nil
		},
	}
	c.Flags().StringVar(&model, "model", "", "path to the GGUF model (real-session mode)")
	c.Flags().StringVar(&promptFile, "prompt", "", "prompt file (real-session mode)")
	c.Flags().IntVar(&tokens, "tokens", 512, "tokens to generate (real-session mode)")
	c.Flags().StringVar(&heatLog, "heat-log", "", "read a heat log produced by a prior run instead of running llama.cpp")
	c.Flags().StringVar(&llamacppBin, "llamacpp-bin", "", "path to the instrumented llama.cpp binary (real-session mode)")
	c.Flags().BoolVar(&simulate, "simulate", false, "synthesize a sparse heat log for a no-model demo")
	c.Flags().IntVar(&simDistinct, "simulate-distinct", 200000, "distinct keys in the simulated table")
	c.Flags().Float64Var(&simAlpha, "simulate-alpha", 1.2, "Zipf alpha (higher = sparser); 1.0 = uniform-ish")
	c.Flags().StringVar(&out, "out", "", "write the profile report (text + machine summary) to this path")
	c.Flags().StringVar(&csvOut, "csv", "", "write the full Pareto curve as CSV to this path")
	c.Flags().StringVar(&tableBytes, "table-bytes", "24GiB", "total n-gram table size")
	c.Flags().IntVar(&entryBytes, "entry-bytes", 4096, "bytes per n-gram table entry (Qwen3.8 ~1024-dim float32 embedding)")
	c.Flags().Float64Var(&coverage, "coverage", 0.95, "lookup coverage the hot working set must reach")
	return c
}

// newServeCmd implements `engram-tier serve`. m2 wires the cold-tier runtime
// into the live lookup path; until then it composes the tier + LRU against the
// profile report and reports the planned hot/cold split, marking the live
// integration as an m2 TODO.
func newServeCmd() *cobra.Command {
	var (
		hotset    string
		coldFile  string
		cacheSize int
	)
	c := &cobra.Command{
		Use:   "serve",
		Short: "Run Qwen3.8 with the cold tier on SSD (m2)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if hotset == "" {
				return fmt.Errorf("serve: --hotset <profile.report> is required (run `engram-tier profile --out ...` first)")
			}
			sum, err := readSummary(hotset)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "EngramTier serve (m2 preview)\n")
			fmt.Fprintf(cmd.OutOrStdout(), "===========================\n")
			fmt.Fprintf(cmd.OutOrStdout(), "hotset:   %d keys (%.1f%% of %d distinct) = %.1f GiB RAM\n",
				sum.HotKeys, sum.HotFraction*100, sum.DistinctKeys, sum.HotBytes/(1<<30))
			fmt.Fprintf(cmd.OutOrStdout(), "cold tier:%.1f GiB on SSD (%s)\n", sum.ColdBytes/(1<<30), coldFile)
			fmt.Fprintf(cmd.OutOrStdout(), "sparse:   %v — %s\n", sum.Sparse, sum.SparsityNote)

			// Compose the cold-tier components to prove they assemble; the live
			// lookup-path wiring is the m2 milestone and is not yet built.
			t, err := tier.NewMMapTier(coldFile, int64(sum.DistinctKeys), 8)
			if err != nil {
				return fmt.Errorf("serve: cold tier init failed: %w", err)
			}
			defer t.Close()
			cache := tier.NewLRU(cacheSize)
			lookup := &tier.TieredLookup{Cold: t, Cache: cache}
			_ = lookup
			fmt.Fprintf(cmd.OutOrStdout(), "\nm2 TODO: wire TieredLookup into the patched n-gram lookup site\n")
			fmt.Fprintf(cmd.OutOrStdout(), "cold region mapped: %d entries x %d bytes\n", t.Count(), t.EntryBytes())
			return nil
		},
	}
	c.Flags().StringVar(&hotset, "hotset", "", "path to the profile report from `engram-tier profile --out` (required)")
	c.Flags().StringVar(&coldFile, "cold-file", "engram-tier.cold.bin", "path to the SSD-mapped cold region file")
	c.Flags().IntVar(&cacheSize, "cache-size", 4096, "LRU hot-cache capacity (cold lookups absorbed in RAM)")
	_ = c.MarkFlagRequired("hotset")
	return c
}

// newBenchCmd implements `engram-tier bench`: sweep hot-set coverage and emit
// the benchmark table + CSV (the m3 artifact). It needs a heat log produced by
// `profile`; like profile it accepts --simulate to run without a model.
func newBenchCmd() *cobra.Command {
	var (
		heatLog    string
		simulate   bool
		simDistinct int
		simAlpha   float64
		tableBytes string
		entryBytes int
		csvOut     string
	)
	c := &cobra.Command{
		Use:   "bench",
		Short: "Sweep hot-set coverage and print the throughput-loss table (m3)",
		RunE: func(cmd *cobra.Command, args []string) error {
			logPath := heatLog
			if logPath == "" {
				logPath = "engram-tier.heat.log"
			}
			if simulate {
				if err := writeSimulatedLog(logPath, simDistinct, simAlpha); err != nil {
					return err
				}
			}
			col := heat.NewCollector()
			if err := col.LoadLog(logPath); err != nil {
				return err
			}
			tb, err := parseBytes(tableBytes)
			if err != nil {
				return err
			}
			rows := bench.Sweep(bench.Spec{
				Heat:       col.HeatMap(),
				TableBytes: tb,
				EntryBytes: float64(entryBytes),
				Model:      bench.DefaultModel(),
			})
			fmt.Fprint(cmd.OutOrStdout(), bench.TextTable(rows))
			if csvOut != "" {
				f, err := os.Create(csvOut)
				if err != nil {
					return err
				}
				defer f.Close()
				if err := bench.WriteCSV(f, rows); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "\nbench CSV written to %s\n", csvOut)
			}
			return nil
		},
	}
	c.Flags().StringVar(&heatLog, "heat-log", "", "heat log to sweep (default engram-tier.heat.log)")
	c.Flags().BoolVar(&simulate, "simulate", false, "synthesize a sparse heat log for a no-model demo")
	c.Flags().IntVar(&simDistinct, "simulate-distinct", 200000, "distinct keys in the simulated table")
	c.Flags().Float64Var(&simAlpha, "simulate-alpha", 1.2, "Zipf alpha (higher = sparser)")
	c.Flags().StringVar(&tableBytes, "table-bytes", "24GiB", "total n-gram table size")
	c.Flags().IntVar(&entryBytes, "entry-bytes", 4096, "bytes per n-gram table entry (Qwen3.8 ~1024-dim float32 embedding)")
	c.Flags().StringVar(&csvOut, "csv", "", "write the sweep as CSV to this path")
	return c
}

// newMCPCmd is the MCP roadmap stub. The heat map becomes an MCP tool in v0.2
// so agent stacks can query a working set; v0.1 ships the CLI. MCP is the
// interop layer the profiler plugs into, not a v0.1 dependency.
func newMCPCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Expose the heat map as an MCP tool (roadmap, v0.2)",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintln(cmd.OutOrStdout(), "engram-tier mcp serve is a v0.2 roadmap item.")
			fmt.Fprintln(cmd.OutOrStdout(), "The heat map will be exposed as an MCP tool so agent stacks can query a working set;")
			fmt.Fprintln(cmd.OutOrStdout(), "see the Roadmap section of the README.")
		},
	}
}

// runInstrumented invokes the patched llama.cpp binary with the profile gate
// set so it streams the heat log, then returns. The GGUF model and prompt are
// forwarded verbatim; the caller owns ensuring the binary was built from a
// patched tree.
func runInstrumented(bin, model, promptFile string, tokens int, logPath string, out interface{}) error {
	if _, err := os.Stat(bin); err != nil {
		return fmt.Errorf("llama.cpp binary not found: %w", err)
	}
	if _, err := os.Stat(model); err != nil {
		return fmt.Errorf("model not found: %w", err)
	}
	args := []string{"-m", model, "-n", strconv.Itoa(tokens)}
	if promptFile != "" {
		args = append(args, "-p", promptFile)
	}
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), "ENGRAM_TIER_PROFILE=1", "ENGRAM_TIER_HEAT_LOG="+logPath)
	cmd.Stdout = nil
	if w, ok := out.(interface{ Write([]byte) (int, error) }); ok {
		cmd.Stdout = w
	}
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// writeSimulatedLog synthesizes a heat log whose lookup counts follow a Zipf
// distribution parameterized by alpha. Higher alpha concentrates lookups on a
// few keys (sparser); alpha=1 is near-uniform. The log carries the heat header
// and one key per line, matching what the patched probe emits.
func writeSimulatedLog(path string, distinct int, alpha float64) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.WriteString(heat.HeatLogHeader + "\n"); err != nil {
		return err
	}
	// Zipf over ranks 1..distinct: p(k) ~ 1/k^alpha, normalized. A small key
	// space (3 two-token combos) keeps the log compact while preserving the
	// heavy tail a real sparse table would show.
	z := rand.NewZipf(rand.New(rand.NewPCG(1, 2)), alpha, 1.0, uint64(distinct))
	const lookups = 5000
	for i := 0; i < lookups; i++ {
		rank := z.Uint64() + 1
		// Map a rank to a plausible 2-token key: token0 = rank mod vocab,
		// token1 = rank / vocab. Keeps keys distinct and realistic-sized.
		const vocab = 128000
		t0 := uint32(rank % vocab)
		t1 := uint32(rank / vocab)
		key := heat.NGramKey{N: 2, Tok: [3]uint32{t0, t1, 0}}
		if _, err := f.WriteString(key.String() + "\n"); err != nil {
			return err
		}
	}
	return nil
}

// parseBytes converts a size string with optional binary/decimal suffix to
// bytes. Supports B, KB/MB/GB/TB (decimal) and KiB/MiB/GiB/TiB (binary). A bare
// number is bytes.
func parseBytes(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty size")
	}
	mult := 1.0
	lower := strings.ToLower(s)
	cases := []struct {
		suffix string
		factor float64
	}{
		{"tib", 1 << 40}, {"gib", 1 << 30}, {"mib", 1 << 20}, {"kib", 1 << 10},
		{"tb", 1e12}, {"gb", 1e9}, {"mb", 1e6}, {"kb", 1e3}, {"b", 1},
	}
	for _, c := range cases {
		if strings.HasSuffix(lower, c.suffix) {
			mult = c.factor
			s = s[:len(s)-len(c.suffix)]
			break
		}
	}
	n, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q: %w", s, err)
	}
	return n * mult, nil
}

// summaryPair is the subset of the machine-readable report block serve reads.
// Kept here rather than in the report package because the wire format is a CLI
// concern (the report writes it; only the CLI parses it back).
type summaryPair struct {
	TotalLookups uint64
	DistinctKeys int
	Coverage     float64
	HotKeys      int
	HotFraction  float64
	HotBytes     float64
	ColdBytes    float64
	Sparse       bool
	SparsityNote string
}

// readSummary parses the machine-readable block that report.Write appends to a
// profile report. It tolerates the human text above it by scanning for the
// `key=value` lines under the known header.
func readSummary(path string) (summaryPair, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return summaryPair{}, err
	}
	var s summaryPair
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "=") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		k, v := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		switch k {
		case "total_lookups":
			s.TotalLookups, _ = strconv.ParseUint(v, 10, 64)
		case "distinct_keys":
			s.DistinctKeys, _ = strconv.Atoi(v)
		case "coverage":
			s.Coverage, _ = strconv.ParseFloat(v, 64)
		case "hot_keys":
			s.HotKeys, _ = strconv.Atoi(v)
		case "hot_fraction":
			s.HotFraction, _ = strconv.ParseFloat(v, 64)
		case "hot_bytes":
			s.HotBytes, _ = strconv.ParseFloat(v, 64)
		case "cold_bytes":
			s.ColdBytes, _ = strconv.ParseFloat(v, 64)
		case "sparse":
			s.Sparse = v == "true"
		case "sparsity_note":
			s.SparsityNote = v
		}
	}
	return s, nil
}
