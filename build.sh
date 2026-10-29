#!/usr/bin/env bash
# Compila WiFiDrop para todos los sistemas de escritorio en ./dist
set -euo pipefail
cd "$(dirname "$0")"
VERSION="${1:-$(git describe --tags --always 2>/dev/null || echo dev)}"
mkdir -p dist
# Icono y metadatos del .exe de Windows (se enlazan solos como *.syso).
if go run ./tools/winres "${VERSION#v}"; then
  echo "  recursos de Windows generados"
else
  echo "  aviso: no se pudieron generar los recursos de Windows (sin icono en el .exe)"
fi
build() {
  local os=$1 arch=$2 ext=${3:-}
  echo "  $os/$arch"
  CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -ldflags "-s -w -X main.version=$VERSION" \
    -o "dist/wifidrop-$os-$arch$ext" .
}
echo "WiFiDrop $VERSION"
build windows amd64 .exe
build windows arm64 .exe
build darwin  amd64
build darwin  arm64
build linux   amd64
build linux   arm64
echo "Listo: $(ls dist | tr '\n' ' ')"
