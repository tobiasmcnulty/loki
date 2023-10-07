package main

// go build -o standalone-tester *.go
// ./standalone-tester
// go tool pprof standalone-tester foo.prof
func main() {
	//testSBFRandomStrings(true)
	//testSBFRandomStringsWithLRU(true)
	//testLRUMemory(true)
	//testSBFRandomStringsWithHashSet(true)
	//testSBFConstantStrings(true)
	//testSBFConstantStringsWithLRU(true)
	//testSBFRandomStringsWithFastCache(true)
	//testUUID()
	testSkips()
}
