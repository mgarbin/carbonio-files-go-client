#!/usr/bin/env bash
# Regenerates img/icon.ico and cmd/carbonio-files-go-client/rsrc_windows_amd64.syso
# from img/ico.png (the single source-of-truth image, see img/icon.go).
#
# Run this after changing img/ico.png; both generated files are committed to
# git (same rationale as frontend/dist: a checkout builds for Windows without
# extra tooling). Requires Python 3 + Pillow (for the .ico) and network
# access to the Go module proxy (for the throwaway winres generator module).
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
src_png="$repo_root/img/ico.png"
out_ico="$repo_root/img/icon.ico"
out_syso="$repo_root/cmd/carbonio-files-go-client/rsrc_windows_amd64.syso"

python3 -c "
from PIL import Image
im = Image.open('$src_png').convert('RGBA')
im.save('$out_ico', sizes=[(256,256),(128,128),(64,64),(48,48),(32,32),(16,16)])
"

scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
(
  cd "$scratch"
  go mod init icongen >/dev/null
  cat > main.go <<'EOF'
package main

import (
	"log"
	"os"

	"github.com/tc-hib/winres"
)

func main() {
	f, err := os.Open(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	ico, err := winres.LoadICO(f)
	if err != nil {
		log.Fatal(err)
	}

	rs := winres.ResourceSet{}
	// Resource ID = RT_ICON's numeric value (3), matching Wails' own
	// winres-based icon embedding (pkg/commands/build/packager.go) and
	// the fixed AppIconID Wails' Windows window-chrome code
	// (winc.AppIconID) reads the window/taskbar icon from.
	if err := rs.SetIcon(winres.RT_ICON, ico); err != nil {
		log.Fatal(err)
	}

	out, err := os.Create(os.Args[2])
	if err != nil {
		log.Fatal(err)
	}
	defer out.Close()

	if err := rs.WriteObject(out, winres.ArchAMD64); err != nil {
		log.Fatal(err)
	}
}
EOF
  go get github.com/tc-hib/winres@v0.3.1 >/dev/null
  go mod tidy >/dev/null
  go run . "$out_ico" "$out_syso"
)

echo "Regenerated $out_ico and $out_syso"
