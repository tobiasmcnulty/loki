package main

import (
	"bufio"
	"github.com/owen-d/BoomFilters/boom"
	"math/rand"
	"os"
	"runtime/pprof"
	"time"
)

type Experiment struct {
	name          string
	tokenizer     Tokenizer
	bloom         func() *boom.ScalableBloomFilter
	encodeChunkID bool
}

func NewExperiment(name string, tokenizer Tokenizer, encodeChunkID bool, bloom func() *boom.ScalableBloomFilter) Experiment {
	return Experiment{
		name:          name,
		tokenizer:     tokenizer,
		bloom:         bloom,
		encodeChunkID: encodeChunkID,
	}
}

var (
	three      = newNGramTokenizer(3, 4, 0)
	threeSkip1 = newNGramTokenizer(3, 4, 1)
	threeSkip2 = newNGramTokenizer(3, 4, 2)
	threeSkip3 = newNGramTokenizer(3, 4, 3)

	onePctError  = func() *boom.ScalableBloomFilter { return boom.NewScalableBloomFilter(1024, 0.01, 0.8) }
	fivePctError = func() *boom.ScalableBloomFilter { return boom.NewScalableBloomFilter(1024, 0.05, 0.8) }

	experiment = NewExperiment(
		"token=3skip0_error=1%_indexchunks=true",
		three,
		true,
		onePctError,
	)
)

// GenerateRandomString generates a random string of the specified length
func GenerateRandomString(length int) []byte {
	rand.Seed(time.Now().UnixNano())

	// Characters to use in the random string
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

	// Create a byte slice of the specified length
	randomString := make([]byte, length)

	// Fill the byte slice with random characters from the charset
	for i := range randomString {
		randomString[i] = charset[rand.Intn(len(charset))]
	}

	return randomString
}

func testSBFRandomStrings(doProfile bool) {
	if doProfile {
		f, _ := os.Create("rand-strings-cpu.prof")
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}
	file, _ := os.Open("big.txt")
	defer file.Close()
	scanner := bufio.NewScanner(file)

	sbf := experiment.bloom()
	for scanner.Scan() {
		line := scanner.Text()
		tokens := experiment.tokenizer.Tokens(line)
		for _, token := range tokens {
			sbf.TestAndAdd(token.Key)
		}
	}
}

func testSBFRandomStringsWithLRU(doProfile bool) {
	if doProfile {
		f, _ := os.Create("rand-strings-lru-cpu.prof")
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}
	file, _ := os.Open("big.txt")
	defer file.Close()
	scanner := bufio.NewScanner(file)

	cache := NewLRUCache4(100000)
	sbf := experiment.bloom()
	for scanner.Scan() {
		line := scanner.Text()
		tokens := experiment.tokenizer.Tokens(line)
		for _, token := range tokens {
			if !cache.Get(token.Key) {
				cache.Put(token.Key)
				sbf.TestAndAdd(token.Key)
			}
		}
	}
}

func testSBFConstantStrings(doProfile bool) {
	if doProfile {
		f, _ := os.Create("const-strings-cpu.prof")
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}
	strBytes := []byte("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
	str := string(strBytes)
	sbf := experiment.bloom()
	for i := 0; i < 128457; i++ {
		tokens := experiment.tokenizer.Tokens(str)
		for _, token := range tokens {
			sbf.TestAndAdd(token.Key)
		}
	}
}

func testSBFConstantStringsWithLRU(doProfile bool) {
	if doProfile {
		f, _ := os.Create("const-strings-lru-cpu.prof")
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}
	strBytes := []byte("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
	str := string(strBytes)
	cache := NewLRUCache4(100000)
	sbf := experiment.bloom()
	for i := 0; i < 128457; i++ {

		tokens := experiment.tokenizer.Tokens(str)
		for _, token := range tokens {
			if !cache.Get(token.Key) {
				cache.Put(token.Key)
				sbf.TestAndAdd(token.Key)
			}
		}
	}
}
