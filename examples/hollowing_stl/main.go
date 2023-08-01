//-----------------------------------------------------------------------------
/*

Import an existing STL. Carve its inside before hollowing it. Re-render.

*/
//-----------------------------------------------------------------------------

package main

import (
	"log"

	"github.com/deadsy/sdfx/obj"
	"github.com/deadsy/sdfx/render"
	"github.com/deadsy/sdfx/sdf"
)

//-----------------------------------------------------------------------------

const wallThickness = 1.0

//-----------------------------------------------------------------------------

func carveinside(path string) (sdf.SDF3, error) {

	// create the SDF from the mesh
	// WARNING: It will only work on non-intersecting closed-surface(s) meshes.
	imported, err := obj.ImportSTL(path, 20, 3, 5)
	if err != nil {
		return nil, err
	}

	inside := sdf.Offset3D(imported, -wallThickness) // Pass negative value for inside.

	return inside, nil
}

//-----------------------------------------------------------------------------

func main() {
	inside, err := carveinside("../../files/teapot.stl")
	if err != nil {
		log.Fatalf("error: %s", err)
	}
	render.ToSTL(inside, "inside-carved-out.stl", render.NewMarchingCubesUniform(300))
}

//-----------------------------------------------------------------------------
