package main

import "math"

type Shape interface {
	Area() float64
	Perimeter() float64
}

type Circle struct{ R float64 }

func (c Circle) Area() float64      { return math.Pi * c.R * c.R }
func (c Circle) Perimeter() float64 { return 2 * math.Pi * c.R }

type Rect struct{ W, H float64 }

func (r *Rect) Area() float64      { return r.W * r.H }
func (r *Rect) Perimeter() float64 { return 2 * (r.W + r.H) }

// TotalArea dispatches through the Shape interface — the concrete method
// is unknown statically, so all implementations should be listed.
func TotalArea(shapes []Shape) float64 {
	total := 0.0
	for _, s := range shapes {
		total += s.Area()
	}
	return total
}

func BuildShapes() []Shape {
	c := Circle{R: 2}
	r := &Rect{W: 3, H: 4}
	return []Shape{c, r}
}
