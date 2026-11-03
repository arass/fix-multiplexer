// Package config validates the public configuration before starting any listener.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Service    Service    `yaml:"service"`
	Upstream   Upstream   `yaml:"upstream"`
	Downstream Downstream `yaml:"downstream"`
	Fanout     Fanout     `yaml:"fanout"`
	Retention  Retention  `yaml:"retention"`
	Logging    Logging    `yaml:"logging"`
}

type Service struct {
	Name            string        `yaml:"name"`
	Mode            string        `yaml:"mode"`
	Bind            string        `yaml:"bind"`
	HealthBind      string        `yaml:"health_bind"`
	MaxMessageBytes int           `yaml:"max_message_bytes"`
	UpstreamQueue   int           `yaml:"upstream_queue_messages"`
	DownstreamQueue int           `yaml:"downstream_queue_messages_per_client"`
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout"`
	StartupTimeout  time.Duration `yaml:"startup_timeout"`
}

// Secret references contain no credential values in the configuration itself.
type Secret struct {
	Env  string `yaml:"env"`
	File string `yaml:"file"`
}

type TLS struct {
	CAFile            string `yaml:"ca_file"`
	CertFile          string `yaml:"cert_file"`
	KeyFile           string `yaml:"key_file"`
	ServerName        string `yaml:"server_name"`
	RequireClientCert bool   `yaml:"require_client_cert"`
}

type Upstream struct {
	ReconnectInterval time.Duration `yaml:"reconnect_interval"`
	Version           string        `yaml:"fix_version"`
	Host              string        `yaml:"host"`
	Port              int           `yaml:"port"`
	Sender            string        `yaml:"sender_comp_id"`
	Target            string        `yaml:"target_comp_id"`
	StoreDir          string        `yaml:"store_dir"`
	TLSEnabled        bool          `yaml:"tls_enabled"`
	TLS               TLS           `yaml:"tls"`
	Username          Secret        `yaml:"username"`
	Password          Secret        `yaml:"password"`
	Dictionary        string        `yaml:"dictionary"`
	AppTypes          []string      `yaml:"app_message_types"`
}

type Client struct {
	Sender   string `yaml:"sender_comp_id"`
	Disabled bool   `yaml:"disabled"`
	Password Secret `yaml:"password"`
}

type Downstream struct {
	Version         string   `yaml:"fix_version"`
	Listen          string   `yaml:"listen"`
	Target          string   `yaml:"target_comp_id"`
	Clients         []Client `yaml:"clients"`
	StoreDir        string   `yaml:"store_dir"`
	TLSEnabled      bool     `yaml:"tls_enabled"`
	TLS             TLS      `yaml:"tls"`
	Dictionary      string   `yaml:"dictionary"`
	DuplicatePolicy string   `yaml:"duplicate_connection_policy"`
}

type Fanout struct {
	Mode      string   `yaml:"mode"`
	AppTypes  []string `yaml:"app_message_types"`
	DenyTypes []string `yaml:"deny_message_types"`
}

type Retention struct {
	DownstreamWindow  time.Duration `yaml:"downstream_replay_window"`
	UpstreamWindow    time.Duration `yaml:"upstream_replay_window"`
	IdleTTL           time.Duration `yaml:"inactive_client_ttl"`
	PruneActive       bool          `yaml:"prune_active_sessions"`
	DiskWarnBytes     int64         `yaml:"disk_warn_bytes"`
	DiskLimitBytes    int64         `yaml:"disk_limit_bytes"`
	SessionLimitBytes int64         `yaml:"session_limit_bytes"`
}

type Logging struct {
	Mode       string   `yaml:"mode"`
	RedactTags []string `yaml:"redact_tags"`
}

