package tests

import (
	"log"
	"time"

	envstruct "code.cloudfoundry.org/go-envstruct"
)

type TestConfig struct {
	LogCacheAddr           string `env:"LOG_CACHE_ADDR,    required, report"`
	LogEmitterAddr         string `env:"LOG_EMITTER_ADDR,  required, report"`
	LogCacheCFAuthProxyURL string `env:"LOG_CACHE_CF_AUTH_PROXY_URL,  required, report"`

	TLS TLS

	UAAURL       string `env:"UAA_URL, required, report"`
	ClientID     string `env:"CLIENT_ID, required, report"`
	ClientSecret string `env:"CLIENT_SECRET, required, noreport"`

	SkipCertVerify bool `env:"SKIP_CERT_VERIFY, report"`

	DefaultTimeout     time.Duration `env:"DEFAULT_TIMEOUT, report"`
	WaitForLogsTimeout time.Duration `env:"LOG_EMIT_TIMEOUT, report"`
}

var config *TestConfig

func LoadConfig() (*TestConfig, error) {
	config := &TestConfig{
		DefaultTimeout:     10 * time.Second,
		WaitForLogsTimeout: 10 * time.Second,
	}
	err := envstruct.Load(config)
	if err != nil {
		return nil, err
	}
	envstruct.WriteReport(config)
	return config, nil
}

func Config() *TestConfig {
	if config != nil {
		return config
	}

	cfg, err := LoadConfig()
	if err != nil {
		log.Fatalf("failed to load log cache test config: %s", err)
	}
	config = cfg
	return config
}
