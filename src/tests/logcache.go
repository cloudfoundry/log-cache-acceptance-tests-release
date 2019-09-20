package tests

import (
	"code.cloudfoundry.org/log-cache/pkg/rpc/logcache_v1"
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"strings"
	"time"

	"code.cloudfoundry.org/go-loggregator/rpc/loggregator_v2"
	"code.cloudfoundry.org/log-cache/pkg/client"
	uuid "github.com/nu7hatch/gouuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"google.golang.org/grpc"
)

var _ = Describe("LogCache", func() {
	var (
		logCacheClient *client.Client
		cfg            *TestConfig
	)

	var minuteRangeQuery = func(query string, duration time.Duration, opts ...client.PromQLOption) []*logcache_v1.PromQL_Point {
		now := time.Now()
		ctx, _ := context.WithTimeout(context.Background(), cfg.DefaultTimeout)

		opts = append(opts,
			client.WithPromQLStart(now.Add(-duration)),
			client.WithPromQLEnd(now),
		)
		result, err := logCacheClient.PromQLRange(
			ctx,
			query,
			opts...,
		)
		Expect(err).ToNot(HaveOccurred())

		matrix := result.GetMatrix()
		if matrix == nil || len(matrix.GetSeries()) == 0 || matrix.GetSeries()[0] == nil {
			return nil
		}

		return matrix.GetSeries()[0].GetPoints()
	}

	Context("with grpc client", func() {
		BeforeEach(func() {
			cfg = Config()
			logCacheClient = client.NewClient(
				cfg.LogCacheAddr,
				client.WithViaGRPC(
					grpc.WithTransportCredentials(
						cfg.TLS.Credentials("log-cache"),
					),
				),
			)
		})

		It("makes emitted logs available", func() {
			s := newUUID()

			start := time.Now()
			emitLogs([]string{s})
			end := time.Now()

			received := countEnvelopes(start, end, logCacheClient.Read, s, 10000)
			Expect(received).To(BeNumerically(">=", 9900))
		})

		It("lists the available source ids that log cache has persisted", func() {
			s := newUUID()

			emitLogs([]string{s})

			ctx, _ := context.WithTimeout(context.Background(), cfg.DefaultTimeout)
			meta, err := logCacheClient.Meta(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(meta).To(HaveKey(s))

			count := meta[s].GetCount()
			Expect(count).To(BeNumerically(">=", 9900))
		})

		It("can query for emitted metrics with PromQL™ Instant Queries©", func() {
			s := newUUID()

			emitGauges([]string{s})

			query := fmt.Sprintf("metric{source_id=%q}", s)
			ctx, _ := context.WithTimeout(context.Background(), cfg.DefaultTimeout)
			result, err := logCacheClient.PromQL(ctx, query)
			Expect(err).ToNot(HaveOccurred())

			vector := result.GetVector()
			Expect(vector.Samples).To(HaveLen(1))
			Expect(vector.Samples[0].Point.GetValue()).To(Equal(10.0))
		})

		It("can do math on emitted metrics with PromQL™ Instant Queries©", func() {
			s := newUUID()
			s2 := newUUID()

			emitGauges([]string{s, s2})

			query := fmt.Sprintf("metric{source_id=%q} + ignoring (source_id) metric{source_id=%q}", s, s2)
			ctx, _ := context.WithTimeout(context.Background(), cfg.DefaultTimeout)
			result, err := logCacheClient.PromQL(ctx, query)
			Expect(err).ToNot(HaveOccurred())

			vector := result.GetVector()
			Expect(vector.Samples).To(HaveLen(1))
			Expect(vector.Samples[0].Point.GetValue()).To(Equal(20.0))
		})

		It("performs aggregations with PromQL™ Range Queries©", func() {
			s := newUUID()

			emitGauges([]string{s})

			Eventually(func() float64 {
				query := fmt.Sprintf("sum_over_time(metric{source_id=%q}[10s])", s)
				points := minuteRangeQuery(query, time.Minute, client.WithPromQLStep("5s"))

				var sum float64
				for _, point := range points {
					sum += point.GetValue()
				}

				return sum
			}, 60).Should(BeEquivalentTo(2 * 100000.0))
		})

		It("validates that CPU for the doppler VM is under 50%", func() {
			query := `avg_over_time(system_cpu_sys{source_id="system_metrics_agent", job="doppler"}[1m])`
			points := minuteRangeQuery(query, time.Minute, client.WithPromQLStep("10s"))

			var sum float64
			for _, point := range points {
				sum += point.GetValue()
			}
			Expect(sum / float64(len(points))).To(BeNumerically("<", 50))
		})

		It("validates that memory for the doppler VM is under 90%", func() {
			query := `avg_over_time(system_mem_percent{source_id="system_metrics_agent", job="doppler"}[1m])`
			points := minuteRangeQuery(query, time.Minute, client.WithPromQLStep("10s"))

			var sum float64
			for _, point := range points {
				sum += point.GetValue()
			}
			Expect(sum / float64(len(points))).To(BeNumerically("<", 90))
		})

		It("validates that swapping for the doppler VM is under 5%", func() {
			query := `avg_over_time(system_swap_percent{source_id="system_metrics_agent", job="doppler"}[1m])`
			points := minuteRangeQuery(query, time.Minute, client.WithPromQLStep("10s"))

			var sum float64
			for _, point := range points {
				sum += point.GetValue()
			}
			Expect(sum / float64(len(points))).To(BeNumerically("<", 5))
		})
	})

	Context("with http client", func() {
		BeforeEach(func() {
			cfg = Config()
			logCacheClient = client.NewClient(
				cfg.LogCacheCFAuthProxyURL,
				client.WithHTTPClient(newOauth2HTTPClient(cfg)),
			)
		})

		It("makes emitted logs available", func() {
			s := newUUID()

			start := time.Now()
			emitLogs([]string{s})
			end := time.Now()

			received := countEnvelopes(start, end, logCacheClient.Read, s, 10000)
			Expect(received).To(BeNumerically(">=", 9000))
		})

		It("lists the available source ids that log cache has persisted", func() {
			s := newUUID()

			emitLogs([]string{s})

			ctx, _ := context.WithTimeout(context.Background(), cfg.DefaultTimeout)
			meta, err := logCacheClient.Meta(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(meta).To(HaveKey(s))

			count := meta[s].GetCount()
			Expect(count).To(BeNumerically(">=", 9900))
		})

		It("can query for emitted metrics with PromQL™", func() {
			s := newUUID()

			emitGauges([]string{s})

			query := fmt.Sprintf("metric{source_id=%q}", s)
			ctx, _ := context.WithTimeout(context.Background(), cfg.DefaultTimeout)
			result, err := logCacheClient.PromQL(ctx, query)
			Expect(err).ToNot(HaveOccurred())

			vector := result.GetVector()
			Expect(vector.Samples).To(HaveLen(1))
			Expect(vector.Samples[0].Point.GetValue()).To(Equal(10.0))
		})

		It("can do math on emitted metrics with PromQL™", func() {
			s := newUUID()
			s2 := newUUID()

			emitGauges([]string{s, s2})

			query := fmt.Sprintf("metric{source_id=%q} + ignoring (source_id) metric{source_id=%q}", s, s2)
			ctx, _ := context.WithTimeout(context.Background(), cfg.DefaultTimeout)
			result, err := logCacheClient.PromQL(ctx, query)
			Expect(err).ToNot(HaveOccurred())

			vector := result.GetVector()
			Expect(vector.Samples).To(HaveLen(1))
			Expect(vector.Samples[0].Point.GetValue()).To(Equal(20.0))
		})

		It("performs aggregations with PromQL™", func() {
			s := newUUID()

			emitGauges([]string{s})

			// log-cache-emitter emits 10,000 gauges with a value of 10.0
			performQuery := func() float64 {
				query := fmt.Sprintf("sum_over_time(metric{source_id=%q}[5m])", s)
				ctx, _ := context.WithTimeout(context.Background(), cfg.DefaultTimeout)
				result, err := logCacheClient.PromQL(ctx, query)
				Expect(err).ToNot(HaveOccurred())

				vector := result.GetVector()
				Expect(vector.Samples).To(HaveLen(1))
				return vector.Samples[0].Point.GetValue()
			}

			// wait for gauges to be processed
			Eventually(performQuery, 60).Should(BeEquivalentTo(100000.0))
			Consistently(performQuery, 30).Should(BeEquivalentTo(100000.0))
		})

		It("performs aggregations with PromQL™ Range Queries©", func() {
			s := newUUID()

			emitGauges([]string{s})

			Eventually(func() float64 {
				query := fmt.Sprintf("sum_over_time(metric{source_id=%q}[10s])", s)
				points := minuteRangeQuery(query, time.Minute, client.WithPromQLStep("5s"))

				var sum float64
				for _, point := range points {
					sum += point.GetValue()
				}

				return sum
			}, 60).Should(BeEquivalentTo(2 * 100000.0))
		})

		It("validates that CPU for the doppler VM is under 50%", func() {
			query := `avg_over_time(system_cpu_sys{source_id="system_metrics_agent", job="doppler"}[2m])`
			points := minuteRangeQuery(query, 2*time.Minute, client.WithPromQLStep("10s"))

			var sum float64
			for _, point := range points {
				sum += point.GetValue()
			}
			Expect(sum / float64(len(points))).To(BeNumerically("<", 50))
		})

		It("validates that memory for the doppler VM is under 90%", func() {
			query := `avg_over_time(system_mem_percent{source_id="system_metrics_agent", job="doppler"}[2m])`
			points := minuteRangeQuery(query, 2*time.Minute, client.WithPromQLStep("10s"))

			var sum float64
			for _, point := range points {
				sum += point.GetValue()
			}
			Expect(sum / float64(len(points))).To(BeNumerically("<", 90))
		})

		It("validates that swapping for the doppler VM is under 5%", func() {
			query := `avg_over_time(system_swap_percent{source_id="system_metrics_agent", job="doppler"}[2m])`
			points := minuteRangeQuery(query, 2*time.Minute, client.WithPromQLStep("10s"))

			var sum float64
			for _, point := range points {
				sum += point.GetValue()
			}
			Expect(sum / float64(len(points))).To(BeNumerically("<", 5))
		})
	})
})

func newOauth2HTTPClient(cfg *TestConfig) *client.Oauth2HTTPClient {
	oauthClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: cfg.SkipCertVerify,
			},
		},
		Timeout: cfg.DefaultTimeout,
	}

	return client.NewOauth2HTTPClient(
		cfg.UAAURL,
		cfg.ClientID,
		cfg.ClientSecret,
		client.WithOauth2HTTPClient(oauthClient),
	)
}

