package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/deckarep/golang-set"
	"github.com/spakin/disjoint"
)

// notify is used to output error messages.
var notify *log.Logger

type Empty struct{} // Zero-byte object

// checkError is a convenience function that aborts on error.
func checkError(e error) {
	if e != nil {
		notify.Fatal(e)
	}
}

// A Graph is a collection of named vertices and edges.  Both vertices and
// edges have an associated weight.
type Graph struct {
	Vs map[string]float64    // Map from a vertex to a weight
	Es map[[2]string]float64 // Map from an edge to a weight
}

// ReadQMASMFile returns the Ising Hamiltonian represented by a QMASM source
// file.
func ReadQMASMFile(r io.Reader) Graph {
	vs := make(map[string]float64)    // Map from a vertex to a weight
	es := make(map[[2]string]float64) // Map from an edge to a weight
	rb := bufio.NewReader(r)
	for {
		// Read one line.
		ln, err := rb.ReadString('\n')
		if err == io.EOF {
			break
		}
		checkError(err)

		// Discard comments.
		hIdx := strings.Index(ln, "#")
		if hIdx >= 0 {
			ln = ln[:hIdx]
		}

		// Parse the line.
		fs := strings.Fields(ln)
		switch len(fs) {
		case 2:
			// Vertex
			v := fs[0]
			wt, err := strconv.ParseFloat(fs[1], 64)
			checkError(err)
			vs[v] += wt
		case 3:
			// Edge, chain, or alias
			var u, v string
			var wt float64
			if fs[1] == "=" || fs[1] == "<->" {
				// Chain or alias
				u, v = fs[0], fs[2]
				wt = -1.0
			} else {
				u, v = fs[0], fs[1]
				wt, err = strconv.ParseFloat(fs[2], 64)
				checkError(err)
			}
			if u > v {
				u, v = v, u
			}
			es[[2]string{u, v}] += wt
			vs[u] += 0.0
			vs[v] += 0.0
		}
	}
	return Graph{Vs: vs, Es: es}
}

// ReadQubistFile returns the Ising Hamiltonian represented by a Qubist source
// file.
func ReadQubistFile(r io.Reader) Graph {
	// Read and discard the first (header) line.
	vs := make(map[string]float64)    // Map from a vertex to a weight
	es := make(map[[2]string]float64) // Map from an edge to a weight
	rb := bufio.NewReader(r)
	ln, err := rb.ReadString('\n')
	checkError(err)

	// Process all remaining lines.
	for {
		// Read one line.
		ln, err = rb.ReadString('\n')
		if err == io.EOF {
			break
		}
		checkError(err)

		// Parse the line.
		fs := strings.Fields(ln)
		if len(fs) == 3 {
			u, v := fs[0], fs[1]
			wt, err := strconv.ParseFloat(fs[2], 64)
			checkError(err)
			if u == v {
				// Vertex
				vs[u] += wt
			} else {
				// Edge
				if u > v {
					u, v = v, u
				}
				es[[2]string{u, v}] += wt
				vs[u] += 0.0
				vs[v] += 0.0
			}
		} else {
			notify.Fatalf("Failed to parse Qubist line %q", strings.TrimSpace(ln))
		}
	}
	return Graph{Vs: vs, Es: es}
}

// spanningTree returns a list of edges in a spanning tree and a list of
// non-tree edges.
func (g Graph) spanningTree() ([][2]string, [][2]string) {
	// Place each vertex in its own set.
	vSet := make(map[string]*disjoint.Element, len(g.Vs))
	for v := range g.Vs {
		vSet[v] = disjoint.NewElement()
	}

	// Add each edge in turn to either a tree list or a non-tree list.
	tEdges := make([][2]string, 0, len(g.Es))
	ntEdges := make([][2]string, 0, len(g.Es))
	for e := range g.Es {
		u, v := vSet[e[0]], vSet[e[1]]
		if u.Find() == v.Find() {
			// Same set --> non-tree edge
			ntEdges = append(ntEdges, e)
		} else {
			// Different sets --> tree edge (and put in same set)
			disjoint.Union(u, v)
			tEdges = append(tEdges, e)
		}
	}
	return tEdges, ntEdges
}

