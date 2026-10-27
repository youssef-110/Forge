// Command loadgen submits a mix of jobs so the dashboard has something to show.
//
//	go run ./cmd/loadgen -url http://localhost:8080 -rate 20 -duration 2m
package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"math/rand/v2"
	"net/http"
	"time"
)

func main() {
	url := flag.String("url", "http://localhost:8080", "queue API base URL")
	rate := flag.Int("rate", 10, "jobs per second")
	d := flag.Duration("duration", time.Minute, "how long to run")
	flag.Parse()

	mix := []string{
		`{"type":"echo","payload":{"message":"hi"},"priority":%d}`,
		`{"type":"sleep","payload":{"ms":%d},"priority":5}`,
		`{"type":"flaky","payload":{"fail_rate":0.3},"priority":%d}`,
		`{"type":"synthgen","payload":{"rows":20000,"seed":%d},"priority":%d}`,
	}
	tick := time.NewTicker(time.Second / time.Duration(*rate))
	defer tick.Stop()
	end := time.After(*d)
	var sent, failed int
	for {
		select {
		case <-end:
			log.Printf("sent %d jobs, %d rejected", sent, failed)
			return
		case <-tick.C:
		}
		var body string
		switch i := rand.IntN(len(mix)); i {
		case 1:
			body = fmt.Sprintf(mix[i], 50+rand.IntN(500))
		case 3:
			body = fmt.Sprintf(mix[i], rand.IntN(1000), rand.IntN(10))
		default:
			body = fmt.Sprintf(mix[i], rand.IntN(10))
		}
		res, err := http.Post(*url+"/v1/jobs", "application/json", bytes.NewBufferString(body))
		if err != nil || res.StatusCode != http.StatusCreated {
			failed++
		} else {
			sent++
		}
		if res != nil {
			res.Body.Close()
		}
	}
}
