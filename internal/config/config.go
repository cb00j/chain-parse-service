package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Environment represents the deployment environment.
type Environment string

const (
	EnvDev     Environment = "dev"
	EnvStaging Environment = "staging"
	EnvProd    Environment = "prod"
)

// APIConfig API服务配置
type APIConfig struct {
	Port         int      `yaml:"port"`
	ReadTimeout  int      `yaml:"read_timeout"`
	WriteTimeout int      `yaml:"write_timeout"`
	AllowOrigins []string `yaml:"allow_origins"`
	RateLimit    float64  `yaml:"rate_limit"`
	RateBurst    int      `yaml:"rate_burst"`
}

// QuoteAssetConfig 报价资产配置（用于价格/价值计算）
type QuoteAssetConfig struct {
	Name string `yaml:"name"`
	Addr string `yaml:"addr"`
	Rank int    `yaml:"rank"` // 优先级越高越优先作为计价基准，100=USD稳定币
}

// Config 应用配置
type Config struct {
	API         APIConfig                 `yaml:"api"`
	Redis       RedisConfig               `yaml:"redis"`
	Chains      map[string]ChainConfig    `yaml:"chains"`
	Protocols   map[string]ProtocolConfig `yaml:"protocols"`
	Processor   ProcessorConfig           `yaml:"processor"`
	Logging     LoggingConfig             `yaml:"logging"`
	Storage     StorageConfig             `yaml:"storage"`
	QuoteAssets []QuoteAssetConfig        `yaml:"quoteAssets"`
}

// MySQLConfig MySQL配置
type MySQLConfig struct {
	Host            string `yaml:"host"`
	Port            int    `yaml:"port"`
	Username        string `yaml:"username"`
	Password        string `yaml:"password"`
	Database        string `yaml:"database"`
	MaxIdleConns    int    `yaml:"max_idle_conns"`
	MaxOpenConns    int    `yaml:"max_open_conns"`
	ConnMaxLifetime int    `yaml:"conn_max_lifetime"`
}

// RedisConfig Redis配置
type RedisConfig struct {
	Host       string `yaml:"host"`
	Port       int    `yaml:"port"`
	Password   string `yaml:"password"`
	DB         int    `yaml:"db"`
	MaxRetries int    `yaml:"maxRetries"`
	PoolSize   int    `yaml:"poolSize"`
}

// ChainConfig 链配置
type ChainConfig struct {
	Enabled     bool   `yaml:"enabled"`
	RPCEndpoint string `yaml:"rpc_endpoint"`
	ChainID     string `yaml:"chain_id"`
	BatchSize   int    `yaml:"batch_size"`
	Timeout     int    `yaml:"timeout"`
	RetryCount  int    `yaml:"retry_count"`
}

// ProtocolConfig 协议配置
type ProtocolConfig struct {
	Enabled           bool     `yaml:"enabled"`
	Chain             string   `yaml:"chain"`
	ContractAddresses []string `yaml:"contract_addresses"`
}

// ProcessorConfig 处理器配置
type ProcessorConfig struct {
	BatchSize     int `yaml:"batch_size"`
	MaxConcurrent int `yaml:"max_concurrent"`
	RetryDelay    int `yaml:"retry_delay"`
	MaxRetries    int `yaml:"max_retries"`
}

// LoggingConfig 日志配置
type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
	Output string `yaml:"output"`
}

// StorageConfig 存储配置
type StorageConfig struct {
	Type     string                `yaml:"type"`
	MySQL    MySQLConfig           `yaml:"mysql"`
	InfluxDB InfluxDBStorageConfig `yaml:"influxdb"`
	PgSQL    PgSQLStorageConfig    `yaml:"pgsql"`
}

// PgSQLStorageConfig PostgreSQL存储配置
type PgSQLStorageConfig struct {
	Host            string `yaml:"host"`
	Port            int    `yaml:"port"`
	Username        string `yaml:"username"`
	Password        string `yaml:"password"`
	Database        string `yaml:"database"`
	SSLMode         string `yaml:"sslmode"`
	MaxOpenConns    int    `yaml:"max_open_conns"`
	MaxIdleConns    int    `yaml:"max_idle_conns"`
	ConnMaxLifetime int    `yaml:"conn_max_lifetime"`
}

