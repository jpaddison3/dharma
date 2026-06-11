# Shared build recipe for the distribution wrappers. Callers must set REPO_ROOT.

build_dharma() { # <goos> <goarch> <output-path>
  echo "building $1/$2..."
  CGO_ENABLED=0 GOOS="$1" GOARCH="$2" \
    go -C "$REPO_ROOT" build -trimpath -ldflags="-s -w" -o "$3" ./cmd/dharma
}
