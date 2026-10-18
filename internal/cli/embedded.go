package cli

import "embed"

//go:embed prompts/*.md templates/*.md
var promptFiles embed.FS
