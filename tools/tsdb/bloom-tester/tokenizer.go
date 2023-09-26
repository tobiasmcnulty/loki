package main

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/log"
)

type Token struct {
	// Either key or value may be empty
	Key, Value string
}
type Tokenizer interface {
	Tokens(line string) []Token
}

type logfmtTokenizer struct {
	parser *log.LogfmtParser
	lbls   *log.LabelsBuilder
}

func (t *logfmtTokenizer) Tokens(line string) []Token {
	t.lbls.Reset()
	t.parser.Process(0, []byte(line), t.lbls)
	ls := t.lbls.LabelsResult().Labels()
	res := make([]Token, 0, len(ls))
	for _, l := range ls {
		res = append(res, Token{Key: l.Name, Value: l.Value})
	}
	return res
}

func newLogfmtTokenizer() *logfmtTokenizer {
	return &logfmtTokenizer{
		// non strict, allow empty values
		parser: log.NewLogfmtParser(false, true),
		lbls:   log.NewBaseLabelsBuilder().ForLabels(nil, 0),
	}
}

type ngramTokenizer struct {
	// [min,max) exclusivity
	min, max, skip int
	buffers        [][]rune // circular buffers used for ngram generation
	runeBuffer     []byte   // buffer used for token generation
}

func newNGramTokenizer(min, max, skip int) *ngramTokenizer {
	capacity := max - min
	t := &ngramTokenizer{
		min:     min,
		max:     max,
		skip:    skip,
		buffers: make([][]rune, capacity),
	}
	for i := t.min; i < t.max; i++ {
		t.buffers[i-t.min] = make([]rune, i)
	}
	t.runeBuffer = make([]byte, 0, max*4)

	return t
}

func (t *ngramTokenizer) Tokens(line string) (res []Token) {
	res = make([]Token, 0, len(line))
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
				t.runeBuffer = reassemble(t.buffers[j], (i+1)%n, t.runeBuffer)
				res = append(res, Token{Key: string(t.tokenBuffer), Value: ""})
			}
		}
		i++
	}
	return
}

func reassemble(buf []rune, pos int, result []byte) []byte {
	result = result[:0] // Reset the result slice
	for i := 0; i < len(buf); i++ {
		cur := (pos + i) % len(buf)
		result = utf8.AppendRune(result, buf[cur])
	}
	return result
}

type WrappedTokenizer struct {
	t Tokenizer
	f func(Token) Token
}

func (w *WrappedTokenizer) Tokens(line string) []Token {
	toks := w.t.Tokens(line)
	res := make([]Token, 0, len(toks)*2)
	for _, tok := range toks {
		res = append(res, w.f(tok))
	}
	return append(res, toks...)
}

func ChunkIDTokenizer(chk logproto.ChunkRef, t Tokenizer) *WrappedTokenizer {
	prefix := fmt.Sprintf("%d:%d:%d:", chk.From, chk.Through, chk.Checksum)
	return &WrappedTokenizer{
		t: t,
		f: func(tok Token) Token {
			var builder strings.Builder
			builder.Grow(256) // make this large once, so we don't need to reallocate for the two writes
			builder.WriteString(prefix)
			builder.WriteString(tok.Key)
			tok.Key = builder.String()
			return tok
		},
	}
}
