package main

import (
	"bufio"
	"bytes"
	"container/list"
	"context"
	"flag"
	"fmt"
	"github.com/grafana/dskit/services"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/prometheus/client_golang/prometheus"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-kit/log/level"
	"github.com/owen-d/BoomFilters/boom"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/log"
	"github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper"
	indexshipper_index "github.com/grafana/loki/pkg/storage/stores/indexshipper/index"
	"github.com/grafana/loki/pkg/storage/stores/tsdb"
	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/tools/tsdb/helpers"
)

func execute() {
	conf, svc, bucket, err := helpers.Setup()
	helpers.ExitErr("setting up", err)

	_, overrides, clientMetrics := helpers.DefaultConfigs()

	flag.Parse()

	objectClient, err := storage.NewObjectClient(conf.StorageConfig.TSDBShipperConfig.SharedStoreType, conf.StorageConfig, clientMetrics)
	helpers.ExitErr("creating object client", err)

	chunkClient := client.NewClient(objectClient, nil, conf.SchemaConfig)

	tableRanges := helpers.GetIndexStoreTableRanges(config.TSDBType, conf.SchemaConfig.Configs)

	openFn := func(p string) (indexshipper_index.Index, error) {
		return tsdb.OpenShippableTSDB(p, tsdb.IndexOpts{})
	}

	shipper, err := indexshipper.NewIndexShipper(
		conf.StorageConfig.TSDBShipperConfig.Config,
		objectClient,
		overrides,
		nil,
		openFn,
		tableRanges[len(tableRanges)-1],
		prometheus.WrapRegistererWithPrefix("loki_tsdb_shipper_", prometheus.DefaultRegisterer),
		util_log.Logger,
	)
	helpers.ExitErr("creating index shipper", err)

	tenants, tableName, err := helpers.ResolveTenants(objectClient, bucket, tableRanges)
	level.Info(util_log.Logger).Log("tenants", strings.Join(tenants, ","), "table", tableName)
	helpers.ExitErr("resolving tenants", err)

	//sampler, err := NewProbabilisticSampler(0.00008)
	sampler, err := NewProbabilisticSampler(1.000)
	helpers.ExitErr("creating sampler", err)

	metrics := NewMetrics(prometheus.DefaultRegisterer)

	level.Info(util_log.Logger).Log("msg", "starting server")
	err = services.StartAndAwaitRunning(context.Background(), svc)
	helpers.ExitErr("waiting for service to start", err)
	level.Info(util_log.Logger).Log("msg", "server started")

	err = analyze(metrics, sampler, shipper, chunkClient, tableName, tenants, objectClient)
	helpers.ExitErr("analyzing", err)
}

var (
	three      = newNGramTokenizer(3, 4, 0)
	threeSkip1 = newNGramTokenizer(3, 4, 1)
	threeSkip2 = newNGramTokenizer(3, 4, 2)
	threeSkip3 = newNGramTokenizer(3, 4, 3)

	onePctError  = func() *boom.ScalableBloomFilter { return boom.NewScalableBloomFilter(1024, 0.01, 0.8) }
	fivePctError = func() *boom.ScalableBloomFilter { return boom.NewScalableBloomFilter(1024, 0.05, 0.8) }
)

