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

	// Source: the server clones GitUrl (any git repository) at GitRef and compiles
	// the repo root into a plugin. Git is the only supported source, and the
	// plugin's `main` package must live at the repo root.
	GitUrl string
	// GitRef is an optional branch or tag to clone (empty = the repo's default
	// branch).
	GitRef string

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
	// Port is the TCP port this instance's gRPC/HTTP server is listening on.
	Port   uint64
	Status string
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

	// Factory rules (Type == FACTORY). A FACTORY source has N creation rules
	// (EvmFactoryRule with EvmLogSourceID = this source, ParentRuleID = NULL). Each
	// rule matches a creation event and spawns a child of a given type/ABI; a rule
	// that spawns FACTORY children carries its own nested rules (recursive). When
	// such a rule spawns a child, its subtree is cloned onto the child source so
	// the child owns its own rules. See EvmFactoryRule.
	FactoryRules []EvmFactoryRule `gorm:"foreignKey:EvmLogSourceID"`

	// ParentSourceID is the FACTORY source that dynamically created this source
	// (0 for manually-created sources). A factory child is unique per
	// (ParentSourceID, Address).
	ParentSourceID uint `gorm:"index"`

	EvmLogPipelineID uint
	EvmJsonAbiID     uint
	EvmBlockchainID  uint
}

// EvmFactoryRule is one creation rule of a FACTORY source: "when CreationFunctionName
// fires, read the new address from CreationAddressLogArg and create a child of
// ChildType using EvmJsonAbiID". A source has N rules (1-to-N). A rule whose
// ChildType is FACTORY carries ChildRules — the rules the spawned child factory
// uses — making the model recursive (factories of factories).
type EvmFactoryRule struct {
	gorm.Model

	// Owner — exactly one is set, the other is NULL: EvmLogSourceID for a source's
	// top-level rules, ParentRuleID for the nested rules of a FACTORY rule. These are
	// nullable pointers (not a 0 sentinel) so the self/source foreign keys are
	// satisfied on databases that enforce them (Postgres/MySQL) — a 0 would reference
	// a non-existent row.
	EvmLogSourceID *uint `gorm:"index"`
	ParentRuleID   *uint `gorm:"index"`

	CreationFunctionName  string
	CreationAddressLogArg string
	// ChildType is the spawned source's type: CONTRACT or FACTORY.
	ChildType string
	// EvmJsonAbiID is the ABI assigned to the spawned child (its own ABI — for a
	// FACTORY child, the ABI its creation events are decoded with).
	EvmJsonAbiID uint

	// Conditions gate the rule: a child is created only when ALL conditions on the
	// creation event's decoded args pass (empty = always).
	Conditions []EvmFactoryRuleCondition `gorm:"foreignKey:EvmFactoryRuleID"`

	// ChildRules are used only when ChildType == FACTORY: the rules the spawned
	// child factory runs.
	ChildRules []EvmFactoryRule `gorm:"foreignKey:ParentRuleID"`
}

// EvmFactoryRuleCondition gates an EvmFactoryRule on a decoded event arg. The rule
// only spawns a child when every condition holds. Comparison is numeric when both
// the arg value and Value parse as integers, otherwise case-insensitive string.
type EvmFactoryRuleCondition struct {
	gorm.Model

	EvmFactoryRuleID uint `gorm:"index"`

	// Arg is the decoded event argument name; Operator is one of eq, neq, gt, gte,
	// lt, lte, contains; Value is compared against the arg's decoded value.
	Arg      string
	Operator string
	Value    string
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
