package evmi_database

import (
	"database/sql"
	"time"

	"github.com/lib/pq"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type UserRole string

const (
	RoleAdmin UserRole = "admin"
	RoleUser  UserRole = "user"
)

type AccessTokenKind string

const (
	// APITokenKind is a long-lived token a user creates as an API key.
	APITokenKind AccessTokenKind = "api"
	// SessionTokenKind is a shorter-lived token issued by password/OAuth login.
	SessionTokenKind AccessTokenKind = "session"
)

// User is an authenticated principal. Password users have a PasswordHash; OAuth
// users have an OAuthSubject and no password.
type User struct {
	gorm.Model

	Username     string `gorm:"uniqueIndex"`
	Email        string
	PasswordHash string
	Role         string
	OAuthSubject string `gorm:"index"`
}

// AccessToken is an opaque bearer token. Only its SHA-256 hash is stored; the
// plaintext is shown once at creation.
type AccessToken struct {
	gorm.Model

	UserID uint `gorm:"index"`
	Name   string
	Kind   string

	TokenHash  string `gorm:"uniqueIndex"`
	ExpiresAt  *time.Time
	LastUsedAt *time.Time
}

// OAuthProvider is an admin-configured OAuth2/OIDC provider used for login. There
// may be several; the signed OAuth state parameter carries the provider id so the
// callback knows which one to use.
type OAuthProvider struct {
	gorm.Model

	Enabled      bool
	Name         string
	ClientID     string
	ClientSecret string
	AuthURL      string
	TokenURL     string
	UserInfoURL  string
	RedirectURL  string
	// Scopes is a space-separated list.
	Scopes string
	// StateSecret is an auto-generated per-provider HMAC key for signing the
	// OAuth state parameter (stateless CSRF protection). Never returned to clients.
	StateSecret string
}

type LogSourceType string

const (
	FullLogSourceType     LogSourceType = "FULL"
	ContractLogSourceType LogSourceType = "CONTRACT"
	TopicLogSourceType    LogSourceType = "TOPIC"
	FactoryLogSourceType  LogSourceType = "FACTORY"
)

type LogSourceStatus string

const (
	RunningLogSourceStatus     LogSourceStatus = "RUNNING"
	LoopbackOffLogSourceStatus LogSourceStatus = "LOOPBACKOFF"
	StoppedLogSourceStatus     LogSourceStatus = "STOPPED"
)

type ExporterStatus string

const (
	RunningExporterStatus ExporterStatus = "RUNNING"
	StoppedExporterStatus ExporterStatus = "STOPPED"
	FailedExporterStatus  ExporterStatus = "FAILED"
)

type PluginStatus string

const (
	NotInstalledPluginStatus PluginStatus = "NOT_INSTALLED"
	InstallingPluginStatus   PluginStatus = "INSTALLING"
	InstalledPluginStatus    PluginStatus = "INSTALLED"
	FailedPluginStatus       PluginStatus = "FAILED"
)

// Plugin is an installable exporter plugin. Its source is resolved and compiled
// into a Go plugin (.so) at SoPath when installed; exporters then reference it.
type Plugin struct {
	gorm.Model

	Name        string
	Description string

	// Source. If LocalPath points at a prebuilt ".so" it is used directly.
	// Otherwise the server builds from GitUrl (any git repository, cloned) or
	// LocalPath (module root), compiling the RelativePath package.
	GitUrl       string
	RelativePath string
	LocalPath    string

	// SoPath is the compiled/resolved shared object; Status is one of
	// PluginStatus and Error holds the last install failure.
	SoPath string
	Status string
	Error  string

	// ConfigSchema is the plugin's declared config parameter schema (a JSON array
	// of exporter.ConfigField), extracted from the plugin at install time. Empty
	// when the plugin does not declare one.
	ConfigSchema datatypes.JSON
}

type EvmiInstance struct {
	gorm.Model

	InstanceId string
	IpV4       string
	Status     string
}

type EvmBlockchain struct {
	gorm.Model
	ChainId uint64
	Name    string
	RpcUrl  string

	BlockRange      uint64
	BlockSlice      uint64
	PullInterval    uint64
	RpcMaxBatchSize uint64

	SqdGatewayAvailable bool
	SqdGatewayUrl       string
}

type EvmJsonAbi struct {
	gorm.Model

	ContractName string
	Content      string
}

type EvmLogStore struct {
	gorm.Model

	Identifier  string
	Description string

	StoreType   string
	StoreConfig datatypes.JSON

	Pipelines []EvmLogPipeline
}

type EvmLogPipeline struct {
	gorm.Model

	Name       string
	LogSources []EvmLogSource

	EvmiInstanceID  uint
	EvmBlockchainID uint
	EvmLogStoreId   uint
}

type EvmLogSource struct {
	gorm.Model

	Enabled bool
	Status  string
	Type    string

	StartBlock uint64
	SyncBlock  uint64

	// Contract type data
	Address sql.NullString

	// Topic type data
	Topic0       sql.NullString
	TopicFilters pq.StringArray `gorm:"type:text[]"`

	// Factory type data
	FactoryChildEvmJsonABI       sql.NullInt32
	FactoryCreationFunctionName  sql.NullString
	FactoryCreationAddressLogArg sql.NullString

	// ParentSourceID is the FACTORY source that dynamically created this source
	// (0 for manually-created sources). A factory child is unique per
	// (ParentSourceID, Address).
	ParentSourceID uint `gorm:"index"`

	EvmLogPipelineID uint
	EvmJsonAbiID     uint
	EvmBlockchainID  uint
}

type EvmiExporter struct {
	gorm.Model

	Name string

	EvmLogPipelineID uint

	// Enabled controls whether the manager starts this exporter.
	Enabled bool
	// Status is one of ExporterStatus.
	Status string

	// StartBlock is the first block the exporter should process.
	StartBlock uint64
	// SyncBlock is the last fully-completed block (every log of blocks <=
	// SyncBlock has been delivered to the plugin). SyncLogIndex is the last
	// log_index delivered within the in-progress block (SyncBlock+1), or -1 when
	// none of it has been processed yet. Together they pin the exact last log the
	// exporter executed, so a restart resumes mid-block instead of replaying it.
	SyncBlock    uint64
	SyncLogIndex int64 `gorm:"default:-1"`

	// PluginID references the installed Plugin whose code this exporter runs.
	PluginID uint
	// PluginConfig is the per-exporter JSON configuration passed to the plugin.
	PluginConfig datatypes.JSON

	ChainSyncStatus datatypes.JSON
}