var experiments = []Experiment{
	// n > error > skip > index

	NewExperiment(
		"token=3skip0_error=1%_indexchunks=true",
		three,
		true,
		onePctError,
	),
	/*
		NewExperiment(
			"token=3skip0_error=1%_indexchunks=false",
			three,
			false,
			onePctError,
		),

		NewExperiment(
			"token=3skip1_error=1%_indexchunks=true",
			threeSkip1,
			true,
			onePctError,
		),
		NewExperiment(
			"token=3skip1_error=1%_indexchunks=false",
			threeSkip1,
			false,
			onePctError,
		),

		NewExperiment(
			"token=3skip2_error=1%_indexchunks=true",
			threeSkip2,
			true,
			onePctError,
		),
		NewExperiment(
			"token=3skip2_error=1%_indexchunks=false",
			threeSkip2,
			false,
			onePctError,
		),

		NewExperiment(
			"token=3skip0_error=5%_indexchunks=true",
			three,
			true,
			fivePctError,
		),
		NewExperiment(
			"token=3skip0_error=5%_indexchunks=false",
			three,
			false,
			fivePctError,
		),

		NewExperiment(
			"token=3skip1_error=5%_indexchunks=true",
			threeSkip1,
			true,
			fivePctError,
		),
		NewExperiment(
			"token=3skip1_error=5%_indexchunks=false",
			threeSkip1,
			false,
			fivePctError,
		),

		NewExperiment(
			"token=3skip2_error=5%_indexchunks=true",
			threeSkip2,
			true,
			fivePctError,
		),
		NewExperiment(
			"token=3skip2_error=5%_indexchunks=false",
			threeSkip2,
			false,
			fivePctError,
		),

	*/
}