func emitLogs(sourceIDs []string) {
	cfg := Config()
	query := strings.Join(sourceIDs, "&sourceIDs=")
	logUrl := fmt.Sprintf("http://%s/emit-logs?sourceIDs=%s", cfg.LogEmitterAddr, query)

	res, err := http.Get(logUrl)

	Expect(err).ToNot(HaveOccurred())
	Expect(res.StatusCode).To(Equal(http.StatusOK))
	waitForLogs()
}

func emitGauges(sourceIDs []string) {
	cfg := Config()
	query := strings.Join(sourceIDs, "&sourceIDs=")
	logUrl := fmt.Sprintf("http://%s/emit-gauges?sourceIDs=%s", cfg.LogEmitterAddr, query)

	res, err := http.Get(logUrl)

	Expect(err).ToNot(HaveOccurred())
	Expect(res.StatusCode).To(Equal(http.StatusOK))
	waitForLogs()
}

func waitForLogs() {
	cfg := Config()
	time.Sleep(cfg.WaitForLogsTimeout)
}

func countEnvelopes(start, end time.Time, reader client.Reader, sourceID string, totalEmitted int) int {
	var receivedCount int
	ctx, _ := context.WithTimeout(context.Background(), Config().DefaultTimeout)
	client.Walk(
		ctx,
		sourceID,
		func(envelopes []*loggregator_v2.Envelope) bool {
			receivedCount += len(envelopes)
			return receivedCount < totalEmitted
		},
		reader,
		client.WithWalkStartTime(start),
		client.WithWalkEndTime(end),
		client.WithWalkBackoff(client.NewRetryBackoff(50*time.Millisecond, 100)),
	)

	return receivedCount
}

func newUUID() string {
	u, err := uuid.NewV4()
	Expect(err).ToNot(HaveOccurred())

	return u.String()
}
