package render

import (
	"os"
	"sync"
)

// writeFE writes a stream of finite elements in the shape of tetrahedra to an ABAQUS or CalculiX file.
func writeFE(wg *sync.WaitGroup, path string) (chan<- []*Tetrahedron, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}

	// External code writes tetrahedra to this channel.
	// This goroutine reads the channel and writes tetrahedra to the file.
	c := make(chan []*Tetrahedron)

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer f.Close()

		// read tetrahedra from the channel and write them to the file
		for ts := range c {
			for _, t := range ts {
				_ = t
				// TODO.
			}
		}
	}()

	return c, nil
}