func analyze(metrics *Metrics, sampler Sampler, shipper indexshipper.IndexShipper, client client.Client, tableName string, tenants []string, objectClient client.ObjectClient) error {
	metrics.tenants.Add(float64(len(tenants)))

	testerNumber := extractTesterNumber(os.Getenv("HOSTNAME"))
	if testerNumber == -1 {
		helpers.ExitErr("extracting hostname index number", nil)
	}
	numTesters, _ := strconv.Atoi(os.Getenv("NUM_TESTERS"))
	if numTesters == -1 {
		helpers.ExitErr("extracting total number of testers", nil)
	}
	level.Info(util_log.Logger).Log("msg", "starting analyze()", "tester", testerNumber, "total", numTesters)

	var n int // count iterated series
	//reportEvery := 10 // report every n chunks
	//pool := newPool(runtime.NumCPU())
	pool := newPool(8)

	for _, tenant := range tenants {
		level.Info(util_log.Logger).Log("Analyzing tenant", tenant, "table", tableName)
		err := shipper.ForEach(
			context.Background(),
			tableName,
			tenant,
			indexshipper_index.ForEachIndexCallback(func(isMultiTenantIndex bool, idx indexshipper_index.Index) error {
				if isMultiTenantIndex {
					return nil
				}

				casted := idx.(*tsdb.TSDBFile).Index.(*tsdb.TSDBIndex)
				_ = casted.ForSeries(
					context.Background(),
					nil, model.Earliest, model.Latest,
					func(ls labels.Labels, fp model.Fingerprint, chks []index.ChunkMeta) {
						chksCpy := make([]index.ChunkMeta, len(chks))
						copy(chksCpy, chks)
						pool.acquire(
							ls.Copy(),
							fp,
							chksCpy,
							func(ls labels.Labels, fp model.Fingerprint, chks []index.ChunkMeta) {

								metrics.series.Inc()
								metrics.chunks.Add(float64(len(chks)))

								if !sampler.Sample() {
									return
								}

								cache := NewHashSet(100000)
								chunkTokenizer := ChunkIDTokenizerHalfInit(experiments[0].tokenizer)

								splitChks := splitSlice(chks, numTesters)
								var transformed []chunk.Chunk
								var firstTimeStamp model.Time
								var lastTimeStamp model.Time
								var firstFP uint64
								var lastFP uint64
								for i, chk := range splitChks[testerNumber] {
									transformed = append(transformed, chunk.Chunk{
										ChunkRef: logproto.ChunkRef{
											Fingerprint: uint64(fp),
											UserID:      tenant,
											From:        chk.From(),
											Through:     chk.Through(),
											Checksum:    chk.Checksum,
										},
									})

									if i == 0 {
										firstTimeStamp = chk.From()
										firstFP = uint64(fp)
									}
									// yes I could do this just on the last one but I'm lazy
									lastTimeStamp = chk.Through()
									lastFP = uint64(fp)
								}

								got, err := client.GetChunks(
									context.Background(),
									transformed,
								)
								if err == nil {
									// record raw chunk sizes
									var chunkTotalUncompressedSize int
									for _, c := range got {
										chunkTotalUncompressedSize += c.Data.(*chunkenc.Facade).LokiChunk().UncompressedSize()
									}
									metrics.chunkSize.Observe(float64(chunkTotalUncompressedSize))
									n += len(got)

									// iterate experiments
									for experimentIdx, experiment := range experiments {
										//level.Info(util_log.Logger).Log("experiment", experiment.name)
										bucketPrefix := "experiment-pod-counts-"
										if !sbfFileExists("bloomtests",
											fmt.Sprint(bucketPrefix, experimentIdx),
											os.Getenv("BUCKET"),
											tenant,
											fmt.Sprint(firstFP),
											fmt.Sprint(lastFP),
											fmt.Sprint(firstTimeStamp),
											fmt.Sprint(lastTimeStamp),
											objectClient) {

											sbf := experiment.bloom()
											cache.Clear()

											// Iterate chunks
											var (
												lines, inserts, collisions float64
											)
											for idx := range got {
												// TODO this won't work if we add more experiments or stop encoding chunk id
												/*
													tokenizer := experiment.tokenizer
													if experiment.encodeChunkID {
														tokenizer = ChunkIDTokenizer(got[idx].ChunkRef, tokenizer)
													}
												*/
												chunkTokenizer.reinit(got[idx].ChunkRef)
												tokenizer := chunkTokenizer // so I don't have to change the lines of code below
												lc := got[idx].Data.(*chunkenc.Facade).LokiChunk()

												// Only report on the last experiment since they run serially
												/*if experimentIdx == len(experiments)-1 && (n+idx+1)%reportEvery == 0 {
												estimatedProgress := float64(fp) / float64(model.Fingerprint(math.MaxUint64)) * 100.
												level.Info(util_log.Logger).Log(
													"msg", "iterated",
													"progress", fmt.Sprintf("%.2f%%", estimatedProgress),
													"chunks", len(chks),
													"series", ls.String(),
												)
												}*/

												itr, err := lc.Iterator(
													context.Background(),
													time.Unix(0, 0),
													time.Unix(0, math.MaxInt64),
													logproto.FORWARD,
													log.NewNoopPipeline().ForStream(ls),
												)
												helpers.ExitErr("getting iterator", err)

												for itr.Next() && itr.Error() == nil {
													toks := tokenizer.Tokens(itr.Entry().Line)
													lines++
													for _, tok := range toks {
														if tok.Key != nil {
															cache.PutBoth(tok.Value, tok.Key)
															//cache.PutBytes(tok.Key)

															/*
																if !cache.Has(tok.Key) { //if !cache.Get(tok.Key) {
																	cache.Set(tok.Key, nil) //cache.Put(tok.Key)
																	if dup := sbf.TestAndAdd(tok.Key); dup {
																		collisions++
																	}
																	inserts++
																}

															*/
														}
													}
												}
												helpers.ExitErr("iterating chunks", itr.Error())
											}
											hashSet := cache.SurfaceMap()
											for _, v := range hashSet {
												if !sbf.TestAndAdd(v) {
													collisions++
												}
												inserts++
											}

											if len(got) > 0 {
												metrics.bloomSize.WithLabelValues(experiment.name).Observe(float64(sbf.Capacity() / 8))
												fillRatio := sbf.FillRatio()
												metrics.hammingWeightRatio.WithLabelValues(experiment.name).Observe(fillRatio)
												metrics.estimatedCount.WithLabelValues(experiment.name).Observe(
													float64(estimatedCount(sbf.Capacity(), sbf.FillRatio())),
												)
												metrics.lines.WithLabelValues(experiment.name).Add(lines)
												metrics.inserts.WithLabelValues(experiment.name).Add(inserts)
												metrics.collisions.WithLabelValues(experiment.name).Add(collisions)

												writeSBF(sbf,
													os.Getenv("DIR"),
													fmt.Sprint(bucketPrefix, experimentIdx),
													os.Getenv("BUCKET"),
													tenant,
													fmt.Sprint(firstFP),
													fmt.Sprint(lastFP),
													fmt.Sprint(firstTimeStamp),
													fmt.Sprint(lastTimeStamp),
													objectClient)

												if err != nil {
													helpers.ExitErr("writing sbf to file", err)
												}
											}
										}
									}
								} else {
									level.Info(util_log.Logger).Log("error getting chunks", err)
								}

								metrics.seriesKept.Inc()
								metrics.chunksKept.Add(float64(len(chks)))
								metrics.chunksPerSeries.Observe(float64(len(chks)))

							},
						)

					},
					labels.MustNewMatcher(labels.MatchEqual, "", ""),
				)

				return nil

			}),
		)
		helpers.ExitErr(fmt.Sprintf("iterating tenant %s", tenant), err)

	}

	level.Info(util_log.Logger).Log("msg", "waiting for workers to finish")
	pool.drain() // wait for workers to finishh
	level.Info(util_log.Logger).Log("msg", "waiting for final scrape")
	time.Sleep(30 * time.Second)         // allow final scrape
	time.Sleep(time.Duration(1<<63 - 1)) // wait forever
	return nil
}

