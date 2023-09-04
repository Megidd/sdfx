package mesh

import (
	"fmt"
	"os"
	"time"

	"github.com/deadsy/sdfx/sdf/finiteelements/buffer"
	v3 "github.com/deadsy/sdfx/vec/v3"
)

// Inp writes different types of finite elements as ABAQUS or CalculiX `inp` file.
type Inp struct {
	// Finite elements mesh.
	Mesh *Fem
	// Output `inp` file path.
	Path string
	// For writing nodes to a separate file.
	PathNodes string
	// For writing elements to a separate file.
	PathElsC3D4 string
	// For writing elements to a separate file.
	PathElsC3D10 string
	// For writing elements to a separate file.
	PathElsC3D8 string
	// For writing elements to a separate file.
	PathElsC3D20R string
	// For writing boundary conditions to a separate file.
	PathBou string
	// For writing loads to a separate file.
	PathLoad string
	// For writing gravity to a separate file.
	PathGravity     string
	GravityIsNeeded bool
	// Output `inp` file would include start layer.
	LayerStart int
	// Output `inp` file would exclude end layer.
	LayerEnd int
	// To write only required nodes to `inp` file.
	TempVBuff *buffer.VB
	// Mechanical properties of 3D print resin.
	MassDensity  float32
	YoungModulus float32
	PoissonRatio float32
	// Just a counter to keep track of written elements
	eleID uint32
	// Just a counter to keep track of written nodes
	nextNode uint32
	// Single point constraint: one or more degrees of freedom are fixed for a given node. Passed in by the caller.
	Restraints []*Restraint
	// Point loads are applied to the nodes of the mesh. Passed in by the caller.
	Loads []*Load
	// Assigns gravity loading in this direction to all elements.
	GravityDirection v3.Vec
	// Assigns gravity loading with magnitude to all elements.
	GravityMagnitude float64
}

// NewInp sets up a new writer.
func NewInp(
	m *Fem,
	path string,
	layerStart, layerEnd int,
	massDensity float32, youngModulus float32, poissonRatio float32,
	restraints []*Restraint,
	loads []*Load,
	gravityDirection v3.Vec,
	gravityMagnitude float64,
	gravityIsNeeded bool,
) *Inp {
	inp := &Inp{
		Mesh:             m,
		Path:             path,
		PathNodes:        path + ".nodes",
		PathElsC3D4:      path + ".elements_C3D4",
		PathElsC3D10:     path + ".elements_C3D10",
		PathElsC3D8:      path + ".elements_C3D8",
		PathElsC3D20R:    path + ".elements_C3D20R",
		PathBou:          path + ".boundary",
		PathLoad:         path + ".load",
		PathGravity:      path + ".gravity",
		GravityIsNeeded:  gravityIsNeeded,
		LayerStart:       layerStart,
		LayerEnd:         layerEnd,
		TempVBuff:        buffer.NewVB(),
		MassDensity:      massDensity,
		YoungModulus:     youngModulus,
		PoissonRatio:     poissonRatio,
		Restraints:       restraints,
		Loads:            loads,
		GravityDirection: gravityDirection,
		GravityMagnitude: gravityMagnitude,
	}

	// TODO: complete loading.
	//
	// Figure out node and voxel for each load.
	// TODO: move this statement to the logic that writes loads to file.
	for _, l := range inp.Loads {
		l.voxels, _, _ = inp.Mesh.VoxelsIntersecting(l.Location)
	}

	return inp
}

