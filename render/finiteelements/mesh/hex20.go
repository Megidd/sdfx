package mesh

import (
	"github.com/deadsy/sdfx/render"
	"github.com/deadsy/sdfx/render/finiteelements/buffer"
	"github.com/deadsy/sdfx/sdf"
	v3 "github.com/deadsy/sdfx/vec/v3"
)

// Hex20 is a mesh of 20-node hexahedra.
// A sophisticated data structure for mesh is required.
// The repeated nodes would be removed.
// The element connectivity would be created with unique nodes.
type Hex20 struct {
	// Index buffer.
	IBuff *buffer.IB
	// Vertex buffer.
	VBuff *buffer.VB
}

// NewHex20 returns a new mesh and number of its layers along Z-axis.
func NewHex20(s sdf.SDF3, r render.RenderFE) (*Hex20, int) {
	fes := render.ToHex20(s, r)

	_, _, layerCountZ := r.LayerCounts(s)

	m := newHex20(layerCountZ)

	// Fill out the mesh with finite elements.
	for _, fe := range fes {
		nodes := [20]v3.Vec{}
		for n := 0; n < 20; n++ {
			nodes[n] = fe.V[n]
		}
		m.addFE(fe.Layer, nodes)
	}

	defer m.VBuff.DestroyHashTable()

	return m, layerCountZ
}

func newHex20(layerCount int) *Hex20 {
	return &Hex20{
		IBuff: buffer.NewIB(layerCount, 20),
		VBuff: buffer.NewVB(),
	}
}

// NodesPerElement returns nodes per element.
func (m *Hex20) NodesPerElement() int {
	return 20
}

// Add a finite element to mesh.
// Layer number and nodes are input.
// The node numbering should follow the convention of CalculiX.
// http://www.dhondt.de/ccx_2.20.pdf
func (m *Hex20) addFE(l int, nodes [20]v3.Vec) {
	indices := [20]uint32{}
	for n := 0; n < 20; n++ {
		indices[n] = m.addVertex(nodes[n])
	}
	m.IBuff.AddFE(l, indices[:])
}

func (m *Hex20) addVertex(vert v3.Vec) uint32 {
	return m.VBuff.Id(vert)
}

func (m *Hex20) vertexCount() int {
	return m.VBuff.VertexCount()
}

func (m *Hex20) vertex(i uint32) v3.Vec {
	return m.VBuff.Vertex(i)
}

// Number of layers along the Z axis.
func (m *Hex20) layerCount() int {
	return m.IBuff.LayerCount()
}

// Number of finite elements on a layer.
func (m *Hex20) feCountOnLayer(l int) int {
	return m.IBuff.FECountOnLayer(l)
}

// Number of finite elements for all layers.
func (m *Hex20) feCount() int {
	return m.IBuff.FECount()
}

// Get a finite element.
// Layer number is input.
// FE index on layer is input.
// FE index could be from 0 to number of tetrahedra on layer.
// Don't return error to increase performance.
func (m *Hex20) feIndicies(l, i int) []uint32 {
	return m.IBuff.FEIndicies(l, i)
}

// Get a finite element.
// Layer number is input.
// FE index on layer is input.
// FE index could be from 0 to number of tetrahedra on layer.
// Don't return error to increase performance.
func (m *Hex20) feVertices(l, i int) []v3.Vec {
	indices := m.IBuff.FEIndicies(l, i)
	vertices := make([]v3.Vec, 20)
	for n := 0; n < 20; n++ {
		vertices[n] = m.VBuff.Vertex(indices[n])
	}
	return vertices
}

// WriteInp saves mesh to ABAQUS or CalculiX `inp` file.
// Units of measurement are mm,N,s,K.
// Refer to https://engineering.stackexchange.com/q/54454/15178
func (m *Hex20) WriteInp(
	path string,
	layersFixed []int,
	massDensity float32,
	youngModulus float32,
	poissonRatio float32,
) error {
	return m.WriteInpLayers(path, 0, m.layerCount(), layersFixed, massDensity, youngModulus, poissonRatio)
}

// WriteInpLayers saves specific layers of mesh to ABAQUS or CalculiX `inp` file.
// Result would include start layer.
// Result would exclude end layer.
// Units of measurement are mm,N,s,K.
// Refer to https://engineering.stackexchange.com/q/54454/15178
func (m *Hex20) WriteInpLayers(
	path string,
	layerStart, layerEnd int,
	layersFixed []int,
	massDensity float32,
	youngModulus float32,
	poissonRatio float32,
) error {
	inp := NewInp(m, path, layerStart, layerEnd, layersFixed, massDensity, youngModulus, poissonRatio)
	return inp.Write()
}

//-----------------------------------------------------------------------------