// n ≈ −m ln(1 − p).
func estimatedCount(m uint, p float64) uint {
	return uint(-float64(m) * math.Log(1-p))
}

func splitSlice(slice []index.ChunkMeta, n int) [][]index.ChunkMeta {
	if n <= 0 {
		return nil // Return nil if n is not positive
	}

	// Calculate the size of each smaller slice
	sliceSize := len(slice) / n
	var result [][]index.ChunkMeta

	// Split the original slice into smaller slices
	for i := 0; i < n; i++ {
		startIndex := i * sliceSize
		endIndex := (i + 1) * sliceSize

		// Handle the last slice to include any remaining elements
		if i == n-1 {
			endIndex = len(slice)
		}

		result = append(result, slice[startIndex:endIndex])
	}

	return result
}

func extractTesterNumber(input string) int {
	// Split the input string by '-' to get individual parts
	parts := strings.Split(input, "-")

	// Extract the last part (the number)
	lastPart := parts[len(parts)-1]

	// Attempt to convert the last part to an integer
	extractedNumber, err := strconv.Atoi(lastPart)
	if err != nil {
		return -1
	}

	// Send the extracted number to the result channel
	return extractedNumber
}

func sbfFileExists(location, prefix, period, tenant, startfp, endfp, startts, endts string, objectClient client.ObjectClient) bool {
	checkSum := "chksum" // TODO, this won't work once we start putting out checksums
	dirPath := fmt.Sprintf("%s/%s/%s/%s", location, prefix, period, tenant)
	fullPath := fmt.Sprintf("%s/%s-%s-%s-%s-%s", dirPath, startfp, endfp, startts, endts, checkSum)

	result, _ := objectClient.ObjectExists(context.Background(), fullPath)

	return result
}

func writeSBF(sbf *boom.ScalableBloomFilter, location, prefix, period, tenant, startfp, endfp, startts, endts string, objectClient client.ObjectClient) {
	checkSum := "chksum"
	dirPath := fmt.Sprintf("%s/%s/%s/%s", location, prefix, period, tenant)
	objectStoragePath := fmt.Sprintf("bloomtests/%s/%s/%s", prefix, period, tenant)
	if err := os.MkdirAll(dirPath, os.ModePerm); err != nil {
		helpers.ExitErr("error creating sbf dir", err)
	}

	err := writeSBFToFile(sbf, fmt.Sprintf("%s/%s-%s-%s-%s-%s", dirPath, startfp, endfp, startts, endts, checkSum))
	if err != nil {
		helpers.ExitErr("writing sbf to file", err)
	}

	writeSBFToObjectStorage(sbf,
		fmt.Sprintf("%s/%s-%s-%s-%s-%s", objectStoragePath, startfp, endfp, startts, endts, checkSum),
		fmt.Sprintf("%s/%s-%s-%s-%s-%s", dirPath, startfp, endfp, startts, endts, checkSum),
		objectClient)
}

