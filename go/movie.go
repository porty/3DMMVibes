package main

import (
	"fmt"
	"io"
	"sort"
)

// SceneRef is a reference to one SCEN child of the MVIE root chunk.
type SceneRef struct {
	CHID uint32 // child ID (position in scene list)
	CNO  uint32 // chunk number of the SCEN chunk
}

// Movie holds the top-level structure of a .3MM file.
type Movie struct {
	Scenes []SceneRef // ordered by CHID
}

// LoadMovie finds the MVIE root chunk in cf and returns the ordered list of
// SCEN children. r is the io.ReaderAt for the same file cf was parsed from.
func LoadMovie(cf *ChunkyFile, r io.ReaderAt) (*Movie, error) {
	// Find the MVIE root chunk (there should be exactly one).
	var mvieChunk Chunk
	found := false
	for _, c := range cf.Chunks {
		if c.CTG == ctgMVIE {
			mvieChunk = c
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("movie: no MVIE chunk found in file")
	}

	// Collect all SCEN children, sorted by CHID.
	type ref struct {
		chid uint32
		cno  uint32
	}
	var refs []ref
	for _, kid := range mvieChunk.Kids {
		if kid.CTG == ctgSCEN {
			refs = append(refs, ref{kid.CHID, kid.CNO})
		}
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].chid < refs[j].chid })

	scenes := make([]SceneRef, len(refs))
	for i, rr := range refs {
		scenes[i] = SceneRef{CHID: rr.chid, CNO: rr.cno}
	}
	return &Movie{Scenes: scenes}, nil
}
