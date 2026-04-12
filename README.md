# TanzuBench Tile

OpsManager tile for deploying TanzuBench on Tanzu Application Service.

## What's in the tile

- **Local leaderboard** — Next.js static site served via gorouter
- **Run benchmarks errand** — triggers the full 99-test suite against a configured model endpoint
- **Export results errand** — packages results for upload to the central TanzuBench leaderboard
- **Smoke tests** — verifies the model endpoint is reachable

## Building

```bash
# 1. Vendor the tanzubench suite
./scripts/vendor-tanzubench.sh /path/to/tanzubench

# 2. Add binary blobs (python3, node20)
bosh add-blob /path/to/cpython-3.12.tar.gz python3/cpython-3.12.tar.gz
bosh add-blob /path/to/node-v20.tar.gz node20/node-v20.tar.gz

# 3. Build the tile
pip install tile-generator
./scripts/build-tile.sh 0.1.0
```

## Deploying

1. Upload `product/tanzubench-<version>.pivotal` to OpsManager
2. Configure the GenAI model endpoint in the tile settings
3. Apply changes
4. Run benchmarks: `bosh -d <deployment> run-errand run-benchmarks`
5. View results at `https://tanzubench.<system-domain>/`

## Architecture

```
OpsManager Tile
├── tanzubench-web (BPM)     → port 8080 → gorouter → tanzubench.<sys-domain>
├── run-benchmarks (errand)  → runs bench_suite.py → writes to persistent disk
├── export-results (errand)  → tars results → bosh scp to retrieve
└── smoke-tests (errand)     → verifies endpoint reachable + model responds
```

Results are stored on persistent disk at `/var/vcap/store/tanzubench/results/`.
The web UI reads from the same path at build-time (static export) or serves
them dynamically.