// InfluxDBStorageConfig InfluxDB存储配置
type InfluxDBStorageConfig struct {
	URL       string `yaml:"url"`
	Token     string `yaml:"token"`
	Org       string `yaml:"org"`
	Bucket    string `yaml:"bucket"`
	BatchSize int    `yaml:"batch_size"`
	FlushTime int    `yaml:"flush_time"`
	Precision string `yaml:"precision"`
}

// GetEnv returns the current environment from APP_ENV (defaults to dev).
func GetEnv() Environment {
	env := os.Getenv("APP_ENV")
	switch Environment(strings.ToLower(env)) {
	case EnvStaging:
		return EnvStaging
	case EnvProd:
		return EnvProd
	default:
		return EnvDev
	}
}

// LoadConfig loads configuration with layered merging:
//
//	base.yaml -> overlay (e.g. api.yaml / chain.yaml) -> env overlay (env/prod.yaml) -> env vars
func LoadConfig(configPath string) (*Config, error) {
	if configPath == "" {
		return nil, fmt.Errorf("config path is required")
	}

	configPath = resolveConfigPath(configPath)

	configDir := filepath.Dir(configPath)

	// Layer 1: base.yaml
	config, err := loadBaseConfig(configDir)
	if err != nil {
		return nil, err
	}

	// Layer 2: service/chain overlay
	if err := mergeFromFile(config, configPath); err != nil {
		return nil, fmt.Errorf("loading overlay config %s: %w", configPath, err)
	}

	// Layer 3: environment overlay (configs/env/{APP_ENV}.yaml)
	env := GetEnv()
	envPath := filepath.Join(configDir, "env", string(env)+".yaml")
	if _, err := os.Stat(envPath); err == nil {
		if err := mergeFromFile(config, envPath); err != nil {
			return nil, fmt.Errorf("loading env config %s: %w", envPath, err)
		}
	}

	// Layer 4: environment variable overrides
	applyEnvOverrides(config)

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return config, nil
}

// LoadChainConfig loads configuration for a specific chain type.
// chainType can be provided as argument or via CHAIN_TYPE env var.
func LoadChainConfig(chainType string) (*Config, error) {
	if chainType == "" {
		chainType = os.Getenv("CHAIN_TYPE")
	}
	if chainType == "" {
		return nil, fmt.Errorf("chain type required: set CHAIN_TYPE env var or pass chainType argument")
	}

	chainType = strings.ToLower(chainType)
	validChains := []string{"sui", "ethereum", "bsc", "solana"}
	isValid := false
	for _, valid := range validChains {
		if chainType == valid {
			isValid = true
			break
		}
	}
	if !isValid {
		return nil, fmt.Errorf("unsupported chain type: %s (supported: %v)", chainType, validChains)
	}

	configPath := fmt.Sprintf("configs/%s.yaml", chainType)
	return LoadConfig(configPath)
}

// loadBaseConfig loads configs/base.yaml if it exists, otherwise returns an empty config.
func loadBaseConfig(configDir string) (*Config, error) {
	config := &Config{}
	basePath := filepath.Join(configDir, "my_base.yaml")
	if _, err := os.Stat(basePath); err == nil {
		if err := mergeFromFile(config, basePath); err != nil {
			return nil, fmt.Errorf("loading base config: %w", err)
		}
	}
	return config, nil
}

// mergeFromFile reads a YAML file and merges non-zero values into dest.
// For maps (Chains, Protocols), entries are merged (not replaced).
// For slices (QuoteAssets, AllowOrigins), overlay replaces base entirely.
func mergeFromFile(dest *Config, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}

	var overlay Config
	if err := yaml.Unmarshal(data, &overlay); err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}

	mergeConfig(dest, &overlay)
	return nil
}