// neighbors returns a map from each vertex to a set of vertices it directly
// touches.
func (g Graph) neighbors(es [][2]string) map[string]map[string]Empty {
	ns := make(map[string]map[string]Empty, len(es))
	for _, e := range es {
		u, v := e[0], e[1]
		if _, ok := ns[v]; !ok {
			ns[v] = make(map[string]Empty)
		}
		ns[v][u] = Empty{}
		if _, ok := ns[u]; !ok {
			ns[u] = make(map[string]Empty)
		}
		ns[u][v] = Empty{}
	}
	return ns
}

// findPath returns the unique path from a source vertex to a destination
// vertex.
func (g Graph) findPath(ns map[string]map[string]Empty, s, d string) []string {
	// Define a depth-first search function.
	visited := make(map[string]Empty)
	var dfs func(s, d string) []string
	dfs = func(s, d string) []string {
		if _, ok := ns[s][d]; ok {
			// Final hop
			return []string{d}
		}
		for m := range ns[s] {
			// Visit each new neighbor in a depth-first manner.
			if _, ok := visited[m]; ok {
				continue // Already visited m
			}
			visited[m] = Empty{}
			path := dfs(m, d)
			if path != nil {
				return append(path, m)
			}
			delete(visited, m)
		}
		return nil // Dead end
	}

	// Perform a depth-first search.  The results come back in reverse
	// order so we in fact search from destination to source.
	visited[d] = Empty{}
	path := append(dfs(d, s), d)
	return path
}

// baseCyclePaths returns a base set of cyclic paths that appear in the graph.
func (g Graph) baseCyclePaths() [][]string {
	tEdges, ntEdges := g.spanningTree()
	ns := g.neighbors(tEdges)
	cycles := make([][]string, 0, len(ntEdges))
	for _, nt := range ntEdges {
		cycles = append(cycles, g.findPath(ns, nt[0], nt[1]))
	}
	return cycles
}

// pathToEdges converts a cycle to a list of edges.  Edge order is
// canonicalized.
func (g Graph) pathToEdges(c []string) [][2]string {
	nv := len(c)
	edges := make([][2]string, nv)
	for i, v1 := range c {
		v2 := c[(i+1)%nv]
		if v1 > v2 {
			v1, v2 = v2, v1
		}
		edges[i] = [2]string{v1, v2}
	}
	return edges
}

// edgesToPath converts a list of edges to a path.
func (g Graph) edgesToPath(es [][2]string) []string {
	// Map each vertex to the two vertices it abuts.
	near := make(map[string][]string, len(es))
	minV := es[0][0]
	for _, e := range es {
		// Keep track of the minimum vertex name.
		if e[0] < minV {
			minV = e[0]
		}
		if e[1] < minV {
			minV = e[1]
		}

		// Make e[1] a neighbor of e[0].
		if abuts, ok := near[e[0]]; ok {
			near[e[0]] = append(abuts, e[1])
		} else {
			abuts = make([]string, 1, 2)
			abuts[0] = e[1]
			near[e[0]] = abuts
		}

		// Make e[0] a neighbor of e[1].
		if abuts, ok := near[e[1]]; ok {
			near[e[1]] = append(abuts, e[0])
		} else {
			abuts = make([]string, 1, 2)
			abuts[0] = e[0]
			near[e[1]] = abuts
		}
	}

	// Construct a chain of vertices, always choosing the unique neighbor
	// that that was not already visited.
	p := make([]string, 1, len(es))
	p[0] = minV // Start with the minimum vertex to be more deterministic.
	for len(near) > 1 {
		last := p[len(p)-1]
		abuts := near[last]
		delete(near, last)
		if _, ok := near[abuts[0]]; ok {
			p = append(p, abuts[0])
		} else {
			p = append(p, abuts[1])
		}
	}
	return p
}

