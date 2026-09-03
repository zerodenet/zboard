package config

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/zeromicro/go-zero/rest"

	"github.com/zerodenet/zboard/backend/internal/security"
	"github.com/zerodenet/zboard/backend/internal/zeroevent"
)

var zeroLocalVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z][0-9A-Za-z.-]*)?$`)

const (
	EnvironmentDevelopment = "development"
	EnvironmentProduction  = "production"
	EnvironmentTest        = "test"
	ZeroKernelLegacy       = "legacy"
	ZeroKernelNativeLocal  = "native-local"
	ZeroKernelNativeMieru  = "native-local-mieru"
	ZeroEventSpoolFile     = "file"
	ZeroEventSpoolLegacy   = "legacy"
	MinimumJWTSecretBytes  = 32
)

type Config struct {
	rest.RestConf
	Environment                         string  `json:"environment,default=development"`
	DatabaseDriver                      string  `json:"database_driver,default=mysql"`
	DataSource                          string  `json:"datasource"`
	DatabaseMaxOpenConnections          int     `json:"database_max_open_connections,default=8"`
	DatabaseMaxIdleConnections          int     `json:"database_max_idle_connections,default=2"`
	DatabaseConnectionMaxLifetimeSecs   int     `json:"database_connection_max_lifetime_seconds,default=3600"`
	JwtSecret                           string  `json:"jwt_secret"`
	BootstrapAdminEmail                 string  `json:"bootstrap_admin_email,optional"`
	BootstrapAdminPassword              string  `json:"bootstrap_admin_password,optional"`
	CredentialEncryptionKey             string  `json:"credential_encryption_key,optional"`
	ZeroArtifactDir                     string  `json:"zero_artifact_dir,optional"`
	ZeroKernelContract                  string  `json:"zero_kernel_contract,default=legacy"`
	ZeroLocalVersion                    string  `json:"zero_local_version,optional"`
	ZeroEventSpoolMode                  string  `json:"zero_event_spool_mode,default=file"`
	ZeroEventSpoolDir                   string  `json:"zero_event_spool_dir,default=/var/lib/zboard/zero-events"`
	ZeroEventSpoolCompressionAlgorithm  string  `json:"zero_event_spool_compression_algorithm,optional"`
	ZeroEventSpoolCompressionLevel      int     `json:"zero_event_spool_compression_level,optional"`
	ZeroEventSpoolCompressionBlockBytes int64   `json:"zero_event_spool_compression_block_bytes,optional"`
	ZeroEventSpoolCompressionWorkers    int     `json:"zero_event_spool_compression_workers,optional"`
	ZeroEventSpoolMaxBytes              int64   `json:"zero_event_spool_max_bytes,optional"`
	ZeroEventSpoolWarningRatio          float64 `json:"zero_event_spool_warning_ratio,optional"`
	ZeroEventSpoolCompactRatio          float64 `json:"zero_event_spool_compact_ratio,optional"`
	ZeroEventSpoolEmergencyRatio        float64 `json:"zero_event_spool_emergency_ratio,optional"`
	ZeroEventSpoolMinFreeBytes          int64   `json:"zero_event_spool_min_free_bytes,optional"`
	ZeroEventSpoolEmergencyReserveBytes int64   `json:"zero_event_spool_emergency_reserve_bytes,optional"`
	ZeroEventSpoolCriticalReserveBytes  int64   `json:"zero_event_spool_critical_reserve_bytes,optional"`

	zeroEventSpoolCompressionEnabledEnv    string
	zeroEventSpoolCompressionAlgorithmEnv  string
	zeroEventSpoolCompressionLevelEnv      string
	zeroEventSpoolCompressionBlockBytesEnv string
	zeroEventSpoolCompressionWorkersEnv    string
	zeroEventSpoolMaxBytesEnv              string
	zeroEventSpoolWarningRatioEnv          string
	zeroEventSpoolCompactRatioEnv          string
	zeroEventSpoolEmergencyRatioEnv        string
	zeroEventSpoolMinFreeBytesEnv          string
	zeroEventSpoolEmergencyReserveBytesEnv string
	zeroEventSpoolCriticalReserveBytesEnv  string
	zeroEventSpoolCompactionEnabledEnv     string
	zeroEventSpoolMergeFlowUpdatesEnv      string
	zeroEventSpoolMergeNodeStatsEnv        string
	databaseMaxOpenConnectionsEnv          string
	databaseMaxIdleConnectionsEnv          string
	databaseConnectionMaxLifetimeSecsEnv   string
	zeroEventSpoolCompression              *zeroevent.CompressionConfig
	zeroEventSpoolCompaction               *zeroevent.CompactionConfig
}

type BootstrapAdmin struct {
	Email    string
	Password string
}

func (c *Config) ApplyEnvironment(getenv func(string) string) {
	if getenv == nil {
		return
	}
	applyOverride(&c.Environment, getenv("ZBOARD_ENVIRONMENT"))
	applyOverride(&c.DatabaseDriver, getenv("ZBOARD_DATABASE_DRIVER"))
	applyOverride(&c.DataSource, getenv("ZBOARD_DATA_SOURCE"))
	applyOverride(&c.databaseMaxOpenConnectionsEnv, getenv("ZBOARD_DATABASE_MAX_OPEN_CONNECTIONS"))
	applyOverride(&c.databaseMaxIdleConnectionsEnv, getenv("ZBOARD_DATABASE_MAX_IDLE_CONNECTIONS"))
	applyOverride(&c.databaseConnectionMaxLifetimeSecsEnv, getenv("ZBOARD_DATABASE_CONNECTION_MAX_LIFETIME_SECONDS"))
	applyOverride(&c.JwtSecret, getenv("ZBOARD_JWT_SECRET"))
	applyOverride(&c.BootstrapAdminEmail, getenv("ZBOARD_BOOTSTRAP_ADMIN_EMAIL"))
	applyOverride(&c.BootstrapAdminPassword, getenv("ZBOARD_BOOTSTRAP_ADMIN_PASSWORD"))
	applyOverride(&c.CredentialEncryptionKey, getenv("ZBOARD_CREDENTIAL_ENCRYPTION_KEY"))
	applyOverride(&c.ZeroArtifactDir, getenv("ZBOARD_ZERO_ARTIFACT_DIR"))
	applyOverride(&c.ZeroKernelContract, getenv("ZBOARD_ZERO_KERNEL_CONTRACT"))
	applyOverride(&c.ZeroLocalVersion, getenv("ZBOARD_ZERO_LOCAL_VERSION"))
	applyOverride(&c.ZeroEventSpoolMode, getenv("ZBOARD_ZERO_EVENT_SPOOL_MODE"))
	applyOverride(&c.ZeroEventSpoolDir, getenv("ZBOARD_ZERO_EVENT_SPOOL_DIR"))
	applyOverride(&c.zeroEventSpoolCompressionEnabledEnv, getenv("ZBOARD_ZERO_EVENT_SPOOL_COMPRESSION_ENABLED"))
	applyOverride(&c.zeroEventSpoolCompressionAlgorithmEnv, getenv("ZBOARD_ZERO_EVENT_SPOOL_COMPRESSION_ALGORITHM"))
	applyOverride(&c.zeroEventSpoolCompressionLevelEnv, getenv("ZBOARD_ZERO_EVENT_SPOOL_COMPRESSION_LEVEL"))
	applyOverride(&c.zeroEventSpoolCompressionBlockBytesEnv, getenv("ZBOARD_ZERO_EVENT_SPOOL_COMPRESSION_BLOCK_BYTES"))
	applyOverride(&c.zeroEventSpoolCompressionWorkersEnv, getenv("ZBOARD_ZERO_EVENT_SPOOL_COMPRESSION_WORKERS"))
	applyOverride(&c.zeroEventSpoolMaxBytesEnv, getenv("ZBOARD_ZERO_EVENT_SPOOL_MAX_BYTES"))
	applyOverride(&c.zeroEventSpoolWarningRatioEnv, getenv("ZBOARD_ZERO_EVENT_SPOOL_WARNING_RATIO"))
	applyOverride(&c.zeroEventSpoolCompactRatioEnv, getenv("ZBOARD_ZERO_EVENT_SPOOL_COMPACT_RATIO"))
	applyOverride(&c.zeroEventSpoolEmergencyRatioEnv, getenv("ZBOARD_ZERO_EVENT_SPOOL_EMERGENCY_RATIO"))
	applyOverride(&c.zeroEventSpoolMinFreeBytesEnv, getenv("ZBOARD_ZERO_EVENT_SPOOL_MIN_FREE_BYTES"))
	applyOverride(&c.zeroEventSpoolEmergencyReserveBytesEnv, getenv("ZBOARD_ZERO_EVENT_SPOOL_EMERGENCY_RESERVE_BYTES"))
	applyOverride(&c.zeroEventSpoolCriticalReserveBytesEnv, getenv("ZBOARD_ZERO_EVENT_SPOOL_CRITICAL_RESERVE_BYTES"))
	applyOverride(&c.zeroEventSpoolCompactionEnabledEnv, getenv("ZBOARD_ZERO_EVENT_SPOOL_COMPACTION_ENABLED"))
	applyOverride(&c.zeroEventSpoolMergeFlowUpdatesEnv, getenv("ZBOARD_ZERO_EVENT_SPOOL_COMPACTION_FLOW_UPDATES"))
	applyOverride(&c.zeroEventSpoolMergeNodeStatsEnv, getenv("ZBOARD_ZERO_EVENT_SPOOL_COMPACTION_NODE_STATS"))
}

func (c *Config) Validate() error {
	environment, err := NormalizeEnvironment(c.Environment)
	if err != nil {
		return err
	}
	c.Environment = environment
	c.ZeroKernelContract = strings.ToLower(strings.TrimSpace(c.ZeroKernelContract))
	if c.ZeroKernelContract == "" {
		c.ZeroKernelContract = ZeroKernelLegacy
	}
	if c.ZeroKernelContract != ZeroKernelLegacy &&
		c.ZeroKernelContract != ZeroKernelNativeLocal &&
		c.ZeroKernelContract != ZeroKernelNativeMieru {
		return fmt.Errorf("unsupported zero_kernel_contract %q", c.ZeroKernelContract)
	}
	c.ZeroLocalVersion = strings.TrimSpace(c.ZeroLocalVersion)
	if c.ZeroLocalVersion != "" && !zeroLocalVersionPattern.MatchString(c.ZeroLocalVersion) {
		return fmt.Errorf("zero_local_version %q is not a supported semantic version", c.ZeroLocalVersion)
	}
	if (c.ZeroKernelContract == ZeroKernelNativeLocal || c.ZeroKernelContract == ZeroKernelNativeMieru) && c.ZeroLocalVersion == "" {
		return errors.New("zero_local_version is required for the native-local kernel contract")
	}
	c.ZeroEventSpoolMode = strings.ToLower(strings.TrimSpace(c.ZeroEventSpoolMode))
	if c.ZeroEventSpoolMode == "" {
		c.ZeroEventSpoolMode = ZeroEventSpoolFile
	}
	if c.ZeroEventSpoolMode != ZeroEventSpoolFile && c.ZeroEventSpoolMode != ZeroEventSpoolLegacy {
		return fmt.Errorf("unsupported zero_event_spool_mode %q", c.ZeroEventSpoolMode)
	}
	c.ZeroEventSpoolDir = strings.TrimSpace(c.ZeroEventSpoolDir)
	if c.ZeroEventSpoolMode == ZeroEventSpoolFile && c.ZeroEventSpoolDir == "" {
		c.ZeroEventSpoolDir = zeroevent.DefaultConfig().Directory
	}
	if err := c.normalizeZeroEventSpoolCompression(); err != nil {
		return err
	}
	if err := c.normalizeZeroEventSpoolStorage(); err != nil {
		return err
	}
	if err := c.normalizeZeroEventSpoolCompaction(); err != nil {
		return err
	}
	if strings.TrimSpace(c.DataSource) == "" {
		return errors.New("datasource is required")
	}
	c.DatabaseDriver = strings.ToLower(strings.TrimSpace(c.DatabaseDriver))
	if c.DatabaseDriver == "" {
		c.DatabaseDriver = "mysql"
	}
	if c.DatabaseDriver != "mysql" && c.DatabaseDriver != "sqlite" {
		return errors.New("database_driver must be mysql or sqlite")
	}
	if err := c.normalizeDatabasePool(); err != nil {
		return err
	}
	if c.DatabaseDriver == "sqlite" {
		c.DatabaseMaxOpenConnections = 1
		c.DatabaseMaxIdleConnections = 1
	}
	if err := ValidateJWTSecret(c.JwtSecret); err != nil {
		return err
	}
	if err := security.ValidateCredentialKey(c.CredentialEncryptionKey); err != nil {
		return err
	}

	admin := c.BootstrapAdmin()
	if admin.Configured() {
		return admin.Validate(environment)
	}
	return nil
}

func (c *Config) normalizeDatabasePool() error {
	if c.DatabaseMaxOpenConnections == 0 {
		c.DatabaseMaxOpenConnections = 8
	}
	if c.DatabaseMaxIdleConnections == 0 {
		c.DatabaseMaxIdleConnections = 2
	}
	if c.DatabaseConnectionMaxLifetimeSecs == 0 {
		c.DatabaseConnectionMaxLifetimeSecs = 3600
	}
	if err := parseIntConfigOverride(&c.DatabaseMaxOpenConnections, c.databaseMaxOpenConnectionsEnv, "ZBOARD_DATABASE_MAX_OPEN_CONNECTIONS"); err != nil {
		return err
	}
	if err := parseIntConfigOverride(&c.DatabaseMaxIdleConnections, c.databaseMaxIdleConnectionsEnv, "ZBOARD_DATABASE_MAX_IDLE_CONNECTIONS"); err != nil {
		return err
	}
	if err := parseIntConfigOverride(&c.DatabaseConnectionMaxLifetimeSecs, c.databaseConnectionMaxLifetimeSecsEnv, "ZBOARD_DATABASE_CONNECTION_MAX_LIFETIME_SECONDS"); err != nil {
		return err
	}
	if c.DatabaseMaxOpenConnections < 1 || c.DatabaseMaxOpenConnections > 256 {
		return errors.New("database_max_open_connections must be between 1 and 256")
	}
	if c.DatabaseMaxIdleConnections < 0 || c.DatabaseMaxIdleConnections > c.DatabaseMaxOpenConnections {
		return errors.New("database_max_idle_connections must be between 0 and database_max_open_connections")
	}
	if c.DatabaseConnectionMaxLifetimeSecs < 60 || c.DatabaseConnectionMaxLifetimeSecs > 86400 {
		return errors.New("database_connection_max_lifetime_seconds must be between 60 and 86400")
	}
	return nil
}

func (c Config) ZeroEventSpoolConfig() zeroevent.Config {
	cfg := zeroevent.DefaultConfig()
	cfg.Enabled = strings.ToLower(strings.TrimSpace(c.ZeroEventSpoolMode)) != ZeroEventSpoolLegacy
	if directory := strings.TrimSpace(c.ZeroEventSpoolDir); directory != "" {
		cfg.Directory = directory
	}
	cfg.Compression = c.zeroEventSpoolCompressionConfig()
	cfg.Storage = c.zeroEventSpoolStorageConfig()
	cfg.Compaction = c.zeroEventSpoolCompactionConfig()
	return cfg
}

func (c *Config) normalizeZeroEventSpoolCompression() error {
	compression := zeroevent.DefaultConfig().Compression
	algorithm := strings.ToLower(strings.TrimSpace(c.ZeroEventSpoolCompressionAlgorithm))
	if algorithm != "" {
		compression.Algorithm = algorithm
	}
	if c.ZeroEventSpoolCompressionLevel != 0 {
		compression.Level = c.ZeroEventSpoolCompressionLevel
	}
	if c.ZeroEventSpoolCompressionBlockBytes != 0 {
		compression.BlockSize = c.ZeroEventSpoolCompressionBlockBytes
	}
	if c.ZeroEventSpoolCompressionWorkers != 0 {
		compression.Workers = c.ZeroEventSpoolCompressionWorkers
	}
	if err := parseBoolConfigOverride(&compression.Enabled, c.zeroEventSpoolCompressionEnabledEnv, "ZBOARD_ZERO_EVENT_SPOOL_COMPRESSION_ENABLED"); err != nil {
		return err
	}
	if value := strings.ToLower(strings.TrimSpace(c.zeroEventSpoolCompressionAlgorithmEnv)); value != "" {
		compression.Algorithm = value
	}
	if err := parseIntConfigOverride(&compression.Level, c.zeroEventSpoolCompressionLevelEnv, "ZBOARD_ZERO_EVENT_SPOOL_COMPRESSION_LEVEL"); err != nil {
		return err
	}
	if err := parseInt64ConfigOverride(&compression.BlockSize, c.zeroEventSpoolCompressionBlockBytesEnv, "ZBOARD_ZERO_EVENT_SPOOL_COMPRESSION_BLOCK_BYTES"); err != nil {
		return err
	}
	if err := parseIntConfigOverride(&compression.Workers, c.zeroEventSpoolCompressionWorkersEnv, "ZBOARD_ZERO_EVENT_SPOOL_COMPRESSION_WORKERS"); err != nil {
		return err
	}
	if compression.Algorithm == zeroevent.CompressionNone {
		compression.Enabled = false
	} else if compression.Algorithm != zeroevent.CompressionZstd && compression.Algorithm != zeroevent.CompressionLZ4 {
		return fmt.Errorf("unsupported Zero event spool compression algorithm %q", compression.Algorithm)
	}
	validation := zeroevent.DefaultConfig()
	validation.Compression = compression
	if err := validation.Validate(); err != nil {
		return fmt.Errorf("invalid Zero event spool compression config: %w", err)
	}
	c.zeroEventSpoolCompression = &compression
	return nil
}

func (c Config) zeroEventSpoolCompressionConfig() zeroevent.CompressionConfig {
	if c.zeroEventSpoolCompression != nil {
		return *c.zeroEventSpoolCompression
	}
	compression := zeroevent.DefaultConfig().Compression
	if algorithm := strings.ToLower(strings.TrimSpace(c.ZeroEventSpoolCompressionAlgorithm)); algorithm != "" {
		compression.Algorithm = algorithm
	}
	if c.ZeroEventSpoolCompressionLevel != 0 {
		compression.Level = c.ZeroEventSpoolCompressionLevel
	}
	if c.ZeroEventSpoolCompressionBlockBytes != 0 {
		compression.BlockSize = c.ZeroEventSpoolCompressionBlockBytes
	}
	if c.ZeroEventSpoolCompressionWorkers != 0 {
		compression.Workers = c.ZeroEventSpoolCompressionWorkers
	}
	_ = parseBoolConfigOverride(&compression.Enabled, c.zeroEventSpoolCompressionEnabledEnv, "")
	if value := strings.ToLower(strings.TrimSpace(c.zeroEventSpoolCompressionAlgorithmEnv)); value != "" {
		compression.Algorithm = value
	}
	_ = parseIntConfigOverride(&compression.Level, c.zeroEventSpoolCompressionLevelEnv, "")
	_ = parseInt64ConfigOverride(&compression.BlockSize, c.zeroEventSpoolCompressionBlockBytesEnv, "")
	_ = parseIntConfigOverride(&compression.Workers, c.zeroEventSpoolCompressionWorkersEnv, "")
	if compression.Algorithm == zeroevent.CompressionNone {
		compression.Enabled = false
	}
	return compression
}

func (c *Config) normalizeZeroEventSpoolStorage() error {
	defaults := zeroevent.DefaultConfig().Storage
	if c.ZeroEventSpoolMaxBytes == 0 {
		c.ZeroEventSpoolMaxBytes = defaults.MaxSize
	}
	if c.ZeroEventSpoolWarningRatio == 0 {
		c.ZeroEventSpoolWarningRatio = defaults.WarningRatio
	}
	if c.ZeroEventSpoolCompactRatio == 0 {
		c.ZeroEventSpoolCompactRatio = defaults.CompactRatio
	}
	if c.ZeroEventSpoolEmergencyRatio == 0 {
		c.ZeroEventSpoolEmergencyRatio = defaults.EmergencyRatio
	}
	if c.ZeroEventSpoolMinFreeBytes == 0 {
		c.ZeroEventSpoolMinFreeBytes = defaults.MinFreeSpace
	}
	if c.ZeroEventSpoolEmergencyReserveBytes == 0 {
		c.ZeroEventSpoolEmergencyReserveBytes = defaults.EmergencyReserve
	}
	if c.ZeroEventSpoolCriticalReserveBytes == 0 {
		c.ZeroEventSpoolCriticalReserveBytes = defaults.CriticalReserve
	}

	if err := parseInt64ConfigOverride(&c.ZeroEventSpoolMaxBytes, c.zeroEventSpoolMaxBytesEnv, "ZBOARD_ZERO_EVENT_SPOOL_MAX_BYTES"); err != nil {
		return err
	}
	if err := parseFloat64ConfigOverride(&c.ZeroEventSpoolWarningRatio, c.zeroEventSpoolWarningRatioEnv, "ZBOARD_ZERO_EVENT_SPOOL_WARNING_RATIO"); err != nil {
		return err
	}
	if err := parseFloat64ConfigOverride(&c.ZeroEventSpoolCompactRatio, c.zeroEventSpoolCompactRatioEnv, "ZBOARD_ZERO_EVENT_SPOOL_COMPACT_RATIO"); err != nil {
		return err
	}
	if err := parseFloat64ConfigOverride(&c.ZeroEventSpoolEmergencyRatio, c.zeroEventSpoolEmergencyRatioEnv, "ZBOARD_ZERO_EVENT_SPOOL_EMERGENCY_RATIO"); err != nil {
		return err
	}
	if err := parseInt64ConfigOverride(&c.ZeroEventSpoolMinFreeBytes, c.zeroEventSpoolMinFreeBytesEnv, "ZBOARD_ZERO_EVENT_SPOOL_MIN_FREE_BYTES"); err != nil {
		return err
	}
	if err := parseInt64ConfigOverride(&c.ZeroEventSpoolEmergencyReserveBytes, c.zeroEventSpoolEmergencyReserveBytesEnv, "ZBOARD_ZERO_EVENT_SPOOL_EMERGENCY_RESERVE_BYTES"); err != nil {
		return err
	}
	if err := parseInt64ConfigOverride(&c.ZeroEventSpoolCriticalReserveBytes, c.zeroEventSpoolCriticalReserveBytesEnv, "ZBOARD_ZERO_EVENT_SPOOL_CRITICAL_RESERVE_BYTES"); err != nil {
		return err
	}

	validation := zeroevent.DefaultConfig()
	validation.Storage = c.zeroEventSpoolStorageConfig()
	if err := validation.Validate(); err != nil {
		return fmt.Errorf("invalid Zero event spool storage config: %w", err)
	}
	return nil
}

func (c Config) zeroEventSpoolStorageConfig() zeroevent.StorageConfig {
	storage := zeroevent.DefaultConfig().Storage
	if c.ZeroEventSpoolMaxBytes != 0 || strings.TrimSpace(c.zeroEventSpoolMaxBytesEnv) != "" {
		storage.MaxSize = c.ZeroEventSpoolMaxBytes
	}
	if c.ZeroEventSpoolWarningRatio != 0 || strings.TrimSpace(c.zeroEventSpoolWarningRatioEnv) != "" {
		storage.WarningRatio = c.ZeroEventSpoolWarningRatio
	}
	if c.ZeroEventSpoolCompactRatio != 0 || strings.TrimSpace(c.zeroEventSpoolCompactRatioEnv) != "" {
		storage.CompactRatio = c.ZeroEventSpoolCompactRatio
	}
	if c.ZeroEventSpoolEmergencyRatio != 0 || strings.TrimSpace(c.zeroEventSpoolEmergencyRatioEnv) != "" {
		storage.EmergencyRatio = c.ZeroEventSpoolEmergencyRatio
	}
	if c.ZeroEventSpoolMinFreeBytes != 0 || strings.TrimSpace(c.zeroEventSpoolMinFreeBytesEnv) != "" {
		storage.MinFreeSpace = c.ZeroEventSpoolMinFreeBytes
	}
	if c.ZeroEventSpoolEmergencyReserveBytes != 0 || strings.TrimSpace(c.zeroEventSpoolEmergencyReserveBytesEnv) != "" {
		storage.EmergencyReserve = c.ZeroEventSpoolEmergencyReserveBytes
	}
	if c.ZeroEventSpoolCriticalReserveBytes != 0 || strings.TrimSpace(c.zeroEventSpoolCriticalReserveBytesEnv) != "" {
		storage.CriticalReserve = c.ZeroEventSpoolCriticalReserveBytes
	}
	return storage
}

func (c *Config) normalizeZeroEventSpoolCompaction() error {
	compaction := zeroevent.DefaultConfig().Compaction
	if err := parseBoolConfigOverride(&compaction.Enabled, c.zeroEventSpoolCompactionEnabledEnv, "ZBOARD_ZERO_EVENT_SPOOL_COMPACTION_ENABLED"); err != nil {
		return err
	}
	if err := parseBoolConfigOverride(&compaction.MergeFlowUpdates, c.zeroEventSpoolMergeFlowUpdatesEnv, "ZBOARD_ZERO_EVENT_SPOOL_COMPACTION_FLOW_UPDATES"); err != nil {
		return err
	}
	if err := parseBoolConfigOverride(&compaction.MergeNodeStats, c.zeroEventSpoolMergeNodeStatsEnv, "ZBOARD_ZERO_EVENT_SPOOL_COMPACTION_NODE_STATS"); err != nil {
		return err
	}
	validation := zeroevent.DefaultConfig()
	validation.Compaction = compaction
	if err := validation.Validate(); err != nil {
		return fmt.Errorf("invalid Zero event spool compaction config: %w", err)
	}
	c.zeroEventSpoolCompaction = &compaction
	return nil
}

func (c Config) zeroEventSpoolCompactionConfig() zeroevent.CompactionConfig {
	if c.zeroEventSpoolCompaction != nil {
		return *c.zeroEventSpoolCompaction
	}
	compaction := zeroevent.DefaultConfig().Compaction
	_ = parseBoolConfigOverride(&compaction.Enabled, c.zeroEventSpoolCompactionEnabledEnv, "")
	_ = parseBoolConfigOverride(&compaction.MergeFlowUpdates, c.zeroEventSpoolMergeFlowUpdatesEnv, "")
	_ = parseBoolConfigOverride(&compaction.MergeNodeStats, c.zeroEventSpoolMergeNodeStatsEnv, "")
	return compaction
}

func parseIntConfigOverride(target *int, raw, name string) error {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		if name == "" {
			return err
		}
		return fmt.Errorf("%s must be an integer: %w", name, err)
	}
	*target = parsed
	return nil
}

func parseInt64ConfigOverride(target *int64, raw, name string) error {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fmt.Errorf("%s must be an integer number of bytes: %w", name, err)
	}
	*target = parsed
	return nil
}

func parseFloat64ConfigOverride(target *float64, raw, name string) error {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fmt.Errorf("%s must be a decimal ratio: %w", name, err)
	}
	*target = parsed
	return nil
}

func parseBoolConfigOverride(target *bool, raw, name string) error {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		if name == "" {
			return err
		}
		return fmt.Errorf("%s must be a boolean: %w", name, err)
	}
	*target = parsed
	return nil
}

func (c Config) BootstrapAdmin() BootstrapAdmin {
	return BootstrapAdmin{
		Email:    strings.TrimSpace(c.BootstrapAdminEmail),
		Password: c.BootstrapAdminPassword,
	}
}

func NormalizeEnvironment(value string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return EnvironmentDevelopment, nil
	}
	switch normalized {
	case "dev":
		return EnvironmentDevelopment, nil
	case "prod":
		return EnvironmentProduction, nil
	case EnvironmentDevelopment, EnvironmentProduction, EnvironmentTest:
		return normalized, nil
	default:
		return "", fmt.Errorf("unsupported environment %q", value)
	}
}

func ValidateJWTSecret(secret string) error {
	trimmed := strings.TrimSpace(secret)
	if isPlaceholder(trimmed) {
		return errors.New("jwt_secret uses a known insecure placeholder")
	}
	if len(trimmed) < MinimumJWTSecretBytes {
		return fmt.Errorf("jwt_secret must contain at least %d bytes", MinimumJWTSecretBytes)
	}
	return nil
}

func (a BootstrapAdmin) Configured() bool {
	return a.Email != "" || a.Password != ""
}

func (a BootstrapAdmin) Validate(environment string) error {
	if a.Email == "" || a.Password == "" {
		return errors.New("bootstrap admin requires email and password")
	}
	if !strings.Contains(a.Email, "@") {
		return errors.New("bootstrap admin email is invalid")
	}
	if isPlaceholder(a.Email) || isPlaceholder(a.Password) {
		return errors.New("bootstrap admin contains an insecure placeholder")
	}
	minimumPasswordBytes := 12
	if environment == EnvironmentProduction {
		minimumPasswordBytes = 16
	}
	if len(a.Password) < minimumPasswordBytes {
		return fmt.Errorf("bootstrap admin password must contain at least %d bytes", minimumPasswordBytes)
	}
	return nil
}

func applyOverride(target *string, value string) {
	if strings.TrimSpace(value) != "" {
		*target = value
	}
}

func isPlaceholder(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	return normalized == "change-me" ||
		normalized == "changeme" ||
		normalized == "dev-jwt-secret" ||
		strings.HasPrefix(normalized, "replace-") ||
		strings.HasPrefix(normalized, "generate-") ||
		strings.HasPrefix(normalized, "choose-")
}