// mergeConfig merges non-zero fields from src into dest.
func mergeConfig(dest, src *Config) {
	// API
	if src.API.Port != 0 {
		dest.API.Port = src.API.Port
	}
	if src.API.ReadTimeout != 0 {
		dest.API.ReadTimeout = src.API.ReadTimeout
	}
	if src.API.WriteTimeout != 0 {
		dest.API.WriteTimeout = src.API.WriteTimeout
	}
	if len(src.API.AllowOrigins) > 0 {
		dest.API.AllowOrigins = src.API.AllowOrigins
	}
	if src.API.RateLimit != 0 {
		dest.API.RateLimit = src.API.RateLimit
	}
	if src.API.RateBurst != 0 {
		dest.API.RateBurst = src.API.RateBurst
	}

	// Redis
	mergeRedis(&dest.Redis, &src.Redis)

	// Chains (merge map entries)
	if len(src.Chains) > 0 {
		if dest.Chains == nil {
			dest.Chains = make(map[string]ChainConfig)
		}
		for k, v := range src.Chains {
			dest.Chains[k] = v
		}
	}

	// Protocols (merge map entries)
	if len(src.Protocols) > 0 {
		if dest.Protocols == nil {
			dest.Protocols = make(map[string]ProtocolConfig)
		}
		for k, v := range src.Protocols {
			dest.Protocols[k] = v
		}
	}

	// Processor
	if src.Processor.BatchSize != 0 {
		dest.Processor.BatchSize = src.Processor.BatchSize
	}
	if src.Processor.MaxConcurrent != 0 {
		dest.Processor.MaxConcurrent = src.Processor.MaxConcurrent
	}
	if src.Processor.RetryDelay != 0 {
		dest.Processor.RetryDelay = src.Processor.RetryDelay
	}
	if src.Processor.MaxRetries != 0 {
		dest.Processor.MaxRetries = src.Processor.MaxRetries
	}

	// Logging
	if src.Logging.Level != "" {
		dest.Logging.Level = src.Logging.Level
	}
	if src.Logging.Format != "" {
		dest.Logging.Format = src.Logging.Format
	}
	if src.Logging.Output != "" {
		dest.Logging.Output = src.Logging.Output
	}

	// Storage
	if src.Storage.Type != "" {
		dest.Storage.Type = src.Storage.Type
	}
	mergeMySQL(&dest.Storage.MySQL, &src.Storage.MySQL)
	mergeInfluxDB(&dest.Storage.InfluxDB, &src.Storage.InfluxDB)
	mergePgSQL(&dest.Storage.PgSQL, &src.Storage.PgSQL)

	// QuoteAssets (overlay replaces entirely)
	if len(src.QuoteAssets) > 0 {
		dest.QuoteAssets = src.QuoteAssets
	}

}

func mergeRedis(dest, src *RedisConfig) {
	if src.Host != "" {
		dest.Host = src.Host
	}
	if src.Port != 0 {
		dest.Port = src.Port
	}
	if src.Password != "" {
		dest.Password = src.Password
	}
	if src.DB != 0 {
		dest.DB = src.DB
	}
	if src.MaxRetries != 0 {
		dest.MaxRetries = src.MaxRetries
	}
	if src.PoolSize != 0 {
		dest.PoolSize = src.PoolSize
	}
}

func mergeMySQL(dest, src *MySQLConfig) {
	if src.Host != "" {
		dest.Host = src.Host
	}
	if src.Port != 0 {
		dest.Port = src.Port
	}
	if src.Username != "" {
		dest.Username = src.Username
	}
	if src.Password != "" {
		dest.Password = src.Password
	}
	if src.Database != "" {
		dest.Database = src.Database
	}
	if src.MaxIdleConns != 0 {
		dest.MaxIdleConns = src.MaxIdleConns
	}
	if src.MaxOpenConns != 0 {
		dest.MaxOpenConns = src.MaxOpenConns
	}
	if src.ConnMaxLifetime != 0 {
		dest.ConnMaxLifetime = src.ConnMaxLifetime
	}
}

