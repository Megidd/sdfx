//-----------------------------------------------------------------------------
/*

Marching Cubes

Convert an SDF3 to a triangle mesh.

*/
//-----------------------------------------------------------------------------

package render

import (
	"fmt"
	"math"
	"runtime"
	"sync"

	"github.com/deadsy/sdfx/sdf"
	"github.com/deadsy/sdfx/vec/conv"
	v3 "github.com/deadsy/sdfx/vec/v3"
	"github.com/deadsy/sdfx/vec/v3i"
)

//-----------------------------------------------------------------------------

// evalReq is used for processing evaluations in parallel.
// A slice of V3 is evaluated with fn, the result is stored in out.
type evalReq struct {
	out []float64
	p   []v3.Vec
	fn  func(v3.Vec) float64
	wg  *sync.WaitGroup
}

var evalProcessCh = make(chan evalReq, 100)

// evalRoutines starts a set of concurrent evaluation routines.
func evalRoutines() {
	for i := 0; i < runtime.NumCPU(); i++ {
		go func() {
			var i int
			var p v3.Vec
			for r := range evalProcessCh {
				for i, p = range r.p {
					r.out[i] = r.fn(p)
				}
				r.wg.Done()
			}
		}()
	}
}

//-----------------------------------------------------------------------------

type layerYZ struct {
	base  v3.Vec    // base coordinate of layer
	inc   v3.Vec    // dx, dy, dz for each step
	steps v3i.Vec   // number of x,y,z steps
	val0  []float64 // SDF values for x layer
	val1  []float64 // SDF values for x + dx layer
}

func newLayerYZ(base, inc v3.Vec, steps v3i.Vec) *layerYZ {
	return &layerYZ{base, inc, steps, nil, nil}
}

// Evaluate the SDF for a given XY layer
func (l *layerYZ) Evaluate(s sdf.SDF3, x int) {

	// Swap the layers
	l.val0, l.val1 = l.val1, l.val0

	ny, nz := l.steps.Y, l.steps.Z
	dx, dy, dz := l.inc.X, l.inc.Y, l.inc.Z

	// allocate storage
	if l.val1 == nil {
		l.val1 = make([]float64, (ny+1)*(nz+1))
	}

	// setup the loop variables
	var p v3.Vec
	p.X = l.base.X + float64(x)*dx

	// define the base struct for requesting evaluation
	eReq := evalReq{
		wg:  new(sync.WaitGroup),
		fn:  s.Evaluate,
		out: l.val1,
	}

	// evaluate the layer
	p.Y = l.base.Y

	// Performance doesn't seem to improve past 100.
	const batchSize = 100

	eReq.p = make([]v3.Vec, 0, batchSize)
	for y := 0; y < ny+1; y++ {
		p.Z = l.base.Z
		for z := 0; z < nz+1; z++ {
			eReq.p = append(eReq.p, p)
			if len(eReq.p) == batchSize {
				eReq.wg.Add(1)
				evalProcessCh <- eReq
				eReq.out = eReq.out[batchSize:]       // shift the output slice for processing
				eReq.p = make([]v3.Vec, 0, batchSize) // create a new slice for the next batch
			}
			p.Z += dz
		}
		p.Y += dy
	}

	// send any remaining points for processing
	if len(eReq.p) > 0 {
		eReq.wg.Add(1)
		evalProcessCh <- eReq
	}

	// Wait for all processing to complete before returning
	eReq.wg.Wait()
}

func (l *layerYZ) Get(x, y, z int) float64 {
	idx := y*(l.steps.Z+1) + z
	if x == 0 {
		return l.val0[idx]
	}
	return l.val1[idx]
}

//-----------------------------------------------------------------------------

func marchingCubes(s sdf.SDF3, box sdf.Box3, step float64) []*Triangle3 {

	var triangles []*Triangle3
	size := box.Size()
	base := box.Min
	steps := conv.V3ToV3i(size.DivScalar(step).Ceil())
	inc := size.Div(conv.V3iToV3(steps))

	// start the evaluation routines
	evalRoutines()

	// create the SDF layer cache
	l := newLayerYZ(base, inc, steps)
	// evaluate the SDF for x = 0
	l.Evaluate(s, 0)

	nx, ny, nz := steps.X, steps.Y, steps.Z
	dx, dy, dz := inc.X, inc.Y, inc.Z

	var p v3.Vec
	p.X = base.X
	for x := 0; x < nx; x++ {
		// read the x + 1 layer
		l.Evaluate(s, x+1)
		// process all cubes in the x and x + 1 layers
		p.Y = base.Y
		for y := 0; y < ny; y++ {
			p.Z = base.Z
			for z := 0; z < nz; z++ {
				x0, y0, z0 := p.X, p.Y, p.Z
				x1, y1, z1 := x0+dx, y0+dy, z0+dz
				corners := [8]v3.Vec{
					{x0, y0, z0},
					{x1, y0, z0},
					{x1, y1, z0},
					{x0, y1, z0},
					{x0, y0, z1},
					{x1, y0, z1},
					{x1, y1, z1},
					{x0, y1, z1}}
				values := [8]float64{
					l.Get(0, y, z),
					l.Get(1, y, z),
					l.Get(1, y+1, z),
					l.Get(0, y+1, z),
					l.Get(0, y, z+1),
					l.Get(1, y, z+1),
					l.Get(1, y+1, z+1),
					l.Get(0, y+1, z+1)}
				triangles = append(triangles, mcToTriangles(corners, values, 0)...)
				p.Z += dz
			}
			p.Y += dy
		}
		p.X += dx
	}

	return triangles
}