// Write starts writing to `inp` file.
func (inp *Inp) Write() error {
	f, err := os.Create(inp.Path)
	if err != nil {
		return err
	}
	defer f.Close()

	err = inp.writeHeader(f)
	if err != nil {
		return err
	}

	// Write nodes.

	// Include a separate file to avoid cluttering the `inp` file.
	_, err = f.WriteString(fmt.Sprintf("*INCLUDE,INPUT=%s\n", inp.PathNodes))
	if err != nil {
		return err
	}

	// Temp buffer is just to avoid writing repeated nodes into the `inpt` file.
	defer inp.TempVBuff.DestroyHashTable()

	err = inp.writeNodes()
	if err != nil {
		return err
	}

	// Write elements.

	// Include a separate file to avoid cluttering the `inp` file.
	_, err = f.WriteString(fmt.Sprintf("*INCLUDE,INPUT=%s\n", inp.PathElsC3D4))
	if err != nil {
		return err
	}
	// Include a separate file to avoid cluttering the `inp` file.
	_, err = f.WriteString(fmt.Sprintf("*INCLUDE,INPUT=%s\n", inp.PathElsC3D10))
	if err != nil {
		return err
	}
	// Include a separate file to avoid cluttering the `inp` file.
	_, err = f.WriteString(fmt.Sprintf("*INCLUDE,INPUT=%s\n", inp.PathElsC3D8))
	if err != nil {
		return err
	}
	// Include a separate file to avoid cluttering the `inp` file.
	_, err = f.WriteString(fmt.Sprintf("*INCLUDE,INPUT=%s\n", inp.PathElsC3D20R))
	if err != nil {
		return err
	}

	err = inp.writeElements()
	if err != nil {
		return err
	}

	// Fix the degrees of freedom one through three for all nodes on specific layers.

	// Include a separate file to avoid cluttering the `inp` file.
	_, err = f.WriteString(fmt.Sprintf("*INCLUDE,INPUT=%s\n", inp.PathBou))
	if err != nil {
		return err
	}

	err = inp.writeBoundary()
	if err != nil {
		return err
	}

	return inp.writeFooter(f)
}

func (inp *Inp) writeHeader(f *os.File) error {
	_, _, layersZ := inp.Mesh.Size()
	if 0 <= inp.LayerStart && inp.LayerStart < inp.LayerEnd && inp.LayerEnd <= layersZ {
		// Good.
	} else {
		return fmt.Errorf("start or end layer is beyond range")
	}

	_, err := f.WriteString("**\n** Structure: finite elements of a 3D model.\n** Generated by: https://github.com/deadsy/sdfx\n**\n")
	if err != nil {
		return err
	}

	_, err = f.WriteString("*HEADING\nModel: 3D model Date: " + time.Now().UTC().Format("2006-Jan-02 MST") + "\n")
	if err != nil {
		return err
	}

	return nil
}

func (inp *Inp) writeNodes() error {
	// Write to a separate file to avoid cluttering the `inp` file.
	f, err := os.Create(inp.PathNodes)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.WriteString("*NODE\n")
	if err != nil {
		return err
	}

	var process func(int, int, int, []*buffer.Element)

	inp.nextNode = 1 // ID starts from one not zero.

	process = func(x, y, z int, els []*buffer.Element) {
		if z >= inp.LayerStart && z < inp.LayerEnd {
			// Good.
		} else {
			return
		}

		for _, el := range els {
			vertices := make([]v3.Vec, len(el.Nodes))
			ids := make([]uint32, len(el.Nodes))
			for n := 0; n < len(el.Nodes); n++ {
				vertices[n] = inp.Mesh.vertex(el.Nodes[n])
				ids[n] = inp.TempVBuff.Id(vertices[n])
			}

			// Write the node IDs.
			for n := 0; n < len(el.Nodes); n++ {
				// Only write node if it's not already written to file.
				if ids[n]+1 == inp.nextNode {
					// ID starts from one not zero.
					_, err = f.WriteString(fmt.Sprintf("%d,%f,%f,%f\n", ids[n]+1, float32(vertices[n].X), float32(vertices[n].Y), float32(vertices[n].Z)))
					if err != nil {
						panic("Couldn't write node to file: " + err.Error())
					}
					inp.nextNode++
				}

			}
		}
	}

	inp.Mesh.iterate(process)

	return nil
}

