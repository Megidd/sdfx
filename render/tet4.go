package render

import v3 "github.com/deadsy/sdfx/vec/v3"

// Tet4 is a 3D tetrahedron consisting of 4 nodes.
// It's a kind of finite element, FE.
// https://en.wikipedia.org/wiki/Tet4
type Tet4 struct {
	V [4]v3.Vec
}

// A mesh of tetrahedra with 4 nodes.
// A sophisticated data structure for mesh is required to store tetrahedra.
// The repeated nodes would be removed.
// The element connectivity would be created with unique nodes.
type MeshTet4 struct {
	T      []uint32              // Index buffer. Every 4 indices would correspond to a tetrahedron.
	V      []v3.Vec              // Vertex buffer. All unique.
	Lookup map[[3]float32]uint32 // Used to avoid repeating vertices when adding a new tetrahedron.
	vCount uint32                // TODO: Remove?
}

func NewMeshTet4() *MeshTet4 {
	return &MeshTet4{
		T:      []uint32{},
		V:      []v3.Vec{},
		Lookup: map[[3]float32]uint32{},
	}
}

// Just an optimization, if there is an estimation of tetrahedra count.
// Affects the speed according to experiments.
func (m *MeshTet4) Allocate(tetCount uint32) {
	m.T = make([]uint32, tetCount*4)
	m.V = make([]v3.Vec, tetCount/4*2) // By experimenting.
	m.Lookup = make(map[[3]float32]uint32, tetCount/4*2)
}

func (t *MeshTet4) AddTet(i uint32, a, b, c, d v3.Vec) {
	// The node numbering should follow the convention of CalculiX.
	// http://www.dhondt.de/ccx_2.20.pdf
	t.T[i*4], t.T[i*4+1], t.T[i*4+2], t.T[i*4+3] = t.AddVertex(a), t.AddVertex(b), t.AddVertex(c), t.AddVertex(d)
}

func (t *MeshTet4) AddVertex(vert v3.Vec) uint32 {
	// TODO: Binary insertion sort and search to eliminate extra allocation
	// TODO: Consider epsilon in comparison and use int (*100) for searching
	if vertID, ok := t.Lookup[[3]float32{float32(vert.X), float32(vert.Y), float32(vert.Z)}]; ok {
		return vertID
	}
	if t.VertexCount() <= int(t.vCount) {
		t.V = append(t.V, vert)
	} else {
		t.V[t.vCount] = vert
	}
	t.Lookup[[3]float32{float32(vert.X), float32(vert.Y), float32(vert.Z)}] = t.vCount
	t.vCount++
	return t.vCount - 1
}

func (m *MeshTet4) VertexCount() int {
	return len(m.V)
}