//-----------------------------------------------------------------------------

func mcToTriangles(p [8]v3.Vec, v [8]float64, x float64) []*Triangle3 {
	// which of the 0..255 patterns do we have?
	index := 0
	for i := 0; i < 8; i++ {
		if v[i] < x {
			index |= 1 << uint(i)
		}
	}
	// do we have any triangles to create?
	if mcEdgeTable[index] == 0 {
		return nil
	}
	// work out the interpolated points on the edges
	var points [12]v3.Vec
	for i := 0; i < 12; i++ {
		bit := 1 << uint(i)
		if mcEdgeTable[index]&bit != 0 {
			a := mcPairTable[i][0]
			b := mcPairTable[i][1]
			points[i] = mcInterpolate(p[a], p[b], v[a], v[b], x)
		}
	}
	// create the triangles
	table := mcTriangleTable[index]
	count := len(table) / 3
	result := make([]*Triangle3, 0, count)
	for i := 0; i < count; i++ {
		t := Triangle3{}
		t.V[2] = points[table[i*3+0]]
		t.V[1] = points[table[i*3+1]]
		t.V[0] = points[table[i*3+2]]
		if !t.Degenerate(0) {
			result = append(result, &t)
		}
	}
	return result
}

//-----------------------------------------------------------------------------

func mcInterpolate(p1, p2 v3.Vec, v1, v2, x float64) v3.Vec {

	closeToV1 := math.Abs(x-v1) < epsilon
	closeToV2 := math.Abs(x-v2) < epsilon

	if closeToV1 && !closeToV2 {
		return p1
	}
	if closeToV2 && !closeToV1 {
		return p2
	}

	var t float64

	if closeToV1 && closeToV2 {
		// Pick the half way point
		t = 0.5
	} else {
		// linear interpolation
		t = (x - v1) / (v2 - v1)
	}

	return v3.Vec{
		p1.X + t*(p2.X-p1.X),
		p1.Y + t*(p2.Y-p1.Y),
		p1.Z + t*(p2.Z-p1.Z),
	}
}

//-----------------------------------------------------------------------------

// MarchingCubesUniform renders using marching cubes with uniform space sampling.
type MarchingCubesUniform struct {
	meshCells int // number of cells on the longest axis of bounding box. e.g 200
}

// NewMarchingCubesUniform returns a Render3 object.
func NewMarchingCubesUniform(meshCells int) *MarchingCubesUniform {
	return &MarchingCubesUniform{
		meshCells: meshCells,
	}
}

// Info returns a string describing the rendered volume.
func (r *MarchingCubesUniform) Info(s sdf.SDF3) string {
	bb0 := s.BoundingBox()
	bb0Size := bb0.Size()
	meshInc := bb0Size.MaxComponent() / float64(r.meshCells)
	bb1Size := bb0Size.DivScalar(meshInc)
	bb1Size = bb1Size.Ceil().AddScalar(1)
	cells := conv.V3ToV3i(bb1Size)
	return fmt.Sprintf("%dx%dx%d", cells.X, cells.Y, cells.Z)
}

// Render produces a 3d triangle mesh over the bounding volume of an sdf3.
func (r *MarchingCubesUniform) Render(s sdf.SDF3, output chan<- []*Triangle3) {
	// work out the region we will sample
	bb0 := s.BoundingBox()
	bb0Size := bb0.Size()
	meshInc := bb0Size.MaxComponent() / float64(r.meshCells)
	bb1Size := bb0Size.DivScalar(meshInc)
	bb1Size = bb1Size.Ceil().AddScalar(1)
	bb1Size = bb1Size.MulScalar(meshInc)
	bb := sdf.NewBox3(bb0.Center(), bb1Size)
	output <- marchingCubes(s, bb, meshInc)
}

//-----------------------------------------------------------------------------