func mergePgSQL(dest, src *PgSQLStorageConfig) {
	if src.Host != "" {
		dest.Host = src.Host
	}
	if src.Port != 0 {
		dest.Port = src.Port
	}
	if src.Username != "" {
		dest.Username = src.Username
	}
	if src.Password != "" {
		dest.Password = src.Password
	}
	if src.Database != "" {
		dest.Database = src.Database
	}
	if src.SSLMode != "" {
		dest.SSLMode = src.SSLMode
	}
	if src.MaxOpenConns != 0 {
		dest.MaxOpenConns = src.MaxOpenConns
	}
	if src.MaxIdleConns != 0 {
		dest.MaxIdleConns = src.MaxIdleConns
	}
	if src.ConnMaxLifetime != 0 {
		dest.ConnMaxLifetime = src.ConnMaxLifetime
	}
}

func mergeInfluxDB(dest, src *InfluxDBStorageConfig) {
	if src.URL != "" {
		dest.URL = src.URL
	}
	if src.Token != "" {
		dest.Token = src.Token
	}
	if src.Org != "" {
		dest.Org = src.Org
	}
	if src.Bucket != "" {
		dest.Bucket = src.Bucket
	}
	if src.BatchSize != 0 {
		dest.BatchSize = src.BatchSize
	}
	if src.FlushTime != 0 {
		dest.FlushTime = src.FlushTime
	}
	if src.Precision != "" {
		dest.Precision = src.Precision
	}
}

// applyEnvOverrides applies environment variable overrides for sensitive and runtime config.
// Convention: CP_ prefix (Chain Parse), double underscore for nesting.
//
// Supported env vars:
//
//	CP_LOG_LEVEL          -> logging.level
//	CP_STORAGE_TYPE       -> storage.type
//	CP_REDIS_HOST         -> redis.host
//	CP_REDIS_PORT         -> redis.port
//	CP_REDIS_PASSWORD     -> redis.password
//	CP_PGSQL_HOST         -> storage.pgsql.host
//	CP_PGSQL_PORT         -> storage.pgsql.port
//	CP_PGSQL_USERNAME     -> storage.pgsql.username
//	CP_PGSQL_PASSWORD     -> storage.pgsql.password
//	CP_PGSQL_DATABASE     -> storage.pgsql.database
//	CP_PGSQL_SSLMODE      -> storage.pgsql.sslmode
//	CP_MYSQL_HOST         -> storage.mysql.host
//	CP_MYSQL_PORT         -> storage.mysql.port
//	CP_MYSQL_USERNAME     -> storage.mysql.username
//	CP_MYSQL_PASSWORD     -> storage.mysql.password
//	CP_MYSQL_DATABASE     -> storage.mysql.database
//	CP_INFLUXDB_URL       -> storage.influxdb.url
//	CP_INFLUXDB_TOKEN     -> storage.influxdb.token
//	CP_INFLUXDB_ORG       -> storage.influxdb.org
//	CP_INFLUXDB_BUCKET    -> storage.influxdb.bucket
//	CP_API_PORT           -> api.port
//	CP_RPC_ENDPOINT_{CHAIN} -> chains.{chain}.rpc_endpoint (e.g. CP_RPC_ENDPOINT_SUI)
func applyEnvOverrides(cfg *Config) {
	// Logging
	envStr(&cfg.Logging.Level, "CP_LOG_LEVEL")

	// Storage type
	envStr(&cfg.Storage.Type, "CP_STORAGE_TYPE")

	// Redis
	envStr(&cfg.Redis.Host, "CP_REDIS_HOST")
	envInt(&cfg.Redis.Port, "CP_REDIS_PORT")
	envStr(&cfg.Redis.Password, "CP_REDIS_PASSWORD")

	// PostgreSQL
	envStr(&cfg.Storage.PgSQL.Host, "CP_PGSQL_HOST")
	envInt(&cfg.Storage.PgSQL.Port, "CP_PGSQL_PORT")
	envStr(&cfg.Storage.PgSQL.Username, "CP_PGSQL_USERNAME")
	envStr(&cfg.Storage.PgSQL.Password, "CP_PGSQL_PASSWORD")
	envStr(&cfg.Storage.PgSQL.Database, "CP_PGSQL_DATABASE")
	envStr(&cfg.Storage.PgSQL.SSLMode, "CP_PGSQL_SSLMODE")

	// MySQL
	envStr(&cfg.Storage.MySQL.Host, "CP_MYSQL_HOST")
	envInt(&cfg.Storage.MySQL.Port, "CP_MYSQL_PORT")
	envStr(&cfg.Storage.MySQL.Username, "CP_MYSQL_USERNAME")
	envStr(&cfg.Storage.MySQL.Password, "CP_MYSQL_PASSWORD")
	envStr(&cfg.Storage.MySQL.Database, "CP_MYSQL_DATABASE")

	// InfluxDB
	envStr(&cfg.Storage.InfluxDB.URL, "CP_INFLUXDB_URL")
	envStr(&cfg.Storage.InfluxDB.Token, "CP_INFLUXDB_TOKEN")
	envStr(&cfg.Storage.InfluxDB.Org, "CP_INFLUXDB_ORG")
	envStr(&cfg.Storage.InfluxDB.Bucket, "CP_INFLUXDB_BUCKET")

	// API
	envInt(&cfg.API.Port, "CP_API_PORT")

	// Per-chain RPC endpoint overrides: CP_RPC_ENDPOINT_SUI, CP_RPC_ENDPOINT_BSC, etc.
	for name, chain := range cfg.Chains {
		envKey := "CP_RPC_ENDPOINT_" + strings.ToUpper(name)
		if val := os.Getenv(envKey); val != "" {
			chain.RPCEndpoint = val
			cfg.Chains[name] = chain
		}
	}
}

