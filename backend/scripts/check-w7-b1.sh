#!/usr/bin/env bash
# W7 B1: images are multi-stage, distroless and non-root.
set -euo pipefail
for bin in api worker; do
  docker build -q -f "Dockerfile.$bin" -t "zomato-$bin:check" . >/dev/null
  user=$(docker inspect --format '{{.Config.User}}' "zomato-$bin:check")
  size=$(docker image inspect --format '{{.Size}}' "zomato-$bin:check")
  if [ -z "$user" ] || [ "$user" = "root" ] || [ "$user" = "0" ]; then echo "FAIL: $bin image runs as root"; exit 1; fi
  if docker run --rm --entrypoint /bin/sh "zomato-$bin:check" -c true 2>/dev/null; then echo "FAIL: $bin image has a shell; use distroless/static"; exit 1; fi
  if [ "$size" -gt 60000000 ]; then echo "FAIL: $bin image is $((size/1000000)) MB; a static Go binary on distroless is under 60 MB"; exit 1; fi
  echo "PASS: $bin image user=$user size=$((size/1000000)) MB, no shell"
done
