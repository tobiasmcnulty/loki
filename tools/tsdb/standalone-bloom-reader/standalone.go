package main

import (
	"bufio"
	"bytes"
	"encoding/gob"
	"fmt"
	"github.com/owen-d/BoomFilters/boom"
	"os"
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

func generateBloom() {
	file, _ := os.Open("big.txt")
	defer file.Close()
	scanner := bufio.NewScanner(file)

	cache := NewLRUCache4(100000)
	sbf := experiment.bloom()
	sbf.Reset()
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

	writeSBFToFile(sbf, "big.txt.sbf")

	testsbf(sbf)
}

func testsbf(sbf *boom.ScalableBloomFilter) {
	var buf bytes.Buffer
	gob.NewEncoder(&buf).Encode(sbf)

	f2 := boom.NewScalableBloomFilter(10, 0.1, 0.8)
	gob.NewDecoder(&buf).Decode(f2)
}

func writeSBFToFile(sbf *boom.ScalableBloomFilter, filename string) error {
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	_, err = sbf.WriteTo(w)

	err = w.Flush()

	return err
}

func readSBFFromFile(filename string) *boom.ScalableBloomFilter {
	file, _ := os.Open(filename)
	defer file.Close()

	sbf := experiment.bloom()
	sbf.ReadFrom(file)
	return sbf
}

func searchForString() {
	//teststrings := []string{"the", "thee", "theee"}

	teststrings := []string{"dri", "drift", "drifted", "drifts", "drifting", "driftx"}
	sbf := readSBFFromFile("big.txt.sbf")
	for _, teststring := range teststrings {
		contains := sbf.Test([]byte(teststring))
		fmt.Println(teststring, contains)

	}
}
