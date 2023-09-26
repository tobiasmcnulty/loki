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

/*
type TokenB struct {
	Key, Value []byte
}
*/

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
	tokenBuffer    []Token  // buffer used for holding tokens
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
	t.tokenBuffer = make([]Token, 0, 1024)

	return t
}

func (t *ngramTokenizer) Tokens(line string) []Token {
	//res = make([]Token, 0, len(line))
	t.tokenBuffer = t.tokenBuffer[:0] // Reset the result slice
	var i int                         // rune index (not position that is measured in the range loop)
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
				t.tokenBuffer = append(t.tokenBuffer, Token{Key: string(t.runeBuffer), Value: ""})
			}
		}
		i++
	}
	return t.tokenBuffer
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
	t           Tokenizer
	f           func(Token) Token
	tokenBuffer []Token
	builder     strings.Builder
	prefix      []byte
}

func (w *WrappedTokenizer) Tokens(line string) []Token {
	w.tokenBuffer = w.tokenBuffer[:0] // Reset the result slice
	toks := w.t.Tokens(line)
	//res := make([]Token, 0, len(toks)*2)
	for _, tok := range toks {
		w.tokenBuffer = append(w.tokenBuffer, w.f(tok))
	}
	return append(w.tokenBuffer, toks...)
}

func ChunkIDTokenizer(chk logproto.ChunkRef, t Tokenizer) *WrappedTokenizer {
	//prefix := fmt.Sprintf("%d:%d:%d:", chk.From, chk.Through, chk.Checksum)
	p := make([]byte, 0, 256)
	b := strings.Builder{}
	p = fmt.Appendf(p, "%d:%d:%d:", chk.From, chk.Through, chk.Checksum)
	b.Grow(256)
	return &WrappedTokenizer{
		t: t,
		f: func(tok Token) Token {
			//var builder strings.Builder
			//builder.Grow(256) // make this large once, so we don't need to reallocate for the two writes
			b.Reset()
			b.WriteString(string(p))
			b.WriteString(tok.Key)
			tok.Key = b.String()
			return tok
		},
		tokenBuffer: make([]Token, 0, 1024),
		builder:     b,
		prefix:      p,
	}
}

func ChunkIDTokenizerHalfInit(t Tokenizer) *WrappedTokenizer {
	b := strings.Builder{}
	b.Grow(256)
	p := make([]byte, 0, 256)
	return &WrappedTokenizer{
		t:           t,
		tokenBuffer: make([]Token, 0, 1024),
		builder:     b,
		prefix:      p,
	}
}

func (w *WrappedTokenizer) reinit(chk logproto.ChunkRef) {
	//prefix := fmt.Sprintf("%d:%d:%d:", chk.From, chk.Through, chk.Checksum)
	w.prefix = w.prefix[:0]

	w.prefix = fmt.Appendf(w.prefix, "%d:%d:%d:", chk.From, chk.Through, chk.Checksum)
	w.f = func(tok Token) Token {
		//var builder strings.Builder
		//builder.Grow(256) // make this large once, so we don't need to reallocate for the two writes
		w.builder.Reset()
		w.builder.WriteString(string(w.prefix))
		w.builder.WriteString(tok.Key)
		tok.Key = w.builder.String()
		return tok
	}

}