// elementaryCycles takes a list of basic cycles and combines these to form all
// elementary cycles using Gibb's algorithm
// (cf. http://dspace.mit.edu/bitstream/handle/1721.1/68106/FTL_R_1982_07.pdf,
// p. 14).
func (g Graph) elementaryCycles(bcs [][][2]string) [][][2]string {
	// Convert the input list of lists of edges to a list of sets of edges.
	phi := make([]mapset.Set, len(bcs))
	for i, c := range bcs {
		phi[i] = mapset.NewSet()
		for _, e := range c {
			phi[i].Add(e)
		}
	}

	// Initialize our various data structures.
	s := mapset.NewSet(phi[0])
	q := mapset.NewSet(phi[0])
	r := mapset.NewSet()
	rs := mapset.NewSet()

	// Consider each basic cycle in turn.
	for i := 1; i < len(phi); i++ {
		// Add to either r or rs the symmetric difference of each cycle
		// in q with the current phi.
		for ti := range q.Iterator().C {
			t := ti.(mapset.Set)
			diff := t.SymmetricDifference(phi[i])
			if t.Intersect(phi[i]).Cardinality() == 0 {
				rs.Add(diff)
			} else {
				r.Add(diff)
			}
		}

		// If any cycle in r is a proper superset of another cycle in
		// r, move the superset from r to rs.
		move := mapset.NewSet() // Elements to move from r to rs
		var wg sync.WaitGroup
		rSlice := r.ToSlice()
		for _, ui := range rSlice {
			wg.Add(1)
			go func(u mapset.Set) {
				defer wg.Done()
				for _, vi := range rSlice {
					v := vi.(mapset.Set)
					if u.IsProperSubset(v) {
						move.Add(v)
					}
				}
			}(ui.(mapset.Set))
		}
		wg.Wait()
		r = r.Difference(move)
		rs = rs.Union(move)

		// Copy r and phi into both s and q then additionally copy rs
		// into q.
		s = s.Union(r)
		s.Add(phi[i])
		q = q.Union(r).Union(rs)
		q.Add(phi[i])
		r.Clear()
		rs.Clear()
	}

	// Convert from a set of sets back to a list of lists.
	ecs := make([][][2]string, 0, s.Cardinality())
	for ci := range s.Iterator().C {
		c := ci.(mapset.Set)
		cyc := make([][2]string, 0, c.Cardinality())
		for ei := range c.Iterator().C {
			e := ei.([2]string)
			cyc = append(cyc, e)
		}
		ecs = append(ecs, cyc)
	}
	return ecs
}

// isFrustrated says whether a cycle is frustrated (i.e., has an odd number of
// antiferromagnetic couplings).
func (g Graph) isFrustrated(p []string) bool {
	afm := uint(0)
	np := len(p)
	for i, u := range p {
		// Determine the coupler strength of edge UV and the strength
		// of the external field applied to each of vertices U and V.
		v := p[(i+1)%np]
		if u > v {
			u, v = v, u
		}
		cs := g.Es[[2]string{u, v}]
		ef := [2]float64{g.Vs[u], g.Vs[v]}

		// If both external fields are stronger than the coupler
		// strength, they override the coupler value in determining if
		// we have a ferromagnetic or antiferromagnetic coupling.
		if math.Abs(ef[0]) > math.Abs(cs) && math.Abs(ef[1]) > math.Abs(cs) {
			// External fields dominate.
			switch {
			case ef[0] > 0.0 && ef[1] < 0.0:
				afm++
			case ef[0] < 0.0 && ef[1] > 0.0:
				afm++
			}
		} else {
			// Coupler strength dominates.
			if cs > 0 {
				afm++
			}
		}
	}
	return afm&1 == 1
}

