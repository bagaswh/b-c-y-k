package main

import (
	"math/rand/v2"
	"testing"
)

const SIZE = 1000000

func BenchmarkSlice(b *testing.B) {
	s := make([]int, SIZE)
	for i := 0; i < SIZE; i++ {
		r := rand.IntN(10521345)
		s[i] = r
	}
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		sumSlice(s)
	}
}

func BenchmarkSliceOfPointers(b *testing.B) {
	s := make([]*int, SIZE)
	for i := 0; i < SIZE; i++ {
		r := rand.IntN(10521345)
		s[i] = &r
	}
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		sumSliceOfPointer(s)
	}
}

type sm interface {
	~[]int | ~map[int]int
}

func sumSlice(s []int) int {
	sum := 0
	for _, i := range s {
		sum += i
	}
	return sum
}

func sumSliceOfPointer(s []*int) int {
	sum := 0
	for _, i := range s {
		sum += *i
	}
	return sum
}

func BenchmarkMap(b *testing.B) {
	m := make(map[int]int, SIZE)
	for i := 0; i < SIZE; i++ {
		r := rand.IntN(10521345)
		m[i] = r
	}
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		sumMap(m)
	}
}

func sumMap(s map[int]int) int {
	sum := 0
	for _, i := range s {
		sum += i
	}
	return sum
}
