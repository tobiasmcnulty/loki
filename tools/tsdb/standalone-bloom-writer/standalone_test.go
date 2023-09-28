package main

import (
	"testing"
)

func BenchmarkSBFRandomStrings(b *testing.B) {
	for i := 0; i < b.N; i++ {
		testSBFRandomStrings(false)
	}
}

func BenchmarkSBFRandomStringsWithLRU(b *testing.B) {
	for i := 0; i < b.N; i++ {
		testSBFRandomStringsWithLRU(false)
	}
}

func BenchmarkSBFRandomStringsWithHashSet(b *testing.B) {
	for i := 0; i < b.N; i++ {
		testSBFRandomStringsWithHashSet(false)
	}
}

/*
func BenchmarkSBFRandomStringsWithFastCache(b *testing.B) {
	for i := 0; i < b.N; i++ {
		testSBFRandomStringsWithFastCache(false)
	}
}

*/

func BenchmarkSBFConstantStrings(b *testing.B) {
	for i := 0; i < b.N; i++ {
		testSBFConstantStrings(false)
	}
}

func BenchmarkSBFConstantStringsWithLRU(b *testing.B) {
	for i := 0; i < b.N; i++ {
		testSBFConstantStringsWithLRU(false)
	}
}
