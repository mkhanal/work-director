// Package cards embeds the rule cards, one directory per category, so a
// binary built from this tree carries the cards it was built with.
package cards

import "embed"

//go:embed */*.md
var Snapshot embed.FS
