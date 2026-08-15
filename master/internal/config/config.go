package config

import (
	"os"
	"strconv"
)

// DefaultInstallScriptURL is the canonical GitHub-hosted agent installer the
// master's generated scripts wrap around. Override with AGENT_INSTALL_SCRIPT_URL
// when the repo moves or a self-hosted mirror is preferred.
const DefaultInstallScriptURL = "https://raw.githubusercontent.com/sspulab106/xiaojiagent/main/scripts/install-agent.sh"

// Config holds master runtime settings. Every value can be overridden by the
// matching environment variable, so the same binary works in dev and prod.
type Config struct {
	Addr          string // HTTP listen address, e.g. ":8080"
	DBDriver      string // "sqlite" (default) | "postgres"
	DBPath        string // sqlite file path
	DBDSN         string // postgres DSN, used when DBDriver == "postgres"
	JWTSecret     string
	TokenTTLHours int
	// PublicURL is the externally reachable base URL of this master, used when
	// generating agent install scripts (so the script can download the agent
	// binary). Falls back to the incoming request origin when empty.
	PublicURL string
	// AgentBinaryDir is where the agent binary served at /downloads/agent is
	// looked up (default "data/bin").
	AgentBinaryDir string
	// VerifyNdpScript is the optional path to scripts/verify-ndp.sh, embedded
	// into generated install scripts so a freshly installed node auto-runs the
	// NDP self-check in subnet mode. Empty falls back to repo-relative paths.
	VerifyNdpScript string
	// InstallScriptURL is the GitHub-hosted install script (scripts/
	// install-agent.sh) that generated install scripts wrap around, so the
	// agent installer logic lives in the repo instead of the master binary.
	InstallScriptURL string
}

func Load() Config {
	return Config{
		Addr:            getEnv("ADDR", ":8080"),
		DBDriver:        getEnv("DB_DRIVER", "sqlite"),
		DBPath:          getEnv("DB_PATH", "data/master.db"),
		DBDSN:           getEnv("DB_DSN", ""),
		JWTSecret:       getEnv("JWT_SECRET", "dev-secret-change-me"),
		TokenTTLHours:   getEnvInt("TOKEN_TTL_HOURS", 72),
		PublicURL:       getEnv("MASTER_PUBLIC_URL", ""),
		AgentBinaryDir:  getEnv("AGENT_BINARY_DIR", "data/bin"),
		VerifyNdpScript: getEnv("VERIFY_NDP_SCRIPT", ""),
		InstallScriptURL: getEnv("AGENT_INSTALL_SCRIPT_URL", DefaultInstallScriptURL),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
