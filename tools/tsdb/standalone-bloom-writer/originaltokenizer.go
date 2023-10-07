package main

import (
	"unicode/utf8"
)

type OriginalToken struct {
	// Either key or value may be empty
	Key, Value string
}
type OriginalTokenizer interface {
	Tokens(line string) []Token
}

type originalngramTokenizer struct {
	// [min,max) exclusivity
	min, max, skip int
	buffers        [][]rune // circular buffers used for ngram generation
}

func originalnewNGramTokenizer(min, max, skip int) *ngramTokenizer {
	t := &ngramTokenizer{
		min:  min,
		max:  max,
		skip: skip,
	}
	for i := t.min; i < t.max; i++ {
		t.buffers = append(t.buffers, make([]rune, i))
	}

	return t

}

func (t *originalngramTokenizer) Tokens(line string) (res []Token) {
	var i int // rune index (not position that is measured in the range loop)
	for _, r := range line {

		// j is the index of the buffer to use
		for j := 0; j < (t.max - t.min); j++ {
			// n is the length of the ngram
			n := j + t.min
			// pos is the position in the buffer to overwrite
			pos := i % n
			t.buffers[j][pos] = r

			if i >= n-1 && (i+1-n)%(t.skip+1) == 0 {
				ngram := originalreassemble(t.buffers[j], (i+1)%n)
				res = append(res, Token{Key: string(ngram), Value: ""})
			}
		}
		i++
	}
	return
}

func originalreassemble(buf []rune, pos int) []byte {
	res := make([]byte, 0, len(buf)*4) // 4 bytes per rune (i32)
	for i := 0; i < len(buf); i++ {
		cur := (pos + i) % len(buf)
		res = utf8.AppendRune(res, buf[cur])
	}
	return res
}
