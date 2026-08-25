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
// into a standalone executable at BinaryPath when installed; exporters then
// reference it and EVMI runs it as a hashicorp/go-plugin subprocess.
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

	// BinaryPath is the installed plugin executable; Status is one of
	// PluginStatus and Error holds the last install failure.
	BinaryPath string
	Status     string
	Error      string

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
	// SyncBlock / SyncLogIndex are the exporter's aggregate position: the minimum
	// over its per-source cursors (EvmiExporterSourceCursor), i.e. the point up to
	// which every source of the pipeline has been exported. They are recomputed
	// and written once per exported batch and exist for the API/UI and metrics —
	// the per-source rows are what the export loop actually resumes from.
	SyncBlock    uint64
	SyncLogIndex int64 `gorm:"default:-1"`

	// PluginID references the installed Plugin whose code this exporter runs.
	PluginID uint
	// PluginConfig is the per-exporter JSON configuration passed to the plugin.
	PluginConfig datatypes.JSON

	ChainSyncStatus datatypes.JSON
}

// EvmiExporterSourceCursor is the export cursor of one exporter for one log
// source. Export progress is tracked per source rather than as a single
// pipeline-wide cursor because the set of sources is not fixed: a FACTORY rule or
// a plugin's Host.CreateLogSource can attach a new source long after the exporter
// started. With one shared cursor such a source could only ever be exported from
// wherever the exporter already stood, silently dropping every log it had stored
// below that point. Its own cursor starts at its own StartBlock instead, so it is
// exported from its very first log.
//
// (SyncBlock, SyncLogIndex) has exactly the same meaning as on EvmiExporter, but
// scoped to EvmLogSourceID: SyncBlock is the last fully-exported block of that
// source and SyncLogIndex the last log_index delivered within SyncBlock+1, or -1
// when none of it has been. These rows are the authoritative cursor;
// EvmiExporter.SyncBlock is the minimum across them, kept for display.
//
// SyncLogIndex deliberately carries no gorm `default` tag: GORM omits zero-valued
// fields for columns that have one, which would turn a legitimate "stopped at
// log_index 0" into the default and replay that block.
type EvmiExporterSourceCursor struct {
	gorm.Model

	EvmiExporterID uint `gorm:"uniqueIndex:idx_exporter_source_cursor"`
	EvmLogSourceID uint `gorm:"uniqueIndex:idx_exporter_source_cursor"`

	SyncBlock    uint64
	SyncLogIndex int64
}