func (inp *Inp) writeElements() error {
	// Write to a separate file to avoid cluttering the `inp` file.
	fC3D4, err := os.Create(inp.PathElsC3D4)
	if err != nil {
		return err
	}
	defer fC3D4.Close()

	// Write to a separate file to avoid cluttering the `inp` file.
	fC3D10, err := os.Create(inp.PathElsC3D10)
	if err != nil {
		return err
	}
	defer fC3D10.Close()

	// Write to a separate file to avoid cluttering the `inp` file.
	fC3D8, err := os.Create(inp.PathElsC3D8)
	if err != nil {
		return err
	}
	defer fC3D8.Close()

	// Write to a separate file to avoid cluttering the `inp` file.
	fC3D20R, err := os.Create(inp.PathElsC3D20R)
	if err != nil {
		return err
	}
	defer fC3D20R.Close()

	_, err = fC3D4.WriteString(fmt.Sprintf("*ELEMENT, TYPE=%s, ELSET=eC3D4\n", "C3D4"))
	if err != nil {
		return err
	}

	_, err = fC3D10.WriteString(fmt.Sprintf("*ELEMENT, TYPE=%s, ELSET=eC3D10\n", "C3D10"))
	if err != nil {
		return err
	}

	_, err = fC3D8.WriteString(fmt.Sprintf("*ELEMENT, TYPE=%s, ELSET=eC3D8\n", "C3D8"))
	if err != nil {
		return err
	}

	_, err = fC3D20R.WriteString(fmt.Sprintf("*ELEMENT, TYPE=%s, ELSET=eC3D20R\n", "C3D20R"))
	if err != nil {
		return err
	}

	// Define a function variable with the signature
	var process func(int, int, int, []*buffer.Element)
	// Assign a function literal to the variable
	process = func(x, y, z int, els []*buffer.Element) {
		if z >= inp.LayerStart && z < inp.LayerEnd {
			// Good.
		} else {
			return
		}
		for _, el := range els {
			ids := make([]uint32, len(el.Nodes))
			for n := 0; n < len(el.Nodes); n++ {
				vertex := inp.Mesh.vertex(el.Nodes[n])
				ids[n] = inp.TempVBuff.Id(vertex)
			}

			// ID starts from one not zero.

			switch el.Type() {
			case buffer.C3D4:
				{
					_, err = fC3D4.WriteString(fmt.Sprintf("%d,%d,%d,%d,%d\n", inp.eleID+1, ids[0]+1, ids[1]+1, ids[2]+1, ids[3]+1))
				}
			case buffer.C3D10:
				{
					_, err = fC3D10.WriteString(fmt.Sprintf("%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d\n", inp.eleID+1, ids[0]+1, ids[1]+1, ids[2]+1, ids[3]+1, ids[4]+1, ids[5]+1, ids[6]+1, ids[7]+1, ids[8]+1, ids[9]+1))
				}
			case buffer.C3D8:
				{
					_, err = fC3D8.WriteString(fmt.Sprintf("%d,%d,%d,%d,%d,%d,%d,%d,%d\n", inp.eleID+1, ids[0]+1, ids[1]+1, ids[2]+1, ids[3]+1, ids[4]+1, ids[5]+1, ids[6]+1, ids[7]+1))
				}
			case buffer.C3D20R:
				{
					// There should not be more than 16 entries in a line;
					// That's why there is new line in the middle.
					// Refer to CalculiX solver documentation:
					// http://www.dhondt.de/ccx_2.20.pdf
					_, err = fC3D20R.WriteString(fmt.Sprintf("%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,%d,\n%d,%d,%d,%d,%d\n", inp.eleID+1, ids[0]+1, ids[1]+1, ids[2]+1, ids[3]+1, ids[4]+1, ids[5]+1, ids[6]+1, ids[7]+1, ids[8]+1, ids[9]+1, ids[10]+1, ids[11]+1, ids[12]+1, ids[13]+1, ids[14]+1, ids[15]+1, ids[16]+1, ids[17]+1, ids[18]+1, ids[19]+1))
				}
			case buffer.Unknown:
				{
					fmt.Println("Element has unknown type :(")
				}
			}

			if err != nil {
				panic("Couldn't write finite element to file: " + err.Error())
			}

			inp.eleID++
		}
	}

	inp.Mesh.iterate(process)

	return nil
}

