package web

import "embed"

// Assets contains the statically exported frontend files.
//
//go:embed all:dist
var Assets embed.FS
