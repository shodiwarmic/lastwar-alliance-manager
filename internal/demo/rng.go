package demo

import (
	"hash/fnv"
	"math"
	"math/rand/v2"
)

// seed is the one fixed seed behind every generated value. Changing it changes the whole
// alliance; the screenshots would then have to be retaken as a set.
const seed = 1894

// stream returns a deterministic generator for one area of the dataset. Each area has its
// own stream so adding a row to one (an extra train log) never reshuffles another (the
// roster's names).
func stream(area string) *rand.Rand {
	h := fnv.New64a()
	h.Write([]byte(area))
	return rand.New(rand.NewPCG(seed, h.Sum64()))
}

// fromDeciles samples a value whose distribution follows the nine decile boundaries, with
// a floor of d1/tailFactor and a ceiling of d9*tailFactor, interpolating linearly between
// neighbouring boundaries. u is a uniform draw in [0, 1).
func fromDeciles(d []float64, u float64) float64 {
	pts := make([]float64, 0, len(d)+2)
	pts = append(pts, d[0]/tailFactor)
	pts = append(pts, d...)
	pts = append(pts, d[len(d)-1]*tailFactor)
	pos := u * float64(len(pts)-1)
	i := int(pos)
	if i >= len(pts)-1 {
		return pts[len(pts)-1]
	}
	return pts[i] + (pts[i+1]-pts[i])*(pos-float64(i))
}

// jitter multiplies v by a factor in [1-f, 1+f].
func jitter(r *rand.Rand, v, f float64) float64 { return v * (1 + f*(2*r.Float64()-1)) }

// pick chooses from a weighted list.
func pick(r *rand.Rand, w []weighted) string {
	var total float64
	for _, x := range w {
		total += x.Weight
	}
	u := r.Float64() * total
	for _, x := range w {
		if u < x.Weight {
			return x.Value
		}
		u -= x.Weight
	}
	return w[len(w)-1].Value
}

// round2 rounds to two significant figures and then to a plausible-looking integer, so a
// generated stat never ends in a long run of zeros (which reads as fake) yet is never a
// copied value either.
func roundStat(r *rand.Rand, v float64) int64 {
	if v <= 0 {
		return 0
	}
	mag := math.Pow(10, math.Floor(math.Log10(v))-3)
	return int64(math.Round(v/mag)*mag) + int64(r.IntN(int(math.Max(1, mag))))
}
