package render

import (
	"fmt"
	"math"
	"sync"

	"github.com/deadsy/sdfx/sdf"
	"github.com/deadsy/sdfx/vec/conv"
	v3 "github.com/deadsy/sdfx/vec/v3"
	"github.com/deadsy/sdfx/vec/v3i"
)

//-----------------------------------------------------------------------------

// MarchingCubesFEUniform renders using marching cubes with uniform space sampling.
type MarchingCubesFEUniform struct {
	meshCells int // number of cells on the longest axis of bounding box. e.g 200
}

// NewMarchingCubesFEUniform returns a RenderHex8 object.
func NewMarchingCubesFEUniform(meshCells int) *MarchingCubesFEUniform {
	return &MarchingCubesFEUniform{
		meshCells: meshCells,
	}
}

// Info returns a string describing the rendered volume.
func (r *MarchingCubesFEUniform) Info(s sdf.SDF3) string {
	bb0 := s.BoundingBox()
	bb0Size := bb0.Size()
	meshInc := bb0Size.MaxComponent() / float64(r.meshCells)
	bb1Size := bb0Size.DivScalar(meshInc)
	bb1Size = bb1Size.Ceil().AddScalar(1)
	cells := conv.V3ToV3i(bb1Size)
	return fmt.Sprintf("%dx%dx%d", cells.X, cells.Y, cells.Z)
}

// To get the layer counts which are consistent with loops of marching algorithm.
func (r *MarchingCubesFEUniform) LayerCounts(s sdf.SDF3) (int, int, int) {
	bb0 := s.BoundingBox()
	bb0Size := bb0.Size()
	meshInc := bb0Size.MaxComponent() / float64(r.meshCells)
	bb1Size := bb0Size.DivScalar(meshInc)
	bb1Size = bb1Size.Ceil().AddScalar(1)
	bb1Size = bb1Size.MulScalar(meshInc)
	bb := sdf.NewBox3(bb0.Center(), bb1Size)
	size := bb.Size()
	steps := conv.V3ToV3i(size.DivScalar(meshInc).Ceil())
	return steps.X, steps.Y, steps.Z
}

// Render produces a finite elements mesh over the bounding volume of an sdf3.
// Finite elements are in the shape of tetrahedra.
func (r *MarchingCubesFEUniform) RenderTet4(s sdf.SDF3, output chan<- []*Tet4) {
	// work out the region we will sample
	bb0 := s.BoundingBox()
	bb0Size := bb0.Size()
	meshInc := bb0Size.MaxComponent() / float64(r.meshCells)
	bb1Size := bb0Size.DivScalar(meshInc)
	bb1Size = bb1Size.Ceil().AddScalar(1)
	bb1Size = bb1Size.MulScalar(meshInc)
	bb := sdf.NewBox3(bb0.Center(), bb1Size)
	output <- marchingCubesTet4(s, bb, meshInc)
}

// Render produces a finite elements mesh over the bounding volume of an sdf3.
// Finite elements are in the shape of tetrahedra.
func (r *MarchingCubesFEUniform) RenderTet10(s sdf.SDF3, output chan<- []*Tet10) {
	// work out the region we will sample
	bb0 := s.BoundingBox()
	bb0Size := bb0.Size()
	meshInc := bb0Size.MaxComponent() / float64(r.meshCells)
	bb1Size := bb0Size.DivScalar(meshInc)
	bb1Size = bb1Size.Ceil().AddScalar(1)
	bb1Size = bb1Size.MulScalar(meshInc)
	bb := sdf.NewBox3(bb0.Center(), bb1Size)
	output <- marchingCubesTet10(s, bb, meshInc)
}

// Render produces a finite elements mesh over the bounding volume of an sdf3.
// Finite elements are in the shape of hexahedra.
func (r *MarchingCubesFEUniform) RenderHex8(s sdf.SDF3, output chan<- []*Hex8) {
	// work out the region we will sample
	bb0 := s.BoundingBox()
	bb0Size := bb0.Size()
	meshInc := bb0Size.MaxComponent() / float64(r.meshCells)
	bb1Size := bb0Size.DivScalar(meshInc)
	bb1Size = bb1Size.Ceil().AddScalar(1)
	bb1Size = bb1Size.MulScalar(meshInc)
	bb := sdf.NewBox3(bb0.Center(), bb1Size)
	output <- marchingCubesHex8(s, bb, meshInc)
}

// Render produces a finite elements mesh over the bounding volume of an sdf3.
// Finite elements are in the shape of hexahedra.
func (r *MarchingCubesFEUniform) RenderHex20(s sdf.SDF3, output chan<- []*Hex20) {
	// work out the region we will sample
	bb0 := s.BoundingBox()
	bb0Size := bb0.Size()
	meshInc := bb0Size.MaxComponent() / float64(r.meshCells)
	bb1Size := bb0Size.DivScalar(meshInc)
	bb1Size = bb1Size.Ceil().AddScalar(1)
	bb1Size = bb1Size.MulScalar(meshInc)
	bb := sdf.NewBox3(bb0.Center(), bb1Size)
	output <- marchingCubesHex20(s, bb, meshInc)
}

//-----------------------------------------------------------------------------

type layerXY struct {
	base  v3.Vec    // base coordinate of layer
	inc   v3.Vec    // dx, dy, dz for each step
	steps v3i.Vec   // number of x,y,z steps
	val0  []float64 // SDF values for z layer
	val1  []float64 // SDF values for z + dz layer
}

func newLayerXY(base, inc v3.Vec, steps v3i.Vec) *layerXY {
	return &layerXY{base, inc, steps, nil, nil}
}