func OutputResults(w io.Writer, g Graph, ecs [][][2]string) {
	// Convert the edges back to paths for a more readable presentation.
	// Determine which paths are frustrated cycles.
	ps := make([][]string, len(ecs))
	isFrust := make([]bool, len(ecs))
	for i, ec := range ecs {
		ps[i] = g.edgesToPath(ec)
		isFrust[i] = g.isFrustrated(ps[i])
	}

	// Output each cycle preceded by whether it is frustrated or not.  As
	// we go along, keep track of all vertices that appear within a
	// frustrated cycle, and tally the number of frustrated cycles
	// encountered.
	fvs := make(map[string]Empty, len(g.Vs))
	nfcs := 0 // Number of frustrated cycles
	for i, p := range ps {
		f := isFrust[i]
		if f {
			fmt.Fprintf(w, "FC  ")
			nfcs++
		} else {
			fmt.Fprintf(w, "NFC ")
		}
		for _, v := range p {
			fmt.Fprintf(w, " %s", v)
			if f {
				fvs[v] = Empty{}
			}
		}
		fmt.Fprintln(w, "")
	}

	// Tally the number of times each vertex appears in a frustrated cycle
	// and in a non-frustrated cycle.
	fVerts := make(map[string]int)
	nfVerts := make(map[string]int)
	for i, p := range ps {
		for _, v := range p {
			if isFrust[i] {
				fVerts[v]++
			} else {
				nfVerts[v]++
			}
		}
	}

	// Output each vertex, categorized and tallied.  Keep track of the
	// number of vertices that are more frustrated than not frustrated.
	nfvs := 0 // Number of frustrated vertices
	for v, t := range fVerts {
		if t > nfVerts[v] {
			fmt.Fprintf(w, "FV   %d %d | %s\n", t, t-nfVerts[v], v)
			nfvs++
		}
	}
	for v, t := range nfVerts {
		if t > fVerts[v] {
			fmt.Fprintf(w, "NFV  %d %d | %s\n", t, t-fVerts[v], v)
		}
	}

	// Tally the number of times each edge appears in a frustrated cycle
	// and in a non-frustrated cycle.
	fEdges := make(map[[2]string]int)
	nfEdges := make(map[[2]string]int)
	for i, p := range ps {
		for j, v1 := range p {
			v2 := p[(j+1)%len(p)]
			if v1 > v2 {
				v1, v2 = v2, v1
			}
			e := [2]string{v1, v2}
			if isFrust[i] {
				fEdges[e]++
			} else {
				nfEdges[e]++
			}
		}
	}

	// Output each edge, categorized and tallied.
	nfes := 0 // Number of frustrated edges
	for e, t := range fEdges {
		if t > nfEdges[e] {
			fmt.Fprintf(w, "FE   %d %d | %s %s\n", t, t-nfEdges[e], e[0], e[1])
			nfes++
		}
	}
	for e, t := range nfEdges {
		if t > fEdges[e] {
			fmt.Fprintf(w, "NFE  %d %d | %s %s\n", t, t-fEdges[e], e[0], e[1])
		}
	}

	// Output some cycle, edge, and vertex statistics.
	fmt.Fprintf(w, "#FC  %d / %d = %f\n", nfcs, len(ps), float64(nfcs)/float64(len(ps)))
	fmt.Fprintf(w, "#FE  %d / %d = %f\n", nfes, len(g.Es), float64(nfes)/float64(len(g.Es)))
	fmt.Fprintf(w, "#FV  %d / %d = %f\n", nfvs, len(g.Vs), float64(nfvs)/float64(len(g.Vs)))
}

func main() {
	// Parse the command line.
	var err error
	notify = log.New(os.Stderr, os.Args[0]+": ", 0)
	inFmt := ""
	flag.StringVar(&inFmt, "format", "qubist", `input file format: "qubist" (default) or "qmasm"`)
	flag.StringVar(&inFmt, "f", "qubist", "shorthand for --format")
	outFile := ""
	flag.StringVar(&outFile, "output", "", "output file name (default: standard output)")
	flag.StringVar(&outFile, "o", "", "shorthand for --output")
	allCycs := flag.Bool("all-cycles", false, "Combine base cycles into elementary cycles (extremely slow; default: false)")
	flag.Parse()

	// Open the output file.
	var w io.Writer = os.Stdout
	if outFile != "" {
		f, err := os.Create(outFile)
		checkError(err)
		defer f.Close()
		w = f
	}

	// Open the input file.
	var r io.Reader
	switch flag.NArg() {
	case 0:
		// Read from standard input.
		r = os.Stdin
	case 1:
		// Read from the named file.
		r, err = os.Open(flag.Arg(0))
		checkError(err)
	default:
		notify.Fatal("More than one input file was specified")
	}

	// Read the input file into a graph.
	var g Graph
	switch inFmt {
	case "qmasm":
		g = ReadQMASMFile(r)
	case "qubist":
		g = ReadQubistFile(r)
	default:
		notify.Fatalf("Unrecognized input format %q", inFmt)
	}

	// Acquire a list of basic cycles and from that a list of elementary
	// cycles.
	bPath := g.baseCyclePaths()
	bcs := make([][][2]string, len(bPath))
	for i, p := range bPath {
		bcs[i] = g.pathToEdges(p)
	}
	fmt.Fprintf(w, "#BCS %d\n", len(bcs))
	var ecs [][][2]string
	if *allCycs {
		ecs = g.elementaryCycles(bcs)
	} else {
		ecs = bcs
	}

	// Tell the user what we discovered.
	OutputResults(w, g, ecs)
}