func Defaults() Config {
	var c Config
	c.Service = Service{Name: "fix-multiplexer", Mode: "nonproduction", Bind: "127.0.0.1", HealthBind: "127.0.0.1:8081", MaxMessageBytes: 1 << 20, UpstreamQueue: 1000, DownstreamQueue: 1000, ShutdownTimeout: 10 * time.Second, StartupTimeout: 15 * time.Second}
	c.Upstream.Version, c.Downstream.Version = "FIX.4.4", "FIX.4.4"
	c.Upstream.ReconnectInterval = 60 * time.Second
	c.Upstream.AppTypes = []string{"D", "F", "G", "H", "V", "x"}
	c.Downstream.DuplicatePolicy = "reject-new"
	c.Fanout = Fanout{Mode: "live-only", AppTypes: []string{"8", "9", "j", "W", "X", "Y", "y"}}
	c.Retention = Retention{DownstreamWindow: 8 * time.Hour, IdleTTL: 8 * time.Hour, DiskWarnBytes: 1 << 30, DiskLimitBytes: 2 << 30, SessionLimitBytes: 64 << 20}
	c.Logging.Mode = "metadata"
	return c
}

func Load(r io.Reader) (Config, error) {
	c := Defaults()
	data, err := io.ReadAll(io.LimitReader(r, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return Config{}, errors.New("configuration unreadable or exceeds 1MiB")
	}
	d := yaml.NewDecoder(bytes.NewReader(data))
	d.KnownFields(true)
	if err := d.Decode(&c); err != nil {
		return Config{}, errors.New("invalid YAML configuration (check syntax and field names)")
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return Config{}, errors.New("configuration must contain exactly one YAML document")
	}
	if err := c.Validate(); err != nil {
		return Config{}, err
	}
	return c, nil
}

var identifier = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
var msgType = regexp.MustCompile(`^[A-Za-z0-9]{1,8}$`)

func IsAdmin(t string) bool {
	switch t {
	case "0", "1", "2", "3", "4", "5", "A":
		return true
	}
	return false
}

func (c Config) Validate() error {
	invalid := func(field string) error { return fmt.Errorf("invalid configuration: %s", field) }
	if c.Service.Mode != "nonproduction" {
		return invalid("service.mode must be nonproduction")
	}
	if !identifier.MatchString(c.Service.Name) {
		return invalid("service.name")
	}
	if ip := net.ParseIP(c.Service.Bind); ip == nil || !ip.IsLoopback() {
		return invalid("service.bind must be loopback in v1")
	}
	if c.Service.HealthBind != "" && !loopbackAddress(c.Service.HealthBind) {
		return invalid("service.health_bind must be a loopback host:port")
	}
	if c.Service.MaxMessageBytes < 256 || c.Service.MaxMessageBytes > 1<<20 {
		return invalid("service.max_message_bytes must be between 256 and 1048576")
	}
	if c.Service.UpstreamQueue < 1 || c.Service.UpstreamQueue > 4096 || c.Service.DownstreamQueue < 1 || c.Service.DownstreamQueue > 4096 {
		return invalid("queue limits must be between 1 and 4096")
	}
	if c.Service.ShutdownTimeout < time.Second || c.Service.ShutdownTimeout > time.Minute {
		return invalid("service.shutdown_timeout must be between 1s and 1m")
	}
	if c.Service.StartupTimeout < time.Second || c.Service.StartupTimeout > time.Minute {
		return invalid("service.startup_timeout must be between 1s and 1m")
	}
	if c.Upstream.Version != "FIX.4.4" || c.Downstream.Version != "FIX.4.4" {
		return invalid("only FIX.4.4 end-to-end is supported")
	}
	if c.Upstream.Host == "" || c.Upstream.Port < 1 || c.Upstream.Port > 65535 {
		return invalid("upstream endpoint")
	}
	if c.Upstream.ReconnectInterval < time.Second {
		return invalid("upstream.reconnect_interval must be at least 1s")
	}
	if !identifier.MatchString(c.Upstream.Sender) || !identifier.MatchString(c.Upstream.Target) || c.Upstream.Sender == c.Upstream.Target {
		return invalid("upstream CompIDs must be distinct safe identifiers")
	}
	if !ipAddress(c.Downstream.Listen) {
		return invalid("downstream.listen must be a literal IP address and port")
	}
	remote := !loopbackAddress(c.Downstream.Listen)
	if c.Downstream.Listen == c.Service.HealthBind {
		return invalid("FIX and management listener addresses must differ")
	}
	if !identifier.MatchString(c.Downstream.Target) {
		return invalid("downstream.target_comp_id")
	}
	if len(c.Downstream.Clients) == 0 || len(c.Downstream.Clients) > 128 {
		return invalid("downstream allowlist must contain between 1 and 128 clients")
	}
	seen := make(map[string]bool)
	for _, cl := range c.Downstream.Clients {
		if !identifier.MatchString(cl.Sender) || cl.Sender == c.Downstream.Target || seen[cl.Sender] {
			return invalid("downstream clients must have distinct safe CompIDs")
		}
		seen[cl.Sender] = true
		if err := validSecret(cl.Password); err != nil {
			return invalid("downstream client password reference")
		}
		if remote && !cl.Disabled && cl.Password.Env == "" && cl.Password.File == "" {
			return invalid("non-loopback downstream listener requires a password for each enabled client")
		}
		if c.Downstream.Target == c.Upstream.Sender && cl.Sender == c.Upstream.Target {
			return invalid("upstream and downstream SessionIDs overlap")
		}
	}
	if c.Downstream.DuplicatePolicy != "reject-new" {
		return invalid("only reject-new duplicate connection policy is supported")
	}
	if c.Upstream.StoreDir == "" || c.Downstream.StoreDir == "" || filepath.Clean(c.Upstream.StoreDir) == filepath.Clean(c.Downstream.StoreDir) {
		return invalid("distinct upstream and downstream store directories required")
	}
	if c.Fanout.Mode != "live-only" {
		return invalid("fanout.mode must be live-only")
	}
	for _, types := range [][]string{c.Upstream.AppTypes, c.Fanout.AppTypes, c.Fanout.DenyTypes} {
		seenTypes := make(map[string]bool)
		for _, t := range types {
			if !msgType.MatchString(t) || IsAdmin(t) || seenTypes[t] {
				return invalid("application message policy contains invalid, administrative or duplicate types")
			}
			seenTypes[t] = true
		}
	}
	if len(c.Upstream.AppTypes) == 0 || len(c.Fanout.AppTypes) == 0 {
		return invalid("application allowlists must be nonempty")
	}
	if c.Retention.DownstreamWindow <= 0 || c.Retention.UpstreamWindow < 0 || c.Retention.IdleTTL <= 0 || c.Retention.PruneActive {
		return invalid("retention requires positive windows and forbids active pruning")
	}
	if c.Retention.DiskWarnBytes <= 0 || c.Retention.DiskLimitBytes <= c.Retention.DiskWarnBytes || c.Retention.SessionLimitBytes < int64(c.Service.MaxMessageBytes)*2 || c.Retention.SessionLimitBytes > c.Retention.DiskLimitBytes {
		return invalid("retention storage budgets")
	}
	if c.Logging.Mode != "metadata" {
		return invalid("logging.mode must be metadata")
	}
	for _, tag := range c.Logging.RedactTags {
		n, err := strconv.Atoi(tag)
		if err != nil || n <= 0 {
			return invalid("logging.redact_tags")
		}
	}
	if validSecret(c.Upstream.Username) != nil || validSecret(c.Upstream.Password) != nil {
		return invalid("upstream credential reference")
	}
	for _, tls := range []TLS{c.Upstream.TLS, c.Downstream.TLS} {
		if (tls.CertFile == "") != (tls.KeyFile == "") {
			return invalid("TLS certificate and key must be supplied together")
		}
	}
	if c.Downstream.TLSEnabled && c.Downstream.TLS.CertFile == "" {
		return invalid("downstream TLS requires certificate and key")
	}
	if c.Downstream.TLS.RequireClientCert && (!c.Downstream.TLSEnabled || c.Downstream.TLS.CAFile == "") {
		return invalid("downstream mTLS requires TLS and CA file")
	}
	return nil
}

func ipAddress(address string) bool {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	n, err := strconv.Atoi(port)
	return ip != nil && err == nil && n > 0 && n <= 65535
}

func loopbackAddress(address string) bool {
	if !ipAddress(address) {
		return false
	}
	host, _, _ := net.SplitHostPort(address)
	return net.ParseIP(host).IsLoopback()
}

func validSecret(s Secret) error {
	if s.Env != "" && s.File != "" {
		return errors.New("choose env or file")
	}
	return nil
}