// These are the vertex pairs for the edges
var mcPairTable = [12][2]int{
	{0, 1}, // 0
	{1, 2}, // 1
	{2, 3}, // 2
	{3, 0}, // 3
	{4, 5}, // 4
	{5, 6}, // 5
	{6, 7}, // 6
	{7, 4}, // 7
	{0, 4}, // 8
	{1, 5}, // 9
	{2, 6}, // 10
	{3, 7}, // 11
}

// 8 vertices -> 256 possible inside/outside combinations
// A 1 bit in the value indicates an edge with a line end point.
// 12 edges -> 12 bit values, note the fwd/rev symmetry
var mcEdgeTable = [256]int{
	0x0000, 0x0109, 0x0203, 0x030a, 0x0406, 0x050f, 0x0605, 0x070c,
	0x080c, 0x0905, 0x0a0f, 0x0b06, 0x0c0a, 0x0d03, 0x0e09, 0x0f00,
	0x0190, 0x0099, 0x0393, 0x029a, 0x0596, 0x049f, 0x0795, 0x069c,
	0x099c, 0x0895, 0x0b9f, 0x0a96, 0x0d9a, 0x0c93, 0x0f99, 0x0e90,
	0x0230, 0x0339, 0x0033, 0x013a, 0x0636, 0x073f, 0x0435, 0x053c,
	0x0a3c, 0x0b35, 0x083f, 0x0936, 0x0e3a, 0x0f33, 0x0c39, 0x0d30,
	0x03a0, 0x02a9, 0x01a3, 0x00aa, 0x07a6, 0x06af, 0x05a5, 0x04ac,
	0x0bac, 0x0aa5, 0x09af, 0x08a6, 0x0faa, 0x0ea3, 0x0da9, 0x0ca0,
	0x0460, 0x0569, 0x0663, 0x076a, 0x0066, 0x016f, 0x0265, 0x036c,
	0x0c6c, 0x0d65, 0x0e6f, 0x0f66, 0x086a, 0x0963, 0x0a69, 0x0b60,
	0x05f0, 0x04f9, 0x07f3, 0x06fa, 0x01f6, 0x00ff, 0x03f5, 0x02fc,
	0x0dfc, 0x0cf5, 0x0fff, 0x0ef6, 0x09fa, 0x08f3, 0x0bf9, 0x0af0,
	0x0650, 0x0759, 0x0453, 0x055a, 0x0256, 0x035f, 0x0055, 0x015c,
	0x0e5c, 0x0f55, 0x0c5f, 0x0d56, 0x0a5a, 0x0b53, 0x0859, 0x0950,
	0x07c0, 0x06c9, 0x05c3, 0x04ca, 0x03c6, 0x02cf, 0x01c5, 0x00cc,
	0x0fcc, 0x0ec5, 0x0dcf, 0x0cc6, 0x0bca, 0x0ac3, 0x09c9, 0x08c0,
	0x08c0, 0x09c9, 0x0ac3, 0x0bca, 0x0cc6, 0x0dcf, 0x0ec5, 0x0fcc,
	0x00cc, 0x01c5, 0x02cf, 0x03c6, 0x04ca, 0x05c3, 0x06c9, 0x07c0,
	0x0950, 0x0859, 0x0b53, 0x0a5a, 0x0d56, 0x0c5f, 0x0f55, 0x0e5c,
	0x015c, 0x0055, 0x035f, 0x0256, 0x055a, 0x0453, 0x0759, 0x0650,
	0x0af0, 0x0bf9, 0x08f3, 0x09fa, 0x0ef6, 0x0fff, 0x0cf5, 0x0dfc,
	0x02fc, 0x03f5, 0x00ff, 0x01f6, 0x06fa, 0x07f3, 0x04f9, 0x05f0,
	0x0b60, 0x0a69, 0x0963, 0x086a, 0x0f66, 0x0e6f, 0x0d65, 0x0c6c,
	0x036c, 0x0265, 0x016f, 0x0066, 0x076a, 0x0663, 0x0569, 0x0460,
	0x0ca0, 0x0da9, 0x0ea3, 0x0faa, 0x08a6, 0x09af, 0x0aa5, 0x0bac,
	0x04ac, 0x05a5, 0x06af, 0x07a6, 0x00aa, 0x01a3, 0x02a9, 0x03a0,
	0x0d30, 0x0c39, 0x0f33, 0x0e3a, 0x0936, 0x083f, 0x0b35, 0x0a3c,
	0x053c, 0x0435, 0x073f, 0x0636, 0x013a, 0x0033, 0x0339, 0x0230,
	0x0e90, 0x0f99, 0x0c93, 0x0d9a, 0x0a96, 0x0b9f, 0x0895, 0x099c,
	0x069c, 0x0795, 0x049f, 0x0596, 0x029a, 0x0393, 0x0099, 0x0190,
	0x0f00, 0x0e09, 0x0d03, 0x0c0a, 0x0b06, 0x0a0f, 0x0905, 0x080c,
	0x070c, 0x0605, 0x050f, 0x0406, 0x030a, 0x0203, 0x0109, 0x0000,
}

