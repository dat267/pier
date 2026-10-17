// Package demo does nothing.
package demo

import "fmt"

const answer = 42
const ratio = 1.5e-3

func main() {
	msg := `raw
string`
	fmt.Println("hello", msg, 'x', 0x1f, 0b1010, answer, ratio)
}

type T struct{ A int }
