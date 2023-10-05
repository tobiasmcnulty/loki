package main

import (
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

type QueryExperiment struct {
	name         string
	searchString string
}

func NewQueryExperiment(name string, searchString string) QueryExperiment {
	return QueryExperiment{name: name,
		searchString: searchString}
}

var queryExperiments = []QueryExperiment{
	NewQueryExperiment("short_common_word", "trace"),
	//NewQueryExperiment("common_three_letter_word", "k8s"),
	NewQueryExperiment("specific_trace", "traceID=2279ea7e83dc812e"),
	NewQueryExperiment("specific_uuid", "8b6b631f-111f-4b29-b435-1e1e4e04aa8c"),
	NewQueryExperiment("longer_string_that_exists", "synthetic-monitoring-agent"),
	//NewQueryExperiment("longer_string_that_doesnt_exist", "abcdefghjiklmnopqrstuvwxyzzy1234567890"),
}

func executeRead() {
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

	err = analyzeRead(metrics, sampler, shipper, chunkClient, tableName, tenants, objectClient)
	helpers.ExitErr("analyzing", err)
}

func analyzeRead(metrics *Metrics, sampler Sampler, shipper indexshipper.IndexShipper, client client.Client, tableName string, tenants []string, objectClient client.ObjectClient) error {
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

	//var n int // count iterated series
	//reportEvery := 10 // report every n chunks
	//pool := newPool(runtime.NumCPU())
	pool := newPool(1)
	//searchString := os.Getenv("SEARCH_STRING")

	for _, tenant := range tenants {
		level.Info(util_log.Logger).Log("Analyzing tenant", tenant, "table", tableName)
		err := shipper.ForEach(
			context.Background(),
			tableName,
			tenant,
			func(isMultiTenantIndex bool, idx indexshipper_index.Index) error {
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
								//chunkTokenizer := ChunkIDTokenizerHalfInit(experiments[0].tokenizer)

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
									// iterate experiments
									for experimentIdx, experiment := range experiments {
										//bucketPrefix := "experiment-pod-counts-"
										//bucketPrefix := "experiment-read-tests-"
										bucketPrefix := os.Getenv("BUCKET_PREFIX")
										if strings.EqualFold(bucketPrefix, "") {
											bucketPrefix = "experiments-"
										}
										if sbfFileExists("bloomtests",
											fmt.Sprint(bucketPrefix, experimentIdx),
											os.Getenv("BUCKET"),
											tenant,
											fmt.Sprint(firstFP),
											fmt.Sprint(lastFP),
											fmt.Sprint(firstTimeStamp),
											fmt.Sprint(lastTimeStamp),
											objectClient) {

											sbf := readSBFFromObjectStorage("bloomtests",
												fmt.Sprint(bucketPrefix, experimentIdx),
												os.Getenv("BUCKET"),
												tenant,
												fmt.Sprint(firstFP),
												fmt.Sprint(lastFP),
												fmt.Sprint(firstTimeStamp),
												fmt.Sprint(lastTimeStamp),
												objectClient)

											for _, queryExperiment := range queryExperiments {
												for idx := range got {
													linesCounted := 0
													matchOnLine := 0
													chunkTokenizer := ChunkIDTokenizerHalfInit(experiment.tokenizer)

													chunkTokenizer.reinit(got[idx].ChunkRef)
													var tokenizer Tokenizer = chunkTokenizer
													if !experiment.encodeChunkID {
														tokenizer = experiment.tokenizer // so I don't have to change the lines of code below
													}
													//tokenizer := chunkTokenizer // so I don't have to change the lines of code below
													lc := got[idx].Data.(*chunkenc.Facade).LokiChunk()

													itr, err := lc.Iterator(
														context.Background(),
														time.Unix(0, 0),
														time.Unix(0, math.MaxInt64),
														logproto.FORWARD,
														log.NewNoopPipeline().ForStream(ls),
													)
													helpers.ExitErr("getting iterator", err)

													for itr.Next() && itr.Error() == nil {
														//fmt.Println("Line:", itr.Entry().Line)
														linesCounted++

														if strings.Contains(itr.Entry().Line, queryExperiment.searchString) {
															//fmt.Println(itr.Entry().Line)
															matchOnLine++
														} else {
														}
													}
													numMatches := 0
													tokens := tokenizer.Tokens(queryExperiment.searchString)
													for _, token := range tokens {
														if sbf.Test(token.Key) {
															numMatches++
														}
													}
													if matchOnLine > 0 {
														metrics.chunkMatchesPerSeries.WithLabelValues(experiment.name, queryExperiment.name).Inc()
														metrics.totalChunkMatchesPerSeries.WithLabelValues(experiment.name, queryExperiment.name).Add(float64(matchOnLine))
													}
													if numMatches == len(tokens) {
														//fullMatchOnSbf++
														metrics.sbfMatchesPerSeries.WithLabelValues(experiment.name, queryExperiment.name).Inc()
													}
													/*
														if (matchOnLine == 0) && (numMatches > 0) {
															metrics.falsePositivesPerSeries.Inc()
														}*/
													if matchOnLine > 0 && numMatches == 0 {
														metrics.missesPerSeries.WithLabelValues(experiment.name, queryExperiment.name).Inc()
													}
													/*
														fmt.Println("Num Matches:", numMatches)
														fmt.Println("len tokens", len(tokens))
														//fmt.Println("match on chunk", matchOnChunk)
														fmt.Println("match on line", matchOnLine)
														fmt.Println("Full match on sbf: ", fullMatchOnSbf)
														fmt.Println("Single match on sbf: ", singleMatchOnSbf)
														fmt.Println("Misses: ", misses)
														fmt.Println("lines counted", linesCounted)
														fmt.Println("------")
														time.Sleep(10 * time.Second)
													*/
													helpers.ExitErr("iterating chunks", itr.Error())
												}

												if len(got) > 0 {
													//metrics.counterPerSeries.Inc()
													metrics.lines.WithLabelValues(experiment.name).Add(float64(len(got)))
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

			},
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

func readSBFFromObjectStorage(location, prefix, period, tenant, startfp, endfp, startts, endts string, objectClient client.ObjectClient) *boom.ScalableBloomFilter {
	objectStoragePath := fmt.Sprintf("bloomtests/%s/%s/%s", prefix, period, tenant)

	sbf := experiments[0].bloom()
	closer, _, _ := objectClient.GetObject(context.Background(), fmt.Sprintf("%s/%s-%s-%s-%s-%s", objectStoragePath, startfp, endfp, startts, endts, "chksum"))
	sbf.ReadFrom(closer)
	return sbf
}