// Evaluate the SDF for a given XY layer
func (l *layerXY) Evaluate(s sdf.SDF3, z int) {

	// Swap the layers
	l.val0, l.val1 = l.val1, l.val0

	nx, ny := l.steps.X, l.steps.Y
	dx, dy, dz := l.inc.X, l.inc.Y, l.inc.Z

	// allocate storage
	if l.val1 == nil {
		l.val1 = make([]float64, (nx+1)*(ny+1))
	}

	// setup the loop variables
	var p v3.Vec
	p.Z = l.base.Z + float64(z)*dz

	// define the base struct for requesting evaluation
	eReq := evalReq{
		wg:  new(sync.WaitGroup),
		fn:  s.Evaluate,
		out: l.val1,
	}

	// evaluate the layer
	p.X = l.base.X

	// Performance doesn't seem to improve past 100.
	const batchSize = 100

	eReq.p = make([]v3.Vec, 0, batchSize)
	for x := 0; x < nx+1; x++ {
		p.Y = l.base.Y
		for y := 0; y < ny+1; y++ {
			eReq.p = append(eReq.p, p)
			if len(eReq.p) == batchSize {
				eReq.wg.Add(1)
				evalProcessCh <- eReq
				eReq.out = eReq.out[batchSize:]       // shift the output slice for processing
				eReq.p = make([]v3.Vec, 0, batchSize) // create a new slice for the next batch
			}
			p.Y += dy
		}
		p.X += dx
	}

	// send any remaining points for processing
	if len(eReq.p) > 0 {
		eReq.wg.Add(1)
		evalProcessCh <- eReq
	}

	// Wait for all processing to complete before returning
	eReq.wg.Wait()
}

func (l *layerXY) Get(x, y, z int) float64 {
	idz := x*(l.steps.Y+1) + y
	if z == 0 {
		return l.val0[idz]
	}
	return l.val1[idz]
}

//-----------------------------------------------------------------------------

// MATHEMATICA script is available here:
// https://math.stackexchange.com/a/4709610/197913
func isZeroVolume(a, b, c, d v3.Vec) (bool, float64) {
	ab := b.Sub(a)
	ac := c.Sub(a)
	ad := d.Sub(a)

	// Note that the `Norm` function of MATHEMATICA is equivalent to our `Length()` function.
	nab := ab.Length()
	ncd := d.Sub(c).Length()
	nbd := d.Sub(b).Length()
	nbc := c.Sub(b).Length()
	nac := ac.Length()
	nad := ad.Length()

	// Check for 0 edge lengths
	if nab == 0 || ncd == 0 ||
		nbd == 0 || nbc == 0 ||
		nac == 0 || nad == 0 {
		return true, 0
	}

	volume := 1.0 / 6.0 * math.Abs(ab.Cross(ac).Dot(ad))
	denom := (nab + ncd) * (nac + nbd) * (nad + nbc)

	// Tolerance derived from here:
	// https://math.stackexchange.com/a/4709610/197913
	tolerance := 480.0

	rho := tolerance * volume / denom

	return rho < 1, volume
}

//-----------------------------------------------------------------------------

// Check by four corner nodes of a 4-node or a 10-node tetrahedral element.
// A more complex method could be separately used for 10-node tetrahedral element.
// But let's keep things simple for now.
func isBad(a, b, c, d v3.Vec) (bool, float64) {
	// Coordinates of the nodes.
	var xl [3][4]float64

	xl[0][0] = a.X
	xl[1][0] = a.Y
	xl[2][0] = a.Z

	xl[0][1] = b.X
	xl[1][1] = b.Y
	xl[2][1] = b.Z

	xl[0][2] = c.X
	xl[1][2] = c.Y
	xl[2][2] = c.Z

	xl[0][3] = d.X
	xl[1][3] = d.Y
	xl[2][3] = d.Z

	// xi, et, and ze are the coordinates of the Gauss point
	// in the integration scheme for the 4-node tetrahedral element.
	// For this element type, there is typically only 1 Gauss point used,
	// which is located at the centroid of the tetrahedron.
	// The coordinates of this Gauss point are (xi, et, ze) = (1/4, 1/4, 1/4).
	var xi float64 = 0.25
	var et float64 = 0.25
	var ze float64 = 0.25

	// Shape functions.
	var shp [4][4]float64

	shp[3][0] = 1.0 - xi - et - ze
	shp[3][1] = xi
	shp[3][2] = et
	shp[3][3] = ze

	// local derivatives of the shape functions: xi-derivative

	shp[0][0] = -1.0
	shp[0][1] = 1.0
	shp[0][2] = 0.0
	shp[0][3] = 0.0

	// local derivatives of the shape functions: eta-derivative

	shp[1][0] = -1.0
	shp[1][1] = 0.0
	shp[1][2] = 1.0
	shp[1][3] = 0.0

	// local derivatives of the shape functions: zeta-derivative

	shp[2][0] = -1.0
	shp[2][1] = 0.0
	shp[2][2] = 0.0
	shp[2][3] = 1.0

	// computation of the local derivative of the global coordinates (xs)
	xs := [3][3]float64{}
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			xs[i][j] = 0.0
			for k := 0; k < 4; k++ {
				xs[i][j] += xl[i][k] * shp[j][k]
			}
		}
	}

	// computation of the jacobian determinant
	xsj := xs[0][0]*(xs[1][1]*xs[2][2]-xs[1][2]*xs[2][1]) -
		xs[0][1]*(xs[1][0]*xs[2][2]-xs[1][2]*xs[2][0]) +
		xs[0][2]*(xs[1][0]*xs[2][1]-xs[1][1]*xs[2][0])

	// According to CCX source code to detect nonpositive jacobian determinant in element
	return xsj < 1e-20, xsj
}

//-----------------------------------------------------------------------------
