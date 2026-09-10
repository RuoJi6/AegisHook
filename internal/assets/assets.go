package assets

import "embed"

//go:embed ui/* adapter.ts opencode.mjs hooks.sh hooks.ps1
var Files embed.FS
