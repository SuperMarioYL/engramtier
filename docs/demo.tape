# EngramTier demo tape (vhs). Renders assets/demo.gif on first tag via
# .github/workflows/demo.yml, and is committed so the README's Demo slot
# always shows a real gif.
#
# The 10-minute happy path, scripted with the no-model simulate path so the
# demo renders in CI without a 24 GiB model. A real run substitutes
# --heat-log <path> for --simulate; the report shape is identical.
Output assets/demo.gif

Set FontSize 15
Set Width 1200
Set Height 620
Set Padding 20
Set Theme Dracula

# Build the binary from the repo root so the tape drives the real CLI.
Type "go build -o ./engram-tier ./cmd/engram-tier && clear"
Enter
Sleep 1500

# 1. Profile a simulated sparse Qwen3.8 n-gram workload.
Type "# 1) profile the access heat of a sparse n-gram table"
Enter
Sleep 500
Type "./engram-tier profile --simulate --simulate-alpha 1.5 --table-bytes 24GiB --coverage 0.95"
Enter
Sleep 2500

# 2. Sweep the hot-set coverage and read the throughput-loss table.
Type "# 2) sweep coverage -> the 'lost only X% throughput' table"
Enter
Sleep 500
Type "./engram-tier bench --simulate --simulate-alpha 1.5 --table-bytes 24GiB"
Enter
Sleep 2500

# 3. Compose the cold-tier runtime against the profile report.
Type "# 3) serve preview: hot RAM working set + SSD cold region"
Enter
Sleep 500
Type "./engram-tier profile --simulate --simulate-alpha 1.5 --table-bytes 24GiB --out /tmp/profile.report >/dev/null"
Enter
Sleep 1500
Type "./engram-tier serve --hotset /tmp/profile.report"
Enter
Sleep 2500
