package main

import (
	"fmt"
	"github.com/go-kit/log/level"
	util_log "github.com/grafana/loki/pkg/util/log"
	"os"
	"strings"
)

// go build ./tools/tsdb/bloom-tester && BUCKET=19580 DIR=/tmp/loki-bloom-tester ./bloom-tester --config.file=/tmp/loki-config.yaml
func main() {
	writeMode := os.Getenv("WRITE_MODE")

	if strings.EqualFold(writeMode, "true") {
		fmt.Println("write mode")
		level.Info(util_log.Logger).Log("msg", "starting up in write mode")
		//time.Sleep(3000 * time.Second)
		execute()
	} else {
		fmt.Println("read mode")
		level.Info(util_log.Logger).Log("msg", "starting up in read mode")
		//time.Sleep(3000 * time.Second)

		executeRead()
	}
}
