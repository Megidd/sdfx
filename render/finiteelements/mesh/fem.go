package mesh

import (
	"math"

	"github.com/deadsy/sdfx/render"
	"github.com/deadsy/sdfx/render/finiteelements/buffer"
	"github.com/deadsy/sdfx/sdf"
	v3 "github.com/deadsy/sdfx/vec/v3"
	"github.com/deadsy/sdfx/vec/v3i"
)

// Fem is a mesh of finite elements.
// A sophisticated data structure for mesh is required.
// The repeated nodes would be removed.
// The element connectivity would be created with unique nodes.
type Fem struct {
	// Index buffer.
	IBuff *buffer.IB
	// Vertex buffer.
	VBuff *buffer.VB
}

// NewFem returns a new mesh and number of its layers along Z-axis.
func NewFem(s sdf.SDF3, r render.RenderFE) (*Fem, int) {
	fes := render.ToFem(s, r)

	voxelLen, voxelDim, mins, maxs := r.Voxels(s)

	m := newFem(voxelLen, voxelDim, mins, maxs)

	// Fill out the mesh with finite elements.
	for _, fe := range fes {
		m.addFE(fe.X, fe.Y, fe.Z, fe.V)
	}

	defer m.VBuff.DestroyHashTable()

	return m, voxelLen.Z
}

func newFem(voxelLen v3i.Vec, voxelDim v3.Vec, mins, maxs []v3.Vec) *Fem {
	return &Fem{
		IBuff: buffer.NewIB(voxelLen, voxelDim, mins, maxs),
		VBuff: buffer.NewVB(),
	}
}

func (m *Fem) Size() (int, int, int) {
	return m.IBuff.Size()
}

// Add a finite element.
// Voxel coordinate and nodes are input.
// The node numbering should follow the convention of CalculiX.
// http://www.dhondt.de/ccx_2.20.pdf
func (m *Fem) addFE(x, y, z int, nodes []v3.Vec) {
	indices := make([]uint32, len(nodes))
	for n := 0; n < len(nodes); n++ {
		indices[n] = m.addVertex(nodes[n])
	}
	m.IBuff.AddFE(x, y, z, indices)
}

func (m *Fem) addVertex(vert v3.Vec) uint32 {
	return m.VBuff.Id(vert)
}

func (m *Fem) vertexCount() int {
	return m.VBuff.VertexCount()
}

func (m *Fem) vertex(i uint32) v3.Vec {
	return m.VBuff.Vertex(i)
}

// To iterate over all voxels and get elements inside each voxel and do stuff with them.
func (m *Fem) iterate(f func(int, int, int, []*buffer.Element)) {
	m.IBuff.Iterate(f)
}

// The closest node is identified.
// Also, the containing voxel is figured out.
//
// This logic has to be here, since we need access to any node vertex.
func (m *Fem) Locate(location v3.Vec) (uint32, v3i.Vec) {
	// Calculating voxel indices.
	idxX := int(math.Floor((location.X - m.IBuff.Grid.Voxels[0].Min.X) / (m.IBuff.Grid.Dim.X)))
	idxY := int(math.Floor((location.Y - m.IBuff.Grid.Voxels[0].Min.Y) / (m.IBuff.Grid.Dim.Y)))
	idxZ := int(math.Floor((location.Z - m.IBuff.Grid.Voxels[0].Min.Z) / (m.IBuff.Grid.Dim.Z)))

	// Ensure indices are within bounds
	if idxX >= m.IBuff.Grid.Len.X {
		idxX = m.IBuff.Grid.Len.X - 1
	}
	if idxY >= m.IBuff.Grid.Len.Y {
		idxY = m.IBuff.Grid.Len.Y - 1
	}
	if idxZ >= m.IBuff.Grid.Len.Z {
		idxZ = m.IBuff.Grid.Len.Z - 1
	}

	// Get elements in the voxel
	elements := m.IBuff.Grid.Get(idxX, idxY, idxZ)

	// Find the closest node
	var closestNode uint32
	minDistance := math.Inf(1)

	for _, element := range elements {
		for _, node := range element.Nodes {
			// A function that gives you the position of a node.
			nodePos := m.vertex(node)

			distance := location.Sub(nodePos).Length()
			if distance < minDistance {
				minDistance = distance
				closestNode = node
			}
		}
	}

	return closestNode, v3i.Vec{X: idxX, Y: idxY, Z: idxZ}
}

// Compute the bounding box of all the input points.
// Return all the voxels that are intersecting with that bounding box.
func (m *Fem) VoxelsIntersecting(points []v3.Vec) ([]v3i.Vec, v3.Vec, v3.Vec) {
	return m.IBuff.Grid.VoxelsIntersecting(points)
}

//-----------------------------------------------------------------------------

// Count separate components consisting of disconnected finite elements.
// They cause FEA solver to throw error.
func (m *Fem) CountComponents() int {
	return m.IBuff.Grid.VoxelComponents()
}

//-----------------------------------------------------------------------------

// WriteInp writes mesh to ABAQUS or CalculiX `inp` file.
func (m *Fem) WriteInp(
	path string,
	massDensity float32,
	youngModulus float32,
	poissonRatio float32,
	restraints []*Restraint,
	loads []*Load,
	gravityDirection v3.Vec,
	gravityMagnitude float64,
) error {
	_, _, layersZ := m.IBuff.Size()
	return m.WriteInpLayers(path, 0, layersZ, massDensity, youngModulus, poissonRatio, restraints, loads, gravityDirection, gravityMagnitude)
}

// WriteInpLayers writes specific layers of mesh to ABAQUS or CalculiX `inp` file.
// Result would include start layer.
// Result would exclude end layer.
func (m *Fem) WriteInpLayers(
	path string,
	layerStart, layerEnd int,
	massDensity float32,
	youngModulus float32,
	poissonRatio float32,
	restraints []*Restraint,
	loads []*Load,
	gravityDirection v3.Vec,
	gravityMagnitude float64,
) error {
	inp := NewInp(m, path, layerStart, layerEnd, massDensity, youngModulus, poissonRatio, restraints, loads, gravityDirection, gravityMagnitude)
	return inp.Write()
}

//-----------------------------------------------------------------------------
