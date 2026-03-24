package mm

import (
	"fmt"
	"io"
)

// ChunkDAG writes a Graphviz DOT digraph to w representing the parent→child
// relationships between chunks in cf. Each node shows the chunk's CTG tag,
// CNO (hex), and size in bytes. Root nodes (chunks with no incoming edges)
// are styled as filled blue boxes; all others are plain ellipses.
//
// Render the output with: dot -Tpng -o graph.png graph.dot
func ChunkDAG(cf *ChunkyFile, w io.Writer) {
	// Build a set of all chunk keys that appear as a child (have a parent).
	hasParent := make(map[uint64]bool)
	for _, c := range cf.Chunks {
		for _, kid := range c.Kids {
			hasParent[chunkKey(kid.CTG, kid.CNO)] = true
		}
	}

	fmt.Fprintln(w, "digraph chunks {")
	fmt.Fprintln(w, "\trankdir=LR")

	// Emit nodes.
	for _, c := range cf.Chunks {
		id := nodeID(c.CTG, c.CNO)
		label := fmt.Sprintf("%s\\n0x%08X\\n%d bytes", CTGToString(c.CTG), c.CNO, c.Size)
		if c.Name != "" {
			label += "\\n" + c.Name
		}
		if !hasParent[chunkKey(c.CTG, c.CNO)] {
			fmt.Fprintf(w, "\t%s [label=\"%s\" shape=box style=filled fillcolor=lightblue]\n", id, label)
		} else {
			fmt.Fprintf(w, "\t%s [label=\"%s\"]\n", id, label)
		}
	}

	// Emit edges.
	for _, c := range cf.Chunks {
		pid := nodeID(c.CTG, c.CNO)
		for _, kid := range c.Kids {
			cid := nodeID(kid.CTG, kid.CNO)
			if kid.CHID != 0 {
				fmt.Fprintf(w, "\t%s -> %s [label=\"chid=%d\"]\n", pid, cid, kid.CHID)
			} else {
				fmt.Fprintf(w, "\t%s -> %s\n", pid, cid)
			}
		}
	}

	fmt.Fprintln(w, "}")
}

// chunkKey returns a unique uint64 key for the (CTG, CNO) pair.
func chunkKey(ctg, cno uint32) uint64 {
	return uint64(ctg)<<32 | uint64(cno)
}

// nodeID returns the quoted DOT node identifier for a (CTG, CNO) pair.
func nodeID(ctg, cno uint32) string {
	return fmt.Sprintf("%q", fmt.Sprintf("%s/0x%08X", CTGToString(ctg), cno))
}
