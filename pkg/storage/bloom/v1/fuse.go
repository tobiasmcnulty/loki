package v1

import (
	"github.com/efficientgo/core/errors"
	"github.com/prometheus/common/model"
)

type Request struct {
	Fp       model.Fingerprint
	Chks     ChunkRefs
	Searches [][]byte
	Response chan<- Output
}

// Output represents a chunk that failed to pass all searches
// and must be downloaded
type Output struct {
	Fp   model.Fingerprint
	Chks ChunkRefs
}

// Fuse combines multiple requests into a single loop iteration
// over the data set and returns the corresponding outputs
// TODO(owen-d): better async control
func (bq *BlockQuerier) Fuse(inputs []PeekingIterator[Request]) *FusedQuerier {
	return NewFusedQuerier(bq, inputs)
}

type FusedQuerier struct {
	bq     *BlockQuerier
	inputs Iterator[[]Request]
}

func NewFusedQuerier(bq *BlockQuerier, inputs []PeekingIterator[Request]) *FusedQuerier {
	heap := NewHeapIterator[Request](
		func(a, b Request) bool {
			return a.Fp < b.Fp
		},
		inputs...,
	)

	merging := NewDedupingIter[Request, []Request](
		func(a Request, b []Request) bool {
			return a.Fp == b[0].Fp
		},
		func(a Request) []Request { return []Request{a} },
		func(a Request, b []Request) []Request {
			return append(b, a)
		},
		NewPeekingIter[Request](heap),
	)
	return &FusedQuerier{
		bq:     bq,
		inputs: merging,
	}
}

func (fq *FusedQuerier) Run() error {
	for fq.inputs.Next() {
		// find all queries for the next relevant fingerprint
		nextBatch := fq.inputs.At()

		fp := nextBatch[0].Fp

		// advance the series iterator to the next fingerprint
		if err := fq.bq.Seek(fp); err != nil {
			return errors.Wrap(err, "seeking to fingerprint")
		}

		if !fq.bq.series.Next() {
			// no more series, we're done since we're iterating desired fingerprints in order
			return nil
		}

		series := fq.bq.series.At()
		if series.Fingerprint != fp {
			// fingerprint not found, can't remove chunks
			for _, input := range nextBatch {
				input.Response <- Output{
					Fp:   fp,
					Chks: input.Chks,
				}
			}
		}

		// Now that we've found the series, we need to find the unpack the bloom
		fq.bq.blooms.Seek(series.Offset)
		if !fq.bq.blooms.Next() {
			// fingerprint not found, can't remove chunks
			for _, input := range nextBatch {
				input.Response <- Output{
					Fp:   fp,
					Chks: input.Chks,
				}
			}
			continue
		}

		bloom := fq.bq.blooms.At()
		// test every input against this chunk
	inputLoop:
		for _, input := range nextBatch {
			mustCheck, inBlooms := input.Chks.Compare(series.Chunks, true)

			// First, see if the search passes the series level bloom before checking for chunks individually
			for _, search := range input.Searches {
				if !bloom.Test(search) {
					// the entire series bloom didn't pass one of the searches,
					// so we can skip checking chunks individually.
					// We still return all chunks that are not included in the bloom
					// as they may still have the data
					input.Response <- Output{
						Fp:   fp,
						Chks: mustCheck,
					}
					continue inputLoop
				}
			}

		chunkLoop:
			for _, chk := range inBlooms {
				for _, search := range input.Searches {
					// TODO(owen-d): meld chunk + search into a single byte slice from the block schema
					var combined = search

					if !bloom.ScalableBloomFilter.Test(combined) {
						continue chunkLoop
					}
				}
				// chunk passed all searches, add to the list of chunks to download
				mustCheck = append(mustCheck, chk)

			}

			input.Response <- Output{
				Fp:   fp,
				Chks: mustCheck,
			}
		}

	}

	return nil
}
