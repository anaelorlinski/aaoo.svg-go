package utils

import "math"

type Fl = float32

func Maxs(values ...Fl) Fl {
	max := values[0]
	for _, w := range values {
		if w > max {
			max = w
		}
	}
	return max
}

func Floor(x Fl) Fl { return Fl(math.Floor(float64(x))) }

func Ceil(x Fl) Fl { return Fl(math.Ceil(float64(x))) }

// Hypot returns SQRT(a^2 + b^2)
func Hypot(a, b Fl) Fl { return Fl(math.Hypot(float64(a), float64(b))) }

type Set map[string]struct{}

func (s Set) Add(key string) {
	s[key] = struct{}{}
}

func (s Set) Has(key string) bool {
	_, in := s[key]
	return in
}

func NewSet(values ...string) Set {
	s := make(Set, len(values))
	for _, v := range values {
		s.Add(v)
	}
	return s
}
