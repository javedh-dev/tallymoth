#!/bin/bash
# Runs the whole matrix: CPU limits, memory limits and data sizes, with the server in Docker.
# Needs: Docker, the static binaries from `go build`, and ~15 GB of free disk for the 10M-event database.
#
#   CGO_ENABLED=0 go build -o /tmp/tm ./cmd/tallymoth && CGO_ENABLED=0 go build -o /tmp/bench ./tools/bench
#   docker build -t tallymoth:bench .
#   tools/bench/matrix.sh /tmp/tm /tmp/bench /var/tmp/bench-data /var/tmp/bench-out
#   /tmp/bench -report /var/tmp/bench-out -docs docs/performance.md
set -u
TM=${1:?path to the tallymoth binary}; BENCH=${2:?path to the bench binary}
DATA=${3:?folder for seeded databases (reused between runs)}; OUT=${4:?folder for result files}
mkdir -p "$DATA" "$OUT"; rm -f "$OUT"/*.json

run() { # label events cpus mem
  local name=${1/:/_}
  echo "=== $1 events=$2 cpus=${3:-none} mem=${4:-none} $(date +%T)"
  "$BENCH" -bin "$TM" -docker tallymoth:bench ${3:+-cpus "$3"} ${4:+-mem "$4"} \
    -data "$DATA" -events "$2" -secs 8 -label "$1" -out "$OUT/$name.json" >/dev/null 2>"$OUT/$name.log" || echo "FAILED $1"
}

for c in 0.5 1 2 4;            do run "cpu:$c" 1000000 "$c" "";  done
for m in 64m 128m 256m 512m 1g; do run "mem:$m" 1000000 2 "$m";  done
for n in 100000 5000000 10000000; do run "data:$n" "$n" 2 1g;     done
# the 1M data point is the same setup as mem:1g
python3 - "$OUT" <<'PY'
import json, sys
d = sys.argv[1]
r = json.load(open(f"{d}/mem_1g.json")); r["label"] = "data:1000000"
json.dump(r, open(f"{d}/data_1000000.json", "w"), indent=2)
PY
echo done $(date +%T)
