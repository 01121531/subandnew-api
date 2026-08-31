package common

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/01121531/subandnew-api/constant"
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

	DebugEnabled = strings.EqualFold(strings.TrimSpace(os.Getenv("DEBUG")), "true")
	if err := initSessionSecurity(); err != nil {
		log.Fatal(err)
	}
	if value := os.Getenv("CRYPTO_SECRET"); value != "" {
		CryptoSecret = value
	} else {
		CryptoSecret = SessionSecret
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

const minimumSessionSecretBytes = 32

var insecureSessionSecrets = map[string]struct{}{
	"change-me":                         {},
	"random_string":                     {},
	"replace-me":                        {},
	"replace-with-a-long-random-string": {},
	"secret":                            {},
	"session_secret":                    {},
	"your-session-secret":               {},
}

func initSessionSecurity() error {
	secret := os.Getenv("SESSION_SECRET")
	if err := validateSessionSecret(secret); err != nil {
		return err
	}
	SessionSecret = secret
	if err := InitSessionCookieSettings(); err != nil {
		return err
	}
	if isProductionEnvironment() && !SessionCookieSecure {
		return fmt.Errorf("APP_ENV=production requires SESSION_COOKIE_SECURE=true and SESSION_COOKIE_TRUSTED_URL")
	}
	return nil
}

func validateSessionSecret(secret string) error {
	if secret == "" {
		return fmt.Errorf("SESSION_SECRET is required and must contain at least %d bytes", minimumSessionSecretBytes)
	}
	if secret != strings.TrimSpace(secret) {
		return fmt.Errorf("SESSION_SECRET must not contain leading or trailing whitespace")
	}
	if len([]byte(secret)) < minimumSessionSecretBytes {
		return fmt.Errorf("SESSION_SECRET must contain at least %d bytes", minimumSessionSecretBytes)
	}
	if _, insecure := insecureSessionSecrets[strings.ToLower(secret)]; insecure {
		return fmt.Errorf("SESSION_SECRET must not use a public example value")
	}
	if repeatedSecretPattern(secret) {
		return fmt.Errorf("SESSION_SECRET must not use a repeated pattern")
	}
	return nil
}

func repeatedSecretPattern(secret string) bool {
	for patternLength := 1; patternLength <= len(secret)/2; patternLength++ {
		if len(secret)%patternLength != 0 {
			continue
		}
		pattern := secret[:patternLength]
		if strings.Repeat(pattern, len(secret)/patternLength) == secret {
			return true
		}
	}
	return false
}

func isProductionEnvironment() bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv("APP_ENV")))
	return value == "production" || value == "prod"
}