func (inp *Inp) writeBoundary() error {
	// Write to a separate file to avoid cluttering the `inp` file.
	f, err := os.Create(inp.PathBou)
	if err != nil {
		return err
	}
	defer f.Close()

	// Figure out reference node and voxels for each restraint.
	for _, r := range inp.Restraints {
		// Only set voxels only if they are not already set.
		// If voxels are already set, it means the caller has decided about voxels.
		if len(r.voxels) < 1 {
			r.voxels, _, _ = inp.Mesh.VoxelsIntersecting(r.Location)
		}
	}

	for i, r := range inp.Restraints {
		isFixedX, isFixedY, isFixedZ := r.IsFixedX, r.IsFixedY, r.IsFixedZ
		if !isFixedX && !isFixedY && !isFixedZ {
			return fmt.Errorf("restraint has no fixed degree of freedom")
		}

		nodeSet := make([]uint32, 0)

		for _, vox := range r.voxels {

			// Get elements in the voxel
			elements := inp.Mesh.IBuff.Grid.Get(vox.X, vox.Y, vox.Z)

			for _, element := range elements {
				for _, node := range element.Nodes {
					// Node ID should be consistant with the temp vertex buffer.
					// Node ID is different on these two: (1) original vertex buffer, (2) temp vertex buffer.
					vertex := inp.Mesh.vertex(node)
					id := inp.TempVBuff.Id(vertex)
					nodeSet = append(nodeSet, id)
				}
			}
		}

		// Write node set for this restraint.
		_, err = f.WriteString(fmt.Sprintf("*NSET,NSET=restraint%d\n", i+1))
		if err != nil {
			return err
		}

		for j, id := range nodeSet {
			if j == len(nodeSet)-1 {
				// The last one is written differently.
				_, err = f.WriteString(fmt.Sprintf("%d\n", id+1))
				if err != nil {
					return err
				}
			} else if j != 0 && j%15 == 0 {
				// According to CCX manual: maximum 16 entries per line.
				_, err = f.WriteString(fmt.Sprintf("%d,\n", id+1))
				if err != nil {
					return err
				}
			} else {
				_, err = f.WriteString(fmt.Sprintf("%d,", id+1))
				if err != nil {
					return err
				}
			}
		}

		// Put the boundary constraints on the reference node.
		_, err = f.WriteString("*BOUNDARY\n")
		if err != nil {
			return err
		}

		// To be written:
		//
		// 1) Node number/ID or node set label.
		// 2) First degree of freedom constrained.
		// 3) Last degree of freedom constrained. This field may be left blank if only
		// one degree of freedom is constrained.
		//
		// Note: written node ID would start from one not zero.

		if isFixedX && isFixedY && isFixedZ {
			_, err = f.WriteString(fmt.Sprintf("restraint%d,1\n", i+1))
			if err != nil {
				panic("Couldn't write boundary to file: " + err.Error())
			}
			_, err = f.WriteString(fmt.Sprintf("restraint%d,2\n", i+1))
			if err != nil {
				panic("Couldn't write boundary to file: " + err.Error())
			}
			_, err = f.WriteString(fmt.Sprintf("restraint%d,3\n", i+1))
			if err != nil {
				panic("Couldn't write boundary to file: " + err.Error())
			}
		} else if isFixedX && isFixedY && !isFixedZ {
			_, err = f.WriteString(fmt.Sprintf("restraint%d,1\n", i+1))
			if err != nil {
				panic("Couldn't write boundary to file: " + err.Error())
			}
			_, err = f.WriteString(fmt.Sprintf("restraint%d,2\n", i+1))
			if err != nil {
				panic("Couldn't write boundary to file: " + err.Error())
			}
		} else if !isFixedX && isFixedY && isFixedZ {
			_, err = f.WriteString(fmt.Sprintf("restraint%d,2\n", i+1))
			if err != nil {
				panic("Couldn't write boundary to file: " + err.Error())
			}
			_, err = f.WriteString(fmt.Sprintf("restraint%d,3\n", i+1))
			if err != nil {
				panic("Couldn't write boundary to file: " + err.Error())
			}
		} else if isFixedX && !isFixedY && isFixedZ {
			_, err = f.WriteString(fmt.Sprintf("restraint%d,1\n", i+1))
			if err != nil {
				panic("Couldn't write boundary to file: " + err.Error())
			}
			_, err = f.WriteString(fmt.Sprintf("restraint%d,3\n", i+1))
			if err != nil {
				panic("Couldn't write boundary to file: " + err.Error())
			}
		} else if isFixedX && !isFixedY && !isFixedZ {
			_, err = f.WriteString(fmt.Sprintf("restraint%d,1\n", i+1))
			if err != nil {
				panic("Couldn't write boundary to file: " + err.Error())
			}
		} else if !isFixedX && isFixedY && !isFixedZ {
			_, err = f.WriteString(fmt.Sprintf("restraint%d,2\n", i+1))
			if err != nil {
				panic("Couldn't write boundary to file: " + err.Error())
			}
		} else if !isFixedX && !isFixedY && isFixedZ {
			_, err = f.WriteString(fmt.Sprintf("restraint%d,3\n", i+1))
			if err != nil {
				panic("Couldn't write boundary to file: " + err.Error())
			}
		}
	}

	return nil
}