// envStr sets *dest from env var if it is set and non-empty.
func envStr(dest *string, key string) {
	if val := os.Getenv(key); val != "" {
		*dest = val
	}
}

// envInt sets *dest from env var if it is set and parses as int.
func envInt(dest *int, key string) {
	if val := os.Getenv(key); val != "" {
		if n, err := strconv.Atoi(val); err == nil {
			*dest = n
		}
	}
}

// resolveConfigPath resolves relative paths against cwd.
func resolveConfigPath(configPath string) string {
	if filepath.IsAbs(configPath) {
		return configPath
	}
	if _, err := os.Stat(configPath); err == nil {
		return configPath
	}
	wd, _ := os.Getwd()
	altPath := filepath.Join(wd, configPath)
	if _, err := os.Stat(altPath); err == nil {
		return altPath
	}
	return configPath
}

// Validate checks required fields and value ranges.
func (c *Config) Validate() error {
	if c.API.Port != 0 && (c.API.Port < 1 || c.API.Port > 65535) {
		return fmt.Errorf("invalid api port: %d (must be 1-65535)", c.API.Port)
	}
	if c.Processor.BatchSize <= 0 {
		return fmt.Errorf("invalid batch size: %d (must be > 0)", c.Processor.BatchSize)
	}
	if c.Processor.MaxRetries <= 0 {
		return fmt.Errorf("invalid max retries: %d (must be > 0)", c.Processor.MaxRetries)
	}

	for name, chain := range c.Chains {
		if chain.Timeout <= 0 {
			return fmt.Errorf("invalid timeout for chain %s: %d (must be > 0)", name, chain.Timeout)
		}
		if chain.BatchSize <= 0 {
			return fmt.Errorf("invalid batch size for chain %s: %d (must be > 0)", name, chain.BatchSize)
		}
		if chain.Enabled && chain.RPCEndpoint == "" {
			return fmt.Errorf("rpc_endpoint is required for enabled chain %s", name)
		}
	}

	// Validate storage config based on type
	switch c.Storage.Type {
	case "pgsql":
		if c.Storage.PgSQL.Host == "" {
			return fmt.Errorf("pgsql host is required")
		}
	case "mysql":
		if c.Storage.MySQL.Host == "" {
			return fmt.Errorf("mysql host is required")
		}
	case "influxdb":
		if c.Storage.InfluxDB.URL == "" {
			return fmt.Errorf("influxdb url is required")
		}
		if c.Storage.InfluxDB.Token == "" {
			return fmt.Errorf("influxdb token is required")
		}
	}

	return nil
}
