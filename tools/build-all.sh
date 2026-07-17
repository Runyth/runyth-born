#!/usr/bin/env bash
set -euo pipefail

declare -r script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
declare -r project_dir="$(cd -- "${script_dir}/.." && pwd)"
declare -r output_dir="${project_dir}/dist"
declare -r version="${VERSION:-dev}"

build_target() {
  declare -r goos="$1"
  declare -r goarch="$2"
  declare -r output_name="$3"

  printf 'Building %s/%s -> %s\n' "${goos}" "${goarch}" "${output_name}"
  CGO_ENABLED=0 GOOS="${goos}" GOARCH="${goarch}" \
    go build \
      -trimpath \
      -ldflags="-s -w -X main.version=${version}" \
      -o "${output_dir}/${output_name}" \
      ./cmd/template-wizard
}

cd -- "${project_dir}"
rm -rf -- "${output_dir}"
mkdir -p -- "${output_dir}"

go test ./...

build_target linux amd64 template-wizard-linux-amd64
build_target windows amd64 template-wizard-windows-amd64.exe
build_target darwin amd64 template-wizard-macos-amd64
build_target darwin arm64 template-wizard-macos-arm64

printf 'Built release files in %s\n' "${output_dir}"
