package common

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/01121531/HUICHUAN-AI/constant"
)

var (
	Port         = flag.Int("port", 3000, "the listening port")
	PrintVersion = flag.Bool("version", false, "print version and exit")
	PrintHelp    = flag.Bool("help", false, "print help and exit")
	LogDir       = flag.String("log-dir", "./logs", "specify the log directory")
)

func printHelp() {
	fmt.Println("SubAndNew API " + Version + " - multi-instance New API and Sub2API control plane")
	fmt.Println("Repository: https://github.com/01121531/subandnew-api")
	fmt.Println("Usage: subandnew-api [--port <port>] [--log-dir <directory>] [--version] [--help]")
}

func InitEnv() {
	flag.Parse()
	if value := os.Getenv("VERSION"); value != "" {
		Version = value
	}
	if *PrintVersion {
		fmt.Println(Version)
		os.Exit(0)
	}
	if *PrintHelp {
		printHelp()
		os.Exit(0)
	}

	if value := os.Getenv("SESSION_SECRET"); value != "" {
		if value == "random_string" {
			log.Fatal("SESSION_SECRET must not use the default random_string value")
		}
		SessionSecret = value
	}
	if value := os.Getenv("CRYPTO_SECRET"); value != "" {
		CryptoSecret = value
	} else {
		CryptoSecret = SessionSecret
	}
	if err := InitSessionCookieSettings(); err != nil {
		log.Fatal(err)
	}
	if value := os.Getenv("SQLITE_PATH"); value != "" {
		SQLitePath = value
	}
	if *LogDir != "" {
		absolute, err := filepath.Abs(*LogDir)
		if err != nil {
			log.Fatal(err)
		}
		*LogDir = absolute
		if err := os.MkdirAll(*LogDir, 0o755); err != nil {
			log.Fatal(err)
		}
	}

	DebugEnabled = os.Getenv("DEBUG") == "true"
	IsMasterNode = os.Getenv("NODE_TYPE") != "slave"
	initNodeNameIdentity()
	TLSInsecureSkipVerify = GetEnvOrDefaultBool("TLS_INSECURE_SKIP_VERIFY", false)
	if TLSInsecureSkipVerify {
		if transport, ok := http.DefaultTransport.(*http.Transport); ok && transport != nil {
			if transport.TLSClientConfig != nil {
				transport.TLSClientConfig.InsecureSkipVerify = true
			} else {
				transport.TLSClientConfig = InsecureTLSConfig
			}
		}
	}
	SMTPStartTLSEnabled = GetEnvOrDefaultBool("SMTP_STARTTLS_ENABLE", GetEnvOrDefaultBool("SMTP_STARTTLS_ENABLED", false))
	SMTPInsecureSkipVerify = GetEnvOrDefaultBool("SMTP_INSECURE_SKIP_VERIFY", GetEnvOrDefaultBool("SMTP_TLS_INSECURE_SKIP_VERIFY", false))
	SyncFrequency = GetEnvOrDefault("SYNC_FREQUENCY", 60)
	GlobalApiRateLimitEnable = GetEnvOrDefaultBool("GLOBAL_API_RATE_LIMIT_ENABLE", true)
	GlobalApiRateLimitNum = GetEnvOrDefault("GLOBAL_API_RATE_LIMIT", 360)
	GlobalApiRateLimitDuration = int64(GetEnvOrDefault("GLOBAL_API_RATE_LIMIT_DURATION", 180))
	GlobalWebRateLimitEnable = GetEnvOrDefaultBool("GLOBAL_WEB_RATE_LIMIT_ENABLE", true)
	GlobalWebRateLimitNum = GetEnvOrDefault("GLOBAL_WEB_RATE_LIMIT", 120)
	GlobalWebRateLimitDuration = int64(GetEnvOrDefault("GLOBAL_WEB_RATE_LIMIT_DURATION", 180))
	CriticalRateLimitEnable = GetEnvOrDefaultBool("CRITICAL_RATE_LIMIT_ENABLE", true)
	CriticalRateLimitNum = GetEnvOrDefault("CRITICAL_RATE_LIMIT", 20)
	CriticalRateLimitDuration = int64(GetEnvOrDefault("CRITICAL_RATE_LIMIT_DURATION", 20*60))
	constant.AnonymousRequestBodyLimitKB = GetEnvOrDefault("ANONYMOUS_REQUEST_BODY_LIMIT_KB", 512)
}
