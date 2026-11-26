package game

import (
	"fmt"
	"math"
)

// Moon is the moon generator's parameters, as the browser's engine reads
// them (frontend/engine/src/voxel/Types.ts MoonParams): a sphere roughened
// by fractal noise and pocked by craters, from a seed.
type Moon struct {
	Seed            int64   `json:"seed"`
	VoxelSize       float64 `json:"voxelSize"`
	Radius          float64 `json:"radius"`
	Amplitude       float64 `json:"amplitude"`
	Frequency       float64 `json:"frequency"`
	Octaves         int     `json:"octaves"`
	Craters         int     `json:"craters"`
	CraterMaxRadius float64 `json:"craterMaxRadius"`
	DustDepth       float64 `json:"dustDepth"`
}

// DefaultMoon is a new world's generator (the engine's DEFAULT_MOON).
var DefaultMoon = Moon{Seed: 1, VoxelSize: 0.5, Radius: 100, Amplitude: 6, Frequency: 0.02, Octaves: 5, Craters: 24, CraterMaxRadius: 22, DustDepth: 1.5}

const moonSchema = `{"type":"object","description":"The moon generator's parameters; any left out keep their value","properties":{
  "seed":{"type":"integer","description":"The random seed: another seed is another moon"},
  "voxelSize":{"type":"number","minimum":0.25,"maximum":2,"description":"Metres per voxel; 0.5 is the target near the player"},
  "radius":{"type":"number","minimum":20,"maximum":400,"description":"The moon's mean radius in metres"},
  "amplitude":{"type":"number","minimum":0,"maximum":40,"description":"Height of the hills in metres"},
  "frequency":{"type":"number","minimum":0.001,"maximum":0.2,"description":"Hills per metre of the first octave"},
  "octaves":{"type":"integer","minimum":1,"maximum":8,"description":"Octaves of fractal noise"},
  "craters":{"type":"integer","minimum":0,"maximum":200,"description":"How many craters"},
  "craterMaxRadius":{"type":"number","minimum":1,"maximum":100,"description":"The largest crater's radius in metres"},
  "dustDepth":{"type":"number","minimum":0,"maximum":20,"description":"Metres of dust over the rock"}}}`

// Check reports what is wrong with the parameters, as problems' paths
// under path.
func (m Moon) Check(path string) (string, string) {
	bad := func(field string, lo, hi float64, v float64) (string, string) {
		if math.IsNaN(v) || v < lo || v > hi {
			return path + "." + field, fmt.Sprintf("%s must be from %g to %g", field, lo, hi)
		}
		return "", ""
	}
	for _, c := range []struct {
		f      string
		lo, hi float64
		v      float64
	}{
		{"voxelSize", 0.25, 2, m.VoxelSize}, {"radius", 20, 400, m.Radius}, {"amplitude", 0, 40, m.Amplitude},
		{"frequency", 0.001, 0.2, m.Frequency}, {"octaves", 1, 8, float64(m.Octaves)}, {"craters", 0, 200, float64(m.Craters)},
		{"craterMaxRadius", 1, 100, m.CraterMaxRadius}, {"dustDepth", 0, 20, m.DustDepth},
	} {
		if p, msg := bad(c.f, c.lo, c.hi, c.v); p != "" {
			return p, msg
		}
	}
	if m.Seed < 0 || m.Seed > math.MaxUint32 {
		return path + ".seed", "seed must be from 0 to 4294967295"
	}
	return "", ""
}