func (inp *Inp) writeLoad() error {
	// Write to a separate file to avoid cluttering the `inp` file.
	f, err := os.Create(inp.PathLoad)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.WriteString("*CLOAD\n")
	if err != nil {
		return err
	}

	// TODO: complete loading.

	// Figure out node and voxel for each.
	for _, l := range inp.Loads {
		l.voxels, _, _ = inp.Mesh.VoxelsIntersecting(l.Location)
	}

	// The closest node to any restraint is already computed.
	for _, l := range inp.Loads {
		// Node ID should be consistant with the temp vertex buffer.
		// Node ID is different on these two: (1) original vertex buffer, (2) temp vertex buffer.
		vertex := inp.Mesh.vertex(l.nodeREF)
		id := inp.TempVBuff.Id(vertex)

		// To be written:
		//
		// 1) Node ID.
		// 2) Degree of freedom.
		// 3) Magnitude of the load.
		//
		// Note: written node ID would start from one not zero.

		_, err = f.WriteString(fmt.Sprintf("%d,1,%f\n", id+1, l.Magnitude.X))
		if err != nil {
			panic("Couldn't write load to file: " + err.Error())
		}
		_, err = f.WriteString(fmt.Sprintf("%d,2,%f\n", id+1, l.Magnitude.Y))
		if err != nil {
			panic("Couldn't write load to file: " + err.Error())
		}
		_, err = f.WriteString(fmt.Sprintf("%d,3,%f\n", id+1, l.Magnitude.Z))
		if err != nil {
			panic("Couldn't write load to file: " + err.Error())
		}
	}

	return nil
}