func writeSBFToFile(sbf *boom.ScalableBloomFilter, filename string) error {
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	bytesWritten, err := sbf.WriteTo(w)
	if err != nil {
		return err
	} else {
		level.Info(util_log.Logger).Log("msg", "wrote sbf", "bytes", bytesWritten, "file", filename)
	}
	err = w.Flush()
	return err
}

func writeSBFToObjectStorage(sbf *boom.ScalableBloomFilter, objectStorageFilename, localFilename string, objectClient client.ObjectClient) {
	// Probably a better way to do this than to reopen the file, but it's late
	file, err := os.Open(localFilename)
	if err != nil {
		level.Info(util_log.Logger).Log("error opening", localFilename, "error", err)
	}

	defer file.Close()

	fileInfo, _ := file.Stat()
	var size int64 = fileInfo.Size()

	buffer := make([]byte, size)

	// read file content to buffer
	file.Read(buffer)

	fileBytes := bytes.NewReader(buffer) // converted to io.ReadSeeker type

	objectClient.PutObject(context.Background(), objectStorageFilename, fileBytes)
	level.Info(util_log.Logger).Log("done writing", objectStorageFilename)
}

type LRUCache struct {
	capacity int
	cache    map[string]*list.Element
	list     *list.List
}

type Entry struct {
	key string
}

func NewLRUCache(capacity int) *LRUCache {
	return &LRUCache{
		capacity: capacity,
		cache:    make(map[string]*list.Element),
		list:     list.New(),
	}
}

func (c *LRUCache) Get(key string) bool {
	if elem, ok := c.cache[key]; ok {
		// Move the accessed element to the front of the list
		c.list.MoveToFront(elem)
		return true
	}
	return false
}

func (c *LRUCache) Put(key string) {
	if elem, ok := c.cache[key]; ok {
		// If the key already exists, move it to the front
		c.list.MoveToFront(elem)
	} else {
		// If the cache is full, remove the least recently used element
		if len(c.cache) >= c.capacity {
			// Get the least recently used element from the back of the list
			tailElem := c.list.Back()
			if tailElem != nil {
				deletedEntry := c.list.Remove(tailElem).(*Entry)
				delete(c.cache, deletedEntry.key)
			}
		}

		// Add the new key to the cache and the front of the list
		newEntry := &Entry{key}
		newElem := c.list.PushFront(newEntry)
		c.cache[key] = newElem
	}
}

func (c *LRUCache) Clear() {
	// Iterate through the list and remove all elements
	for elem := c.list.Front(); elem != nil; elem = elem.Next() {
		delete(c.cache, elem.Value.(*Entry).key)
	}

	// Clear the list
	c.list.Init()
}

type LRUCache2 struct {
	capacity int
	cache    map[string]*LRUNode2
	head     *LRUNode2
	tail     *LRUNode2
}

type LRUNode2 struct {
	key string
	//value interface{}
	prev *LRUNode2
	next *LRUNode2
}

func NewLRUCache2(capacity int) *LRUCache2 {
	return &LRUCache2{
		capacity: capacity,
		cache:    make(map[string]*LRUNode2),
	}
}

func (c *LRUCache2) Get(key string) bool {
	if node, ok := c.cache[key]; ok {
		// Move the accessed element to the front
		c.moveToFront(node)
		return true
	}
	return false
}

