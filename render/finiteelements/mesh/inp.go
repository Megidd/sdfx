package mesh

import (
	"fmt"
	"os"
	"time"

	"github.com/deadsy/sdfx/render/finiteelements/buffer"
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
	// Output `inp` file would include start layer.
	LayerStart int
	// Output `inp` file would exclude end layer.
	LayerEnd int
	// Layers fixed to the 3D print floor i.e. bottom layers. The boundary conditions.
	LayersFixed []int
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
	// Just a counter to keep track of written boundaries
	nextNodeBou uint32
	// Inside the function, according to the x, y, z, the caller decides on restraint.
	Restraint func(x, y, z float64) (bool, bool, bool)
	// Inside the function, according to the x, y, z, the caller decides on load.
	Load func(x, y, z float64) (float64, float64, float64)
}

// NewInp sets up a new writer.
func NewInp(
	m *Fem,
	path string,
	layerStart, layerEnd int,
	layersFixed []int,
	massDensity float32, youngModulus float32, poissonRatio float32,
	restraint func(x, y, z float64) (bool, bool, bool),
	load func(x, y, z float64) (float64, float64, float64),
) *Inp {
	return &Inp{
		Mesh:          m,
		Path:          path,
		PathNodes:     path + ".nodes",
		PathElsC3D4:   path + ".elements_C3D4",
		PathElsC3D10:  path + ".elements_C3D10",
		PathElsC3D8:   path + ".elements_C3D8",
		PathElsC3D20R: path + ".elements_C3D20R",
		PathBou:       path + ".boundary",
		LayerStart:    layerStart,
		LayerEnd:      layerEnd,
		LayersFixed:   layersFixed,
		TempVBuff:     buffer.NewVB(),
		MassDensity:   massDensity,
		YoungModulus:  youngModulus,
		PoissonRatio:  poissonRatio,
		Restraint:     restraint,
		Load:          load,
	}
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

	_, err = fC3D10.WriteString(fmt.Sprintf("*ELEMENT, TYPE=%s, ELSET=e3D10\n", "C3D10"))
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

	_, err = f.WriteString("*BOUNDARY\n")
	if err != nil {
		return err
	}

	var process func(int, int, int, []*buffer.Element)

	inp.nextNodeBou = 1 // ID starts from one not zero.

	process = func(x, y, z int, els []*buffer.Element) {
		var isLayerFixed bool
		for _, l := range inp.LayersFixed {
			if l == z {
				isLayerFixed = true
			}
		}

		if !isLayerFixed {
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
				if ids[n]+1 == inp.nextNodeBou {
					// ID starts from one not zero.
					_, err = f.WriteString(fmt.Sprintf("%d,1,3\n", ids[n]+1))
					if err != nil {
						panic("Couldn't write boundary to file: " + err.Error())
					}
					inp.nextNodeBou++
				}
			}
		}
	}

	inp.Mesh.iterate(process)

	return nil
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
	_, err = f.WriteString("*SOLID SECTION,MATERIAL=resin,ELSET=e3D10\n")
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

	// Write distributed loads.

	_, err = f.WriteString("*DLOAD\n")
	if err != nil {
		return err
	}

	// Assign gravity loading in the "positive" z-direction with magnitude 9810 to all elements.
	//
	// SLA 3D printing is done upside-down. 3D model is hanging from the print floor.
	// That's why gravity is in "positive" z-direction.
	// Here ”gravity” really stands for any acceleration vector.
	//
	// Refer to CalculiX solver documentation:
	// http://www.dhondt.de/ccx_2.20.pdf
	_, err = f.WriteString("eC3D4,GRAV,9810.,0.,0.,1.\n")
	if err != nil {
		return err
	}
	_, err = f.WriteString("e3D10,GRAV,9810.,0.,0.,1.\n")
	if err != nil {
		return err
	}
	_, err = f.WriteString("eC3D8,GRAV,9810.,0.,0.,1.\n")
	if err != nil {
		return err
	}
	_, err = f.WriteString("eC3D20R,GRAV,9810.,0.,0.,1.\n")
	if err != nil {
		return err
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
