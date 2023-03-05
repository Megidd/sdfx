package render

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/deadsy/sdfx/render/buffer"
	"github.com/deadsy/sdfx/sdf"
	v3 "github.com/deadsy/sdfx/vec/v3"
)

// Hex8 is a 3D hexahedron consisting of 8 nodes.
// It's a kind of finite element, FE.
// https://en.wikipedia.org/wiki/Hexahedron
type Hex8 struct {
	// Coordinates of 8 corner nodes or vertices.
	V [8]v3.Vec
	// The layer to which tetrahedron belongs. Layers are along Z axis.
	// For finite element analysis - FEA - of 3D printed objects, it's more efficient to store layer along Z axis.
	// The 3D print is done along the Z axis. Likewise, FEA is done along the Z axis.
	// Sampling/marching algorithm is expected to return the layer to which a finite element belongs.
	layer int
}

//-----------------------------------------------------------------------------

// A mesh of 8-node hexahedra.
// A sophisticated data structure for mesh is required.
// The repeated nodes would be removed.
// The element connectivity would be created with unique nodes.
type MeshHex8 struct {
	// Index buffer.
	IBuff *buffer.Hex8IB
	// Vertex buffer.
	VBuff *buffer.VB
}

// To get a new mesh and number of its layers along Z-axis.
func NewMeshHex8(s sdf.SDF3, r RenderHex8) (*MeshHex8, int) {
	fes := ToHex8(s, r)

	_, _, layerCountZ := r.LayerCounts(s)

	m := newMeshHex8(layerCountZ)

	// Fill out the mesh with finite elements.
	for _, fe := range fes {
		nodes := [8]v3.Vec{}
		for n := 0; n < 8; n++ {
			nodes[n] = fe.V[n]
		}
		m.addFE(fe.layer, nodes)
	}

	defer m.VBuff.DestroyHashTable()

	return m, layerCountZ
}

func newMeshHex8(layerCount int) *MeshHex8 {
	return &MeshHex8{
		IBuff: buffer.NewHex8IB(layerCount),
		VBuff: buffer.NewVB(),
	}
}

// Add a finite element to mesh.
// Layer number and nodes are input.
// The node numbering should follow the convention of CalculiX.
// http://www.dhondt.de/ccx_2.20.pdf
func (m *MeshHex8) addFE(l int, nodes [8]v3.Vec) {
	indices := [8]uint32{}
	for n := 0; n < 8; n++ {
		indices[n] = m.addVertex(nodes[n])
	}
	m.IBuff.AddFE(l, indices)
}

func (m *MeshHex8) addVertex(vert v3.Vec) uint32 {
	return m.VBuff.Id(vert)
}

func (m *MeshHex8) vertexCount() int {
	return m.VBuff.VertexCount()
}

func (m *MeshHex8) vertex(i uint32) v3.Vec {
	return m.VBuff.Vertex(i)
}

// Number of layers along the Z axis.
func (m *MeshHex8) layerCount() int {
	return m.IBuff.LayerCount()
}

// Number of finite elements on a layer.
func (m *MeshHex8) feCountOnLayer(l int) int {
	return m.IBuff.FECountOnLayer(l)
}

// Number of finite elements for all layers.
func (m *MeshHex8) feCount() int {
	return m.IBuff.FECount()
}

// Get a finite element.
// Layer number is input.
// FE index on layer is input.
// FE index could be from 0 to number of tetrahedra on layer.
// Don't return error to increase performance.
func (m *MeshHex8) feIndicies(l, i int) [8]uint32 {
	return m.IBuff.FEIndicies(l, i)
}

// Get a finite element.
// Layer number is input.
// FE index on layer is input.
// FE index could be from 0 to number of tetrahedra on layer.
// Don't return error to increase performance.
func (m *MeshHex8) feVertices(l, i int) [8]v3.Vec {
	indices := m.IBuff.FEIndicies(l, i)
	vertices := [8]v3.Vec{}
	for n := 0; n < 8; n++ {
		vertices[n] = m.VBuff.Vertex(indices[n])
	}
	return vertices
}

// Write mesh to ABAQUS or CalculiX `inp` file.
func (m *MeshHex8) WriteInp(path string) error {
	return m.WriteInpLayers(path, 0, m.layerCount())
}

// Write specific layers of mesh to ABAQUS or CalculiX `inp` file.
// Result would include start layer.
// Result would exclude end layer.
func (m *MeshHex8) WriteInpLayers(path string, layerStart, layerEnd int) error {
	if 0 <= layerStart && layerStart < layerEnd && layerEnd <= m.layerCount() {
		// Good.
	} else {
		return fmt.Errorf("start or end layer is beyond range")
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	// Write headers.

	_, err = f.WriteString("**\n** Structure: finite elements of a 3D model.\n** Generated by: https://github.com/deadsy/sdfx\n**\n")
	if err != nil {
		return err
	}

	_, err = f.WriteString("*HEADING\nModel: 3D model Date: " + time.Now().UTC().Format("2006-Jan-02 MST") + "\n")
	if err != nil {
		return err
	}

	// Write nodes.

	_, err = f.WriteString("*NODE\n")
	if err != nil {
		return err
	}

	// To write only required nodes to the file.
	tempVBuff := buffer.NewVB()
	defer tempVBuff.DestroyHashTable()

	nodes := [8]v3.Vec{}
	ids := [8]uint32{}
	for l := layerStart; l < layerEnd; l++ {
		for i := 0; i < m.feCountOnLayer(l); i++ {
			// Get the node IDs.
			nodes = m.feVertices(l, i)
			for n := 0; n < 8; n++ {
				ids[n] = tempVBuff.Id(nodes[n])
			}

			// Write the node IDs.
			for n := 0; n < 8; n++ {
				// ID starts from one not zero.
				_, err = f.WriteString(fmt.Sprintf("%d,%f,%f,%f\n", ids[n]+1, float32(nodes[n].X), float32(nodes[n].Y), float32(nodes[n].Z)))
				if err != nil {
					return err
				}
			}
		}
	}

	// Write elements.

	_, err = f.WriteString("*ELEMENT, TYPE=C3D8, ELSET=Eall\n")
	if err != nil {
		return err
	}

	var eleID uint32
	for l := layerStart; l < layerEnd; l++ {
		for i := 0; i < m.feCountOnLayer(l); i++ {
			nodes = m.feVertices(l, i)
			for n := 0; n < 8; n++ {
				ids[n] = tempVBuff.Id(nodes[n])
			}

			// ID starts from one not zero.
			_, err = f.WriteString(fmt.Sprintf("%d,%d,%d,%d,%d,%d,%d,%d,%d\n", eleID+1, ids[0]+1, ids[1]+1, ids[2]+1, ids[3]+1, ids[4]+1, ids[5]+1, ids[6]+1, ids[7]+1))
			if err != nil {
				return err
			}
			eleID++
		}
	}

	return nil
}

//-----------------------------------------------------------------------------

// writeHex8 writes a stream of finite elements, in the shape of 8-node hexahedra, to an array.
func writeHex8(wg *sync.WaitGroup, hex8s *[]Hex8) chan<- []*Hex8 {
	// External code writes tetrahedra to this channel.
	// This goroutine reads the channel and stores tetrahedra.
	c := make(chan []*Hex8)

	wg.Add(1)
	go func() {
		defer wg.Done()
		// read finite elements from the channel and handle them
		for fes := range c {
			for _, fe := range fes {
				*hex8s = append(*hex8s, *fe)
			}
		}
	}()

	return c
}

//-----------------------------------------------------------------------------
