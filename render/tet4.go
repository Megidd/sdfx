package render

import (
	"fmt"
	"os"
	"runtime"
	"time"

	v3 "github.com/deadsy/sdfx/vec/v3"
)

// Tet4 is a 3D tetrahedron consisting of 4 nodes.
// It's a kind of finite element, FE.
// https://en.wikipedia.org/wiki/Tetrahedron
type Tet4 struct {
	// Coordinates of 4 corner nodes or vertices.
	V [4]v3.Vec
	// The layer to which tetrahedron belongs. Layers are along Z axis.
	// For finite element analysis - FEA - of 3D printed objects, it's more efficient to store layer along Z axis.
	// The 3D print is done along the Z axis. Likewise, FEA is done along the Z axis.
	// Sampling/marching algorithm is expected to generate finite elements along the Z axis.
	layer int
}

// A mesh of tetrahedra with 4 nodes.
// A sophisticated data structure for mesh is required to store tetrahedra.
// The repeated nodes would be removed.
// The element connectivity would be created with unique nodes.
type MeshTet4 struct {
	// Index buffer.
	// Every 4 indices would correspond to a tetrahedron. Low-level for performance.
	// Tetrahedra are stored by their layer on Z axis.
	T [][]uint32
	// Vertex buffer.
	// All coordinates are unique.
	V []v3.Vec
	// Used to avoid repeating vertices when adding a new tetrahedron.
	Lookup map[[3]float64]uint32
}

func NewMeshTet4(layerCount int) *MeshTet4 {
	t := &MeshTet4{
		T:      nil,
		V:      []v3.Vec{},
		Lookup: map[[3]float64]uint32{},
	}

	// Initialize.
	t.T = make([][]uint32, layerCount)
	for l := 0; l < layerCount; l++ {
		t.T[l] = make([]uint32, 0)
	}

	return t
}

// Layer number and 4 nodes are input.
// The node numbering should follow the convention of CalculiX.
// http://www.dhondt.de/ccx_2.20.pdf
func (m *MeshTet4) AddTet4(l int, a, b, c, d v3.Vec) {
	m.T[l] = append(m.T[l], m.addVertex(a), m.addVertex(b), m.addVertex(c), m.addVertex(d))
}

func (m *MeshTet4) addVertex(vert v3.Vec) uint32 {
	// TODO: Binary insertion sort and search to eliminate extra allocation
	// TODO: Consider epsilon in comparison and use int (*100) for searching
	if vertID, ok := m.Lookup[[3]float64{vert.X, vert.Y, vert.Z}]; ok {
		// Vertex already exists. It's repeated.
		return vertID
	}

	// Vertex is new, so append it.
	m.V = append(m.V, vert)

	// Store index of the appended vertex.
	m.Lookup[[3]float64{vert.X, vert.Y, vert.Z}] = uint32(m.vertexCount() - 1)

	// Return index of the appended vertex.
	return uint32(m.vertexCount() - 1)
}

func (m *MeshTet4) vertexCount() int {
	return len(m.V)
}

func (m *MeshTet4) vertex(i int) v3.Vec {
	return m.V[i]
}

// To be called after adding all tetrahedra to the mesh.
func (t *MeshTet4) Finalize() {
	// Clear memory.
	t.Lookup = nil
	runtime.GC()
}

// Number of layers along the Z axis.
func (m *MeshTet4) LayerCount() int {
	return len(m.T)
}

// Number of tetrahedra on a layer.
func (m *MeshTet4) Tet4CountOnLayer(l int) int {
	return len(m.T[l]) / 4
}

// Number of tetrahedra for all layers.
func (m *MeshTet4) Tet4Count() int {
	var count int
	for _, t := range m.T {
		count += len(t) / 4
	}
	return count
}

// Layer number is input.
// Tetrahedron index on layer is input.
// Tetrahedron index could be from 0 to number of tetrahedra on layer.
// Don't return error to increase performance.
func (m *MeshTet4) Tet4Indicies(l, i int) (uint32, uint32, uint32, uint32) {
	return m.T[l][i*4], m.T[l][i*4+1], m.T[l][i*4+2], m.T[l][i*4+3]
}

// Layer number is input.
// Tetrahedron index on layer is input.
// Tetrahedron index could be from 0 to number of tetrahedra on layer.
// Don't return error to increase performance.
func (m *MeshTet4) Tet4Vertices(l, i int) (v3.Vec, v3.Vec, v3.Vec, v3.Vec) {
	return m.V[m.T[l][i*4]], m.V[m.T[l][i*4+1]], m.V[m.T[l][i*4+2]], m.V[m.T[l][i*4+3]]
}

// Write mesh to ABAQUS or CalculiX `inp` file.
func (m *MeshTet4) WriteInp(path string) error {
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

	var node v3.Vec
	for i := 0; i < m.vertexCount(); i++ {
		node = m.vertex(i)
		// ID starts from one not zero.
		_, err = f.WriteString(fmt.Sprintf("%d,%f,%f,%f\n", i+1, float32(node.X), float32(node.Y), float32(node.Z)))
		if err != nil {
			return err
		}
	}

	// Write elements.

	_, err = f.WriteString("*ELEMENT, TYPE=C3D4, ELSET=Eall\n")
	if err != nil {
		return err
	}

	var eleID uint32
	var nodeID0, nodeID1, nodeID2, nodeID3 uint32
	for l := 0; l < m.LayerCount(); l++ {
		for i := 0; i < m.Tet4CountOnLayer(l); i++ {
			nodeID0, nodeID1, nodeID2, nodeID3 = m.Tet4Indicies(l, i)
			// ID starts from one not zero.
			_, err = f.WriteString(fmt.Sprintf("%d,%d,%d,%d,%d\n", eleID+1, nodeID0+1, nodeID1+1, nodeID2+1, nodeID3+1))
			if err != nil {
				return err
			}
			eleID++
		}
	}

	return nil
}
