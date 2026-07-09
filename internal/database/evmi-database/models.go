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

// OAuthConfig is a singleton row holding the admin-configured OAuth2/OIDC
// provider used for login.
type OAuthConfig struct {
	gorm.Model

	Enabled      bool
	Provider     string
	ClientID     string
	ClientSecret string
	AuthURL      string
	TokenURL     string
	UserInfoURL  string
	RedirectURL  string
	// Scopes is a space-separated list.
	Scopes string
	// StateSecret is an auto-generated HMAC key for signing the OAuth state
	// parameter (stateless CSRF protection). Never returned to clients.
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

	// Plugin source. If PluginLocalPath points at a prebuilt ".so" it is loaded
	// directly. Otherwise the server builds a plugin from source: it clones
	// PluginGithubUrl (when set) or uses PluginLocalPath as the module root, and
	// builds the package at PluginRelativePath.
	PluginConfig       datatypes.JSON
	PluginGithubUrl    string
	PluginRelativePath string
	PluginLocalPath    string

	ChainSyncStatus datatypes.JSON
}