func (c *LRUCache2) Put(key string) {
	if node, ok := c.cache[key]; ok {
		// If the key already exists, update the value and move it to the front
		c.moveToFront(node)
	} else {
		// If the cache is full, remove the least recently used element
		if len(c.cache) >= c.capacity {
			c.removeTail()
		}

		// Add the new key to the cache and the front
		newNode := &LRUNode2{key: key}
		c.cache[key] = newNode
		c.addToFront(newNode)
	}
}

func (c *LRUCache2) moveToFront(node *LRUNode2) {
	if node == c.head {
		return
	}
	if node == c.tail {
		c.tail = node.prev
		c.tail.next = nil
	} else {
		node.prev.next = node.next
		node.next.prev = node.prev
	}
	c.addToFront(node)
}

func (c *LRUCache2) addToFront(node *LRUNode2) {
	node.prev = nil
	node.next = c.head
	if c.head != nil {
		c.head.prev = node
	}
	c.head = node
	if c.tail == nil {
		c.tail = node
	}
}

func (c *LRUCache2) removeTail() {
	if c.tail == nil {
		return
	}
	delete(c.cache, c.tail.key)
	if c.tail == c.head {
		c.head = nil
		c.tail = nil
	} else {
		c.tail = c.tail.prev
		c.tail.next = nil
	}
}

type LRUCache4 struct {
	capacity int
	cache    map[string]*list.Element
	list     *list.List
}

type Entry4 struct {
	key   string
	value []byte
}

func NewLRUCache4(capacity int) *LRUCache4 {
	return &LRUCache4{
		capacity: capacity,
		cache:    make(map[string]*list.Element),
		list:     list.New(),
	}
}

func (c *LRUCache4) Get(value []byte) bool {
	key := string(value)
	if elem, ok := c.cache[key]; ok {
		// Move the accessed element to the front of the list
		c.list.MoveToFront(elem)
		return true
	}
	return false
}

func (c *LRUCache4) GetString(key string) (bool, []byte) {
	if elem, ok := c.cache[key]; ok {
		// Move the accessed element to the front of the list
		c.list.MoveToFront(elem)
		return true, elem.Value.(*Entry4).value
	}
	return false, nil
}

func (c *LRUCache4) Put(value []byte) {
	key := string(value)

	if elem, ok := c.cache[key]; ok {
		// If the key already exists, move it to the front
		c.list.MoveToFront(elem)
	} else {
		// If the cache is full, remove the least recently used element
		if len(c.cache) >= c.capacity {
			// Get the least recently used element from the back of the list
			tailElem := c.list.Back()
			if tailElem != nil {
				deletedEntry := c.list.Remove(tailElem).(*Entry4)
				delete(c.cache, deletedEntry.key)
			}
		}

		// Add the new key to the cache and the front of the list
		newEntry := &Entry4{key, value}
		newElem := c.list.PushFront(newEntry)
		c.cache[key] = newElem
	}
}

func (c *LRUCache4) Clear() {
	// Iterate through the list and remove all elements
	for elem := c.list.Front(); elem != nil; elem = elem.Next() {
		delete(c.cache, elem.Value.(*Entry4).key)
	}

	// Clear the list
	c.list.Init()
}

type HashSet struct {
	capacity int
	cache    map[string][]byte
}

func NewHashSet(capacity int) *HashSet {
	return &HashSet{
		capacity: capacity,
		cache:    make(map[string][]byte),
	}
}

func (c *HashSet) Get(key string) (bool, []byte) {
	if value, ok := c.cache[key]; ok {
		return true, value
	}
	return false, nil
}

func (c *HashSet) Put(key string) {
	c.cache[key] = []byte(key)
}

func (c *HashSet) PutBytes(value []byte) {
	key := string(value)
	c.cache[key] = []byte(key)
}

func (c *HashSet) PutBoth(key string, value []byte) {
	c.cache[key] = value
}

func (c *HashSet) SurfaceMap() map[string][]byte {
	return c.cache
}

func (c *HashSet) Clear() {
	for k := range c.cache {
		delete(c.cache, k)
	}
}
