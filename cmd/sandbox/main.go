package main

import "fmt"

func main() {
	m := map[int]int{}
	fmt.Printf("%v\n", m)
	m[1] = 2
	fmt.Printf("%v\n", m)
	delete(m, 1)
	fmt.Printf("%v\n", m)
}
