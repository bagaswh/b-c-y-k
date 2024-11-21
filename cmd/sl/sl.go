package main

import "fmt"

func main() {
	s := make([]byte, 4)
	s[0] = byte('a')
	s[1] = byte('b')
	s[2] = byte('c')
	s[3] = byte('d')
	fmt.Println(len(s), cap(s), string(s[len(s):cap(s)]))

	s = s[:0]
	fmt.Println(len(s), cap(s), string(s))
	s = s[:cap(s)]
	fmt.Println(len(s), cap(s), string(s))
	s = s[:]
	fmt.Println(len(s), cap(s), string(s))
	s = s[1:]
	fmt.Println(len(s), cap(s), string(s))
}