// specify the edges used to create the triangle(s)
var mcTriangleTable = [256][]int{
	// 0b00000000 case 0
	{},
	// 0b00000001 case 1
	{0, 8, 3},
	// 0b00000010 case 2
	{0, 1, 9},
	// 0b00000011 case 3
	{1, 8, 3, 9, 8, 1},
	// 0b00000100 case 4
	{1, 2, 10},
	// 0b00000101 case 5
	{0, 8, 3, 1, 2, 10},
	// 0b00000110 case 6
	{9, 2, 10, 0, 2, 9},
	// 0b00000111 case 7
	{2, 8, 3, 2, 10, 8, 10, 9, 8},
	// 0b00001000 case 8
	{3, 11, 2},
	// 0b00001001 case 9
	{0, 11, 2, 8, 11, 0},
	// 0b00001010 case 10
	{1, 9, 0, 2, 3, 11},
	// 0b00001011 case 11
	{1, 11, 2, 1, 9, 11, 9, 8, 11},
	// 0b00001100 case 12
	{3, 10, 1, 11, 10, 3},
	// 0b00001101 case 13
	{0, 10, 1, 0, 8, 10, 8, 11, 10},
	// 0b00001110 case 14
	{3, 9, 0, 3, 11, 9, 11, 10, 9},
	// 0b00001111 case 15
	{9, 8, 10, 10, 8, 11},
	// 0b00010000 case 16
	{4, 7, 8},
	// 0b00010001 case 17
	{4, 3, 0, 7, 3, 4},
	// 0b00010010 case 18
	{0, 1, 9, 8, 4, 7},
	// 0b00010011 case 19
	{4, 1, 9, 4, 7, 1, 7, 3, 1},
	// 0b00010100 case 20
	{1, 2, 10, 8, 4, 7},
	// 0b00010101 case 21
	{3, 4, 7, 3, 0, 4, 1, 2, 10},
	// 0b00010110 case 22
	{9, 2, 10, 9, 0, 2, 8, 4, 7},
	// 0b00010111 case 23
	{2, 10, 9, 2, 9, 7, 2, 7, 3, 7, 9, 4},
	// 0b00011000 case 24
	{8, 4, 7, 3, 11, 2},
	// 0b00011001 case 25
	{11, 4, 7, 11, 2, 4, 2, 0, 4},
	// 0b00011010 case 26
	{9, 0, 1, 8, 4, 7, 2, 3, 11},
	// 0b00011011 case 27
	{4, 7, 11, 9, 4, 11, 9, 11, 2, 9, 2, 1},
	// 0b00011100 case 28
	{3, 10, 1, 3, 11, 10, 7, 8, 4},
	// 0b00011101 case 29
	{1, 11, 10, 1, 4, 11, 1, 0, 4, 7, 11, 4},
	// 0b00011110 case 30
	{4, 7, 8, 9, 0, 11, 9, 11, 10, 11, 0, 3},
	// 0b00011111 case 31
	{4, 7, 11, 4, 11, 9, 9, 11, 10},
	// 0b00100000 case 32
	{9, 5, 4},
	// 0b00100001 case 33
	{9, 5, 4, 0, 8, 3},
	// 0b00100010 case 34
	{0, 5, 4, 1, 5, 0},
	// 0b00100011 case 35
	{8, 5, 4, 8, 3, 5, 3, 1, 5},
	// 0b00100100 case 36
	{1, 2, 10, 9, 5, 4},
	// 0b00100101 case 37
	{3, 0, 8, 1, 2, 10, 4, 9, 5},
	// 0b00100110 case 38
	{5, 2, 10, 5, 4, 2, 4, 0, 2},
	// 0b00100111 case 39
	{2, 10, 5, 3, 2, 5, 3, 5, 4, 3, 4, 8},
	// 0b00101000 case 40
	{9, 5, 4, 2, 3, 11},
	// 0b00101001 case 41
	{0, 11, 2, 0, 8, 11, 4, 9, 5},
	// 0b00101010 case 42
	{0, 5, 4, 0, 1, 5, 2, 3, 11},
	// 0b00101011 case 43
	{2, 1, 5, 2, 5, 8, 2, 8, 11, 4, 8, 5},
	// 0b00101100 case 44
	{10, 3, 11, 10, 1, 3, 9, 5, 4},
	// 0b00101101 case 45
	{4, 9, 5, 0, 8, 1, 8, 10, 1, 8, 11, 10},
	// 0b00101110 case 46
	{5, 4, 0, 5, 0, 11, 5, 11, 10, 11, 0, 3},
	// 0b00101111 case 47
	{5, 4, 8, 5, 8, 10, 10, 8, 11},
	// 0b00110000 case 48
	{9, 7, 8, 5, 7, 9},
	// 0b00110001 case 49
	{9, 3, 0, 9, 5, 3, 5, 7, 3},
	// 0b00110010 case 50
	{0, 7, 8, 0, 1, 7, 1, 5, 7},
	// 0b00110011 case 51
	{1, 5, 3, 3, 5, 7},
	// 0b00110100 case 52
	{9, 7, 8, 9, 5, 7, 10, 1, 2},
	// 0b00110101 case 53
	{10, 1, 2, 9, 5, 0, 5, 3, 0, 5, 7, 3},
	// 0b00110110 case 54
	{8, 0, 2, 8, 2, 5, 8, 5, 7, 10, 5, 2},
	// 0b00110111 case 55
	{2, 10, 5, 2, 5, 3, 3, 5, 7},
	// 0b00111000 case 56
	{7, 9, 5, 7, 8, 9, 3, 11, 2},
	// 0b00111001 case 57
	{9, 5, 7, 9, 7, 2, 9, 2, 0, 2, 7, 11},
	// 0b00111010 case 58
	{2, 3, 11, 0, 1, 8, 1, 7, 8, 1, 5, 7},
	// 0b00111011 case 59
	{11, 2, 1, 11, 1, 7, 7, 1, 5},
	// 0b00111100 case 60
	{9, 5, 8, 8, 5, 7, 10, 1, 3, 10, 3, 11},
	// 0b00111101 case 61
	{5, 7, 0, 5, 0, 9, 7, 11, 0, 1, 0, 10, 11, 10, 0},
	// 0b00111110 case 62
	{11, 10, 0, 11, 0, 3, 10, 5, 0, 8, 0, 7, 5, 7, 0},
	// 0b00111111 case 63
	{11, 10, 5, 7, 11, 5},
	// 0b01000000 case 64
	{10, 6, 5},
	// 0b01000001 case 65
	{0, 8, 3, 5, 10, 6},
	// 0b01000010 case 66
	{9, 0, 1, 5, 10, 6},
	// 0b01000011 case 67
	{1, 8, 3, 1, 9, 8, 5, 10, 6},
	// 0b01000100 case 68
	{1, 6, 5, 2, 6, 1},
	// 0b01000101 case 69
	{1, 6, 5, 1, 2, 6, 3, 0, 8},
	// 0b01000110 case 70
	{9, 6, 5, 9, 0, 6, 0, 2, 6},
	// 0b01000111 case 71
	{5, 9, 8, 5, 8, 2, 5, 2, 6, 3, 2, 8},
	// 0b01001000 case 72
	{2, 3, 11, 10, 6, 5},
	// 0b01001001 case 73
	{11, 0, 8, 11, 2, 0, 10, 6, 5},
	// 0b01001010 case 74
	{0, 1, 9, 2, 3, 11, 5, 10, 6},
	// 0b01001011 case 75
	{5, 10, 6, 1, 9, 2, 9, 11, 2, 9, 8, 11},
	// 0b01001100 case 76
	{6, 3, 11, 6, 5, 3, 5, 1, 3},
	// 0b01001101 case 77
	{0, 8, 11, 0, 11, 5, 0, 5, 1, 5, 11, 6},
	// 0b01001110 case 78
	{3, 11, 6, 0, 3, 6, 0, 6, 5, 0, 5, 9},
	// 0b01001111 case 79
	{6, 5, 9, 6, 9, 11, 11, 9, 8},
	// 0b01010000 case 80
	{5, 10, 6, 4, 7, 8},
	// 0b01010001 case 81
	{4, 3, 0, 4, 7, 3, 6, 5, 10},
	// 0b01010010 case 82
	{1, 9, 0, 5, 10, 6, 8, 4, 7},
	// 0b01010011 case 83
	{10, 6, 5, 1, 9, 7, 1, 7, 3, 7, 9, 4},
	// 0b01010100 case 84
	{6, 1, 2, 6, 5, 1, 4, 7, 8},
	// 0b01010101 case 85
	{1, 2, 5, 5, 2, 6, 3, 0, 4, 3, 4, 7},
	// 0b01010110 case 86
	{8, 4, 7, 9, 0, 5, 0, 6, 5, 0, 2, 6},
	// 0b01010111 case 87
	{7, 3, 9, 7, 9, 4, 3, 2, 9, 5, 9, 6, 2, 6, 9},
	// 0b01011000 case 88
	{3, 11, 2, 7, 8, 4, 10, 6, 5},
	// 0b01011001 case 89
	{5, 10, 6, 4, 7, 2, 4, 2, 0, 2, 7, 11},
	// 0b01011010 case 90
	{0, 1, 9, 4, 7, 8, 2, 3, 11, 5, 10, 6},
	// 0b01011011 case 91
	{9, 2, 1, 9, 11, 2, 9, 4, 11, 7, 11, 4, 5, 10, 6},
	// 0b01011100 case 92
	{8, 4, 7, 3, 11, 5, 3, 5, 1, 5, 11, 6},
	// 0b01011101 case 93
	{5, 1, 11, 5, 11, 6, 1, 0, 11, 7, 11, 4, 0, 4, 11},
	// 0b01011110 case 94
	{0, 5, 9, 0, 6, 5, 0, 3, 6, 11, 6, 3, 8, 4, 7},
	// 0b01011111 case 95
	{6, 5, 9, 6, 9, 11, 4, 7, 9, 7, 11, 9},
	// 0b01100000 case 96
	{10, 4, 9, 6, 4, 10},
	// 0b01100001 case 97
	{4, 10, 6, 4, 9, 10, 0, 8, 3},
	// 0b01100010 case 98
	{10, 0, 1, 10, 6, 0, 6, 4, 0},
	// 0b01100011 case 99
	{8, 3, 1, 8, 1, 6, 8, 6, 4, 6, 1, 10},
	// 0b01100100 case 100
	{1, 4, 9, 1, 2, 4, 2, 6, 4},
	// 0b01100101 case 101
	{3, 0, 8, 1, 2, 9, 2, 4, 9, 2, 6, 4},
	// 0b01100110 case 102
	{0, 2, 4, 4, 2, 6},
	// 0b01100111 case 103
	{8, 3, 2, 8, 2, 4, 4, 2, 6},
	// 0b01101000 case 104
	{10, 4, 9, 10, 6, 4, 11, 2, 3},
	// 0b01101001 case 105
	{0, 8, 2, 2, 8, 11, 4, 9, 10, 4, 10, 6},
	// 0b01101010 case 106
	{3, 11, 2, 0, 1, 6, 0, 6, 4, 6, 1, 10},
	// 0b01101011 case 107
	{6, 4, 1, 6, 1, 10, 4, 8, 1, 2, 1, 11, 8, 11, 1},
	// 0b01101100 case 108
	{9, 6, 4, 9, 3, 6, 9, 1, 3, 11, 6, 3},
	// 0b01101101 case 109
	{8, 11, 1, 8, 1, 0, 11, 6, 1, 9, 1, 4, 6, 4, 1},
	// 0b01101110 case 110
	{3, 11, 6, 3, 6, 0, 0, 6, 4},
	{6, 4, 8, 11, 6, 8},
	{7, 10, 6, 7, 8, 10, 8, 9, 10},
	{0, 7, 3, 0, 10, 7, 0, 9, 10, 6, 7, 10},
	{10, 6, 7, 1, 10, 7, 1, 7, 8, 1, 8, 0},
	{10, 6, 7, 10, 7, 1, 1, 7, 3},
	{1, 2, 6, 1, 6, 8, 1, 8, 9, 8, 6, 7},
	{2, 6, 9, 2, 9, 1, 6, 7, 9, 0, 9, 3, 7, 3, 9},
	{7, 8, 0, 7, 0, 6, 6, 0, 2},
	{7, 3, 2, 6, 7, 2},
	{2, 3, 11, 10, 6, 8, 10, 8, 9, 8, 6, 7},
	{2, 0, 7, 2, 7, 11, 0, 9, 7, 6, 7, 10, 9, 10, 7},
	{1, 8, 0, 1, 7, 8, 1, 10, 7, 6, 7, 10, 2, 3, 11},
	{11, 2, 1, 11, 1, 7, 10, 6, 1, 6, 7, 1},
	{8, 9, 6, 8, 6, 7, 9, 1, 6, 11, 6, 3, 1, 3, 6},
	{0, 9, 1, 11, 6, 7},
	{7, 8, 0, 7, 0, 6, 3, 11, 0, 11, 6, 0},
	{7, 11, 6},
	{7, 6, 11},
	{3, 0, 8, 11, 7, 6},
	{0, 1, 9, 11, 7, 6},
	{8, 1, 9, 8, 3, 1, 11, 7, 6},
	{10, 1, 2, 6, 11, 7},
	{1, 2, 10, 3, 0, 8, 6, 11, 7},
	{2, 9, 0, 2, 10, 9, 6, 11, 7},
	{6, 11, 7, 2, 10, 3, 10, 8, 3, 10, 9, 8},
	{7, 2, 3, 6, 2, 7},
	{7, 0, 8, 7, 6, 0, 6, 2, 0},
	{2, 7, 6, 2, 3, 7, 0, 1, 9},
	{1, 6, 2, 1, 8, 6, 1, 9, 8, 8, 7, 6},
	{10, 7, 6, 10, 1, 7, 1, 3, 7},
	{10, 7, 6, 1, 7, 10, 1, 8, 7, 1, 0, 8},
	{0, 3, 7, 0, 7, 10, 0, 10, 9, 6, 10, 7},
	{7, 6, 10, 7, 10, 8, 8, 10, 9},
	{6, 8, 4, 11, 8, 6},
	{3, 6, 11, 3, 0, 6, 0, 4, 6},
	{8, 6, 11, 8, 4, 6, 9, 0, 1},
	{9, 4, 6, 9, 6, 3, 9, 3, 1, 11, 3, 6},
	{6, 8, 4, 6, 11, 8, 2, 10, 1},
	{1, 2, 10, 3, 0, 11, 0, 6, 11, 0, 4, 6},
	{4, 11, 8, 4, 6, 11, 0, 2, 9, 2, 10, 9},
	{10, 9, 3, 10, 3, 2, 9, 4, 3, 11, 3, 6, 4, 6, 3},
	{8, 2, 3, 8, 4, 2, 4, 6, 2},
	{0, 4, 2, 4, 6, 2},
	{1, 9, 0, 2, 3, 4, 2, 4, 6, 4, 3, 8},
	{1, 9, 4, 1, 4, 2, 2, 4, 6},
	{8, 1, 3, 8, 6, 1, 8, 4, 6, 6, 10, 1},
	{10, 1, 0, 10, 0, 6, 6, 0, 4},
	{4, 6, 3, 4, 3, 8, 6, 10, 3, 0, 3, 9, 10, 9, 3},
	{10, 9, 4, 6, 10, 4},
	{4, 9, 5, 7, 6, 11},
	{0, 8, 3, 4, 9, 5, 11, 7, 6},
	{5, 0, 1, 5, 4, 0, 7, 6, 11},
	{11, 7, 6, 8, 3, 4, 3, 5, 4, 3, 1, 5},
	{9, 5, 4, 10, 1, 2, 7, 6, 11},
	{6, 11, 7, 1, 2, 10, 0, 8, 3, 4, 9, 5},
	{7, 6, 11, 5, 4, 10, 4, 2, 10, 4, 0, 2},
	{3, 4, 8, 3, 5, 4, 3, 2, 5, 10, 5, 2, 11, 7, 6},
	{7, 2, 3, 7, 6, 2, 5, 4, 9},
	{9, 5, 4, 0, 8, 6, 0, 6, 2, 6, 8, 7},
	{3, 6, 2, 3, 7, 6, 1, 5, 0, 5, 4, 0},
	{6, 2, 8, 6, 8, 7, 2, 1, 8, 4, 8, 5, 1, 5, 8},
	{9, 5, 4, 10, 1, 6, 1, 7, 6, 1, 3, 7},
	{1, 6, 10, 1, 7, 6, 1, 0, 7, 8, 7, 0, 9, 5, 4},
	{4, 0, 10, 4, 10, 5, 0, 3, 10, 6, 10, 7, 3, 7, 10},
	{7, 6, 10, 7, 10, 8, 5, 4, 10, 4, 8, 10},
	{6, 9, 5, 6, 11, 9, 11, 8, 9},
	{3, 6, 11, 0, 6, 3, 0, 5, 6, 0, 9, 5},
	{0, 11, 8, 0, 5, 11, 0, 1, 5, 5, 6, 11},
	{6, 11, 3, 6, 3, 5, 5, 3, 1},
	{1, 2, 10, 9, 5, 11, 9, 11, 8, 11, 5, 6},
	{0, 11, 3, 0, 6, 11, 0, 9, 6, 5, 6, 9, 1, 2, 10},
	{11, 8, 5, 11, 5, 6, 8, 0, 5, 10, 5, 2, 0, 2, 5},
	{6, 11, 3, 6, 3, 5, 2, 10, 3, 10, 5, 3},
	{5, 8, 9, 5, 2, 8, 5, 6, 2, 3, 8, 2},
	{9, 5, 6, 9, 6, 0, 0, 6, 2},
	{1, 5, 8, 1, 8, 0, 5, 6, 8, 3, 8, 2, 6, 2, 8},
	{1, 5, 6, 2, 1, 6},
	{1, 3, 6, 1, 6, 10, 3, 8, 6, 5, 6, 9, 8, 9, 6},
	{10, 1, 0, 10, 0, 6, 9, 5, 0, 5, 6, 0},
	{0, 3, 8, 5, 6, 10},
	{10, 5, 6},
	{11, 5, 10, 7, 5, 11},
	{11, 5, 10, 11, 7, 5, 8, 3, 0},
	{5, 11, 7, 5, 10, 11, 1, 9, 0},
	{10, 7, 5, 10, 11, 7, 9, 8, 1, 8, 3, 1},
	{11, 1, 2, 11, 7, 1, 7, 5, 1},
	{0, 8, 3, 1, 2, 7, 1, 7, 5, 7, 2, 11},
	{9, 7, 5, 9, 2, 7, 9, 0, 2, 2, 11, 7},
	{7, 5, 2, 7, 2, 11, 5, 9, 2, 3, 2, 8, 9, 8, 2},
	{2, 5, 10, 2, 3, 5, 3, 7, 5},
	{8, 2, 0, 8, 5, 2, 8, 7, 5, 10, 2, 5},
	{9, 0, 1, 5, 10, 3, 5, 3, 7, 3, 10, 2},
	{9, 8, 2, 9, 2, 1, 8, 7, 2, 10, 2, 5, 7, 5, 2},
	{1, 3, 5, 3, 7, 5},
	{0, 8, 7, 0, 7, 1, 1, 7, 5},
	{9, 0, 3, 9, 3, 5, 5, 3, 7},
	{9, 8, 7, 5, 9, 7},
	{5, 8, 4, 5, 10, 8, 10, 11, 8},
	{5, 0, 4, 5, 11, 0, 5, 10, 11, 11, 3, 0},
	{0, 1, 9, 8, 4, 10, 8, 10, 11, 10, 4, 5},
	{10, 11, 4, 10, 4, 5, 11, 3, 4, 9, 4, 1, 3, 1, 4},
	{2, 5, 1, 2, 8, 5, 2, 11, 8, 4, 5, 8},
	{0, 4, 11, 0, 11, 3, 4, 5, 11, 2, 11, 1, 5, 1, 11},
	{0, 2, 5, 0, 5, 9, 2, 11, 5, 4, 5, 8, 11, 8, 5},
	{9, 4, 5, 2, 11, 3},
	{2, 5, 10, 3, 5, 2, 3, 4, 5, 3, 8, 4},
	{5, 10, 2, 5, 2, 4, 4, 2, 0},
	{3, 10, 2, 3, 5, 10, 3, 8, 5, 4, 5, 8, 0, 1, 9},
	{5, 10, 2, 5, 2, 4, 1, 9, 2, 9, 4, 2},
	{8, 4, 5, 8, 5, 3, 3, 5, 1},
	{0, 4, 5, 1, 0, 5},
	{8, 4, 5, 8, 5, 3, 9, 0, 5, 0, 3, 5},
	{9, 4, 5},
	{4, 11, 7, 4, 9, 11, 9, 10, 11},
	{0, 8, 3, 4, 9, 7, 9, 11, 7, 9, 10, 11},
	{1, 10, 11, 1, 11, 4, 1, 4, 0, 7, 4, 11},
	{3, 1, 4, 3, 4, 8, 1, 10, 4, 7, 4, 11, 10, 11, 4},
	{4, 11, 7, 9, 11, 4, 9, 2, 11, 9, 1, 2},
	{9, 7, 4, 9, 11, 7, 9, 1, 11, 2, 11, 1, 0, 8, 3},
	{11, 7, 4, 11, 4, 2, 2, 4, 0},
	{11, 7, 4, 11, 4, 2, 8, 3, 4, 3, 2, 4},
	{2, 9, 10, 2, 7, 9, 2, 3, 7, 7, 4, 9},
	{9, 10, 7, 9, 7, 4, 10, 2, 7, 8, 7, 0, 2, 0, 7},
	{3, 7, 10, 3, 10, 2, 7, 4, 10, 1, 10, 0, 4, 0, 10},
	{1, 10, 2, 8, 7, 4},
	{4, 9, 1, 4, 1, 7, 7, 1, 3},
	{4, 9, 1, 4, 1, 7, 0, 8, 1, 8, 7, 1},
	{4, 0, 3, 7, 4, 3},
	{4, 8, 7},
	{9, 10, 8, 10, 11, 8},
	{3, 0, 9, 3, 9, 11, 11, 9, 10},
	{0, 1, 10, 0, 10, 8, 8, 10, 11},
	{3, 1, 10, 11, 3, 10},
	{1, 2, 11, 1, 11, 9, 9, 11, 8},
	{3, 0, 9, 3, 9, 11, 1, 2, 9, 2, 11, 9},
	{0, 2, 11, 8, 0, 11},
	{3, 2, 11},
	{2, 3, 8, 2, 8, 10, 10, 8, 9},
	{9, 10, 2, 0, 9, 2},
	{2, 3, 8, 2, 8, 10, 0, 1, 8, 1, 10, 8},
	{1, 10, 2},
	{1, 3, 8, 9, 1, 8},
	{0, 9, 1},
	{0, 3, 8},
	{},
}

//-----------------------------------------------------------------------------
