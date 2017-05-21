/* This file provides functions for reading and parsing input files in
different formats. */

package main

import (
	"bufio"
	"io"
	"strconv"
	"strings"
)

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
