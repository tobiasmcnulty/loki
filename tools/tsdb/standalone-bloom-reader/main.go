package main

// go build -o standalone-tester *.go
// ./standalone-tester
// go tool pprof standalone-tester foo.prof
func main() {
	generateBloom()
	searchForString()
}
