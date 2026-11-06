// Package agentguides owns slick's embedded agent runbooks.
package agentguides

import (
	"embed"
	"io/fs"
	"slices"
	"sync"

	"github.com/matcra587/docent"
)

//go:embed guides/*.md
var files embed.FS

// Files returns the guide-rooted filesystem for contract validation.
func Files() fs.FS {
	root, err := fs.Sub(files, "guides")
	if err != nil {
		panic(err)
	}
	return root
}

var load = sync.OnceValues(func() (*docent.GuideSet, error) {
	return docent.LoadGuides(Files())
})

// Load parses the embedded runbooks and validates their structure.
func Load() (*docent.GuideSet, error) { return load() }

// Names includes canonical slugs and preserved legacy aliases.
func Names() []string {
	guides, err := Load()
	if err != nil {
		return nil
	}
	var names []string
	for guide := range guides.All() {
		names = append(names, guide.Slug)
		names = append(names, guide.Aliases...)
	}
	slices.Sort(names)
	return names
}