func (inp *Inp) writeGravity() error {
	// Write to a separate file to avoid cluttering the `inp` file.
	f, err := os.Create(inp.PathGravity)
	if err != nil {
		return err
	}
	defer f.Close()

	// Write distributed loads.

	_, err = f.WriteString("*DLOAD\n")
	if err != nil {
		return err
	}

	// Assign gravity loading in any direction with any magnitude to all elements.
	//
	// 9810 could be gravity magnitude in mm/sec^2 units.
	//
	// SLA 3D printing is done upside-down. 3D model is hanging from the print floor.
	// That's why gravity could be in "positive" z-direction.
	// Here ”gravity” really stands for any acceleration vector.
	//
	// Refer to CalculiX solver documentation:
	// http://www.dhondt.de/ccx_2.20.pdf
	_, err = f.WriteString(
		fmt.Sprintf(
			"eC3D4,GRAV,%v,%v,%v,%v\n",
			inp.GravityMagnitude,
			inp.GravityDirection.X, inp.GravityDirection.Y, inp.GravityDirection.Z,
		),
	)
	if err != nil {
		return err
	}
	_, err = f.WriteString(
		fmt.Sprintf(
			"eC3D10,GRAV,%v,%v,%v,%v\n",
			inp.GravityMagnitude,
			inp.GravityDirection.X, inp.GravityDirection.Y, inp.GravityDirection.Z,
		),
	)
	if err != nil {
		return err
	}
	_, err = f.WriteString(
		fmt.Sprintf(
			"eC3D8,GRAV,%v,%v,%v,%v\n",
			inp.GravityMagnitude,
			inp.GravityDirection.X, inp.GravityDirection.Y, inp.GravityDirection.Z,
		),
	)
	if err != nil {
		return err
	}
	_, err = f.WriteString(
		fmt.Sprintf(
			"eC3D20R,GRAV,%v,%v,%v,%v\n",
			inp.GravityMagnitude,
			inp.GravityDirection.X, inp.GravityDirection.Y, inp.GravityDirection.Z,
		),
	)

	return err
}

func (inp *Inp) writeFooter(f *os.File) error {

	// Define material.
	// Units of measurement are mm,N,s,K.
	// Refer to:
	// https://engineering.stackexchange.com/q/54454/15178
	// Refer to:
	// Units chapter of CalculiX solver documentation:
	// http://www.dhondt.de/ccx_2.20.pdf

	_, err := f.WriteString("*MATERIAL, name=resin\n")
	if err != nil {
		return err
	}

	_, err = f.WriteString(fmt.Sprintf("*ELASTIC,TYPE=ISO\n%e,%e,0\n", inp.YoungModulus, inp.PoissonRatio))
	if err != nil {
		return err
	}

	_, err = f.WriteString(fmt.Sprintf("*DENSITY\n%e\n", inp.MassDensity))
	if err != nil {
		return err
	}

	// Assign material to all elements
	_, err = f.WriteString("*SOLID SECTION,MATERIAL=resin,ELSET=eC3D4\n")
	if err != nil {
		return err
	}

	// Assign material to all elements
	_, err = f.WriteString("*SOLID SECTION,MATERIAL=resin,ELSET=eC3D10\n")
	if err != nil {
		return err
	}

	// Assign material to all elements
	_, err = f.WriteString("*SOLID SECTION,MATERIAL=resin,ELSET=eC3D8\n")
	if err != nil {
		return err
	}

	// Assign material to all elements
	_, err = f.WriteString("*SOLID SECTION,MATERIAL=resin,ELSET=eC3D20R\n")
	if err != nil {
		return err
	}

	// Write analysis

	_, err = f.WriteString("*STEP\n*STATIC\n")
	if err != nil {
		return err
	}

	// Write point loads.

	// Include a separate file to avoid cluttering the `inp` file.
	_, err = f.WriteString(fmt.Sprintf("*INCLUDE,INPUT=%s\n", inp.PathLoad))
	if err != nil {
		return err
	}

	err = inp.writeLoad()
	if err != nil {
		return err
	}

	if inp.GravityIsNeeded {
		// Include a separate file to avoid cluttering the `inp` file.
		_, err = f.WriteString(fmt.Sprintf("*INCLUDE,INPUT=%s\n", inp.PathGravity))
		if err != nil {
			return err
		}

		err = inp.writeGravity()
		if err != nil {
			return err
		}
	}

	// Pick element results.

	_, err = f.WriteString("*EL FILE\n")
	if err != nil {
		return err
	}

	_, err = f.WriteString("S\n")
	if err != nil {
		return err
	}

	// Pick node results.

	_, err = f.WriteString("*NODE FILE\n")
	if err != nil {
		return err
	}

	_, err = f.WriteString("U\n")
	if err != nil {
		return err
	}

	// Conclude.

	_, err = f.WriteString("*END STEP\n")
	if err != nil {
		return err
	}

	return nil
}
