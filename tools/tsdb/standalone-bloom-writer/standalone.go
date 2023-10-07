package main

import (
	"bufio"
	"fmt"
	"github.com/owen-d/BoomFilters/boom"
	"math/rand"
	"os"
	"runtime/pprof"
	"strings"
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

const uuid = "2b1a5e46-36a2-4694-a4b1-f34cc7bdfc45"

var (
	three      = newNGramTokenizer(3, 4, 0)
	threeSkip1 = newNGramTokenizer(3, 4, 1)
	threeSkip2 = newNGramTokenizer(3, 4, 2)
	threeSkip3 = newNGramTokenizer(3, 4, 3)

	onePctError  = func() *boom.ScalableBloomFilter { return boom.NewScalableBloomFilter(1024, 0.01, 0.8) }
	fivePctError = func() *boom.ScalableBloomFilter { return boom.NewScalableBloomFilter(1024, 0.05, 0.8) }

	originalThreeSkip2 = originalnewNGramTokenizer(3, 4, 2)
	/*
		experiment = NewExperiment(
			"token=3skip0_error=1%_indexchunks=true",
			three,
			true,
			onePctError,
		)*/

	experiment = NewExperiment(
		"token=3skip0_error=1%_indexchunks=true",
		three,
		true,
		onePctError,
	)
	experiment2 = NewExperiment(
		"token=3skip2_error=1%_indexchunks=true",
		originalThreeSkip2,
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
	cache.Clear()
	if doProfile {
		f2, _ := os.Create("rand-strings-lru-mem.prof")
		pprof.WriteHeapProfile(f2)
		f2.Close()
	}
}

func testLRUMemory(doProfile bool) {

	cache := NewLRUCache4(100)

	for i := 0; i < 10; i++ {
		file, _ := os.Open("big.txt")
		defer file.Close()
		scanner := bufio.NewScanner(file)

		for scanner.Scan() {
			line := scanner.Text()
			tokens := experiment.tokenizer.Tokens(line)
			for _, token := range tokens {
				if !cache.Get(token.Key) {
					cache.Put(token.Key)
				}
			}
		}
		cache.Clear()
	}
	if doProfile {
		f2, _ := os.Create("rand-strings-lru-mem.prof")
		pprof.WriteHeapProfile(f2)
		f2.Close()
	}
}

/*
func testSBFRandomStringsWithFastCache(doProfile bool) {
	if doProfile {
		f, _ := os.Create("rand-strings-fc-cpu.prof")
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}
	file, _ := os.Open("big.txt")
	defer file.Close()
	scanner := bufio.NewScanner(file)

	cache := fastcache.New(256)
	sbf := experiment.bloom()
	for scanner.Scan() {
		line := scanner.Text()
		tokens := experiment.tokenizer.Tokens(line)
		for _, token := range tokens {
			if !cache.Has(token.Key) {
				cache.Set(token.Key, nil)
				sbf.TestAndAdd(token.Key)
			}
		}
	}
}
*/

func testSBFRandomStringsWithHashSet(doProfile bool) {
	if doProfile {
		f, _ := os.Create("rand-strings-hashset-cpu.prof")
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}
	file, _ := os.Open("big.txt")
	defer file.Close()
	scanner := bufio.NewScanner(file)

	cache := NewHashSet(100000)
	sbf := experiment.bloom()
	for scanner.Scan() {
		line := scanner.Text()
		tokens := experiment.tokenizer.Tokens(line)
		for _, token := range tokens {
			cache.PutBoth(token.Value, token.Key)
		}
	}
	hashSet := cache.SurfaceMap()
	for _, v := range hashSet {
		sbf.TestAndAdd(v)
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

func testUUID() {
	sbf := experiment.bloom()
	tokens := experiment.tokenizer.Tokens(uuid)
	for _, token := range tokens {
		sbf.TestAndAdd(token.Key)
	}

	for _, token := range tokens {
		fmt.Println(token.Key)
		if !sbf.Test(token.Key) {
			panic("not found")
		}
	}

}

const teststring1 = "this is my test string"
const teststring2 = " this is my test string"
const teststring3 = "  this is my test string"
const testvalue = "test "

func testSkips() {
	sbf1 := experiment2.bloom()
	tokens1 := experiment2.tokenizer.Tokens(teststring1)
	for _, token1 := range tokens1 {
		sbf1.TestAndAdd(token1.Key)
	}

	sbf2 := experiment2.bloom()
	tokens2 := experiment2.tokenizer.Tokens(teststring2)
	for _, token2 := range tokens2 {
		sbf2.TestAndAdd(token2.Key)
	}

	sbf3 := experiment2.bloom()
	tokens3 := experiment2.tokenizer.Tokens(teststring3)
	for _, token3 := range tokens3 {
		sbf3.TestAndAdd(token3.Key)
	}
	if strings.Contains(teststring1, testvalue) {
		fmt.Println("teststring1 contains 'test '")
	}
	if strings.Contains(teststring2, testvalue) {
		fmt.Println("teststring2 contains 'test '")
	}
	if strings.Contains(teststring3, testvalue) {
		fmt.Println("teststring3 contains 'test '")
	}
	testValueTokens := experiment2.tokenizer.Tokens(testvalue)
	for _, testToken := range testValueTokens {
		fmt.Println(testToken.Value)
		fmt.Println("sbf1:", sbf1.Test(testToken.Key))
		fmt.Println("sbf2:", sbf2.Test(testToken.Key))
		fmt.Println("sbf3:", sbf3.Test(testToken.Key))
	}

}
